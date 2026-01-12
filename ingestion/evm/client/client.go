package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/metrics"
	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/rpc"

	"github.com/gorilla/websocket"
	"github.com/klauspost/compress/zstd"
)

// Block represents a received block with its parsed data
type Block struct {
	Number uint64
	Data   *rpc.NormalizedBlock
}

// Client connects to an EVM sink and streams blocks
type Client struct {
	addr         string
	conn         *websocket.Conn
	zstdDec      *zstd.Decoder
	reconnect    bool
	bufferConfig BufferConfig
}

// NewClient creates a new sink client
func NewClient(addr string, opts ...Option) *Client {
	dec, _ := zstd.NewReader(nil)
	c := &Client{
		addr:         addr,
		reconnect:    true,
		zstdDec:      dec,
		bufferConfig: DefaultBufferConfig(),
	}
	for _, opt := range opts {
		opt(c)
	}
	// Initialize capacity metric
	metrics.ClientBufferCapacityBytes.Set(float64(c.bufferConfig.BufferSize))
	return c
}

// Handler is called for each pack of blocks (one websocket frame).
// Historical sync: up to 100 blocks per pack.
// Real-time: 1 block per pack.
type Handler func(blocks []Block) error

// Stream connects and streams block packs, calling handler for each pack.
// Automatically reconnects on disconnect if enabled.
func (c *Client) Stream(ctx context.Context, fromBlock uint64, handler Handler) error {
	currentBlock := fromBlock

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := c.connect(ctx, currentBlock); err != nil {
			if !c.reconnect {
				return err
			}
			time.Sleep(5 * time.Second)
			continue
		}

		// Init buffer and channels
		buf := newReceiveBuffer(c.bufferConfig)
		errCh := make(chan error, 1)

		// Start receiver
		recvCtx, cancelRecv := context.WithCancel(ctx)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.receiveLoop(recvCtx, buf, errCh)
		}()

		// Run processor
		lastBlock, err := c.processLoop(ctx, buf, currentBlock, handler, errCh)

		// Cleanup
		cancelRecv()
		buf.close()
		c.Close()
		wg.Wait()

		if err != nil {
			// If context passed to Stream was canceled, return clean error
			if ctx.Err() != nil {
				return ctx.Err()
			}

			if !c.reconnect {
				return err
			}

			// Update resume position if we processed anything
			if lastBlock >= currentBlock {
				currentBlock = lastBlock + 1
			}

			time.Sleep(5 * time.Second)
			continue
		}
	}
}

func (c *Client) receiveLoop(ctx context.Context, buf *receiveBuffer, errCh chan<- error) {
	for {
		if !buf.waitForSpace() {
			return // Buffer closed
		}

		// Double check context before reading
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := c.conn.ReadMessage()
		if err != nil {
			select {
			case errCh <- err:
			default:
			}
			return
		}

		// Copy data (websocket buffer is reused)
		dataCopy := make([]byte, len(data))
		copy(dataCopy, data)

		buf.push(dataCopy)
	}
}

func (c *Client) processLoop(ctx context.Context, buf *receiveBuffer, fromBlock uint64, handler Handler, errCh <-chan error) (uint64, error) {
	currentBlock := fromBlock

	for {
		// Check for errors from receiver
		select {
		case <-ctx.Done():
			return currentBlock - 1, ctx.Err()
		case err := <-errCh:
			return currentBlock - 1, err
		default:
		}

		batch := buf.sliceBatch(c.bufferConfig.MaxBatchSize)
		if len(batch) == 0 {
			// Check if receiver failed while we were waiting
			select {
			case err := <-errCh:
				return currentBlock - 1, err
			default:
				time.Sleep(1 * time.Millisecond)
				continue
			}
		}

		// Process batch
		var allBlocks []Block
		for _, item := range batch {
			decompressed, err := c.zstdDec.DecodeAll(item.compressedData, nil)
			if err != nil {
				return currentBlock - 1, fmt.Errorf("decompress: %w", err)
			}

			for _, line := range bytes.Split(decompressed, []byte{'\n'}) {
				if len(line) == 0 {
					continue
				}
				var nb rpc.NormalizedBlock
				if err := json.Unmarshal(line, &nb); err != nil {
					return currentBlock - 1, fmt.Errorf("parse block: %w", err)
				}
				blockNum, err := parseHex(nb.Block.Number)
				if err != nil {
					return currentBlock - 1, fmt.Errorf("parse block number: %w", err)
				}

				// Filter old blocks (e.g. from batch overlap)
				if blockNum >= currentBlock {
					allBlocks = append(allBlocks, Block{Number: blockNum, Data: &nb})
				}
			}
		}

		if len(allBlocks) > 0 {
			if err := handler(allBlocks); err != nil {
				return currentBlock - 1, err
			}
			currentBlock = allBlocks[len(allBlocks)-1].Number + 1
		}
	}
}

// Close closes the connection
func (c *Client) Close() error {
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

func (c *Client) connect(ctx context.Context, fromBlock uint64) error {
	url := fmt.Sprintf("ws://%s/ws?from=%d", c.addr, fromBlock)
	conn, _, err := (&websocket.Dialer{HandshakeTimeout: 10 * time.Second}).DialContext(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	c.conn = conn
	return nil
}

func parseHex(s string) (uint64, error) {
	return strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
}
