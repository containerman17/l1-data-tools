package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

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
	addr      string
	conn      *websocket.Conn
	zstdDec   *zstd.Decoder
	reconnect bool
}

// Option configures the client
type Option func(*Client)

// WithReconnect enables automatic reconnection (default: true)
func WithReconnect(enabled bool) Option {
	return func(c *Client) {
		c.reconnect = enabled
	}
}

// NewClient creates a new sink client
func NewClient(addr string, opts ...Option) *Client {
	dec, _ := zstd.NewReader(nil)
	c := &Client{
		addr:      addr,
		reconnect: true,
		zstdDec:   dec,
	}
	for _, opt := range opts {
		opt(c)
	}
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

		for {
			select {
			case <-ctx.Done():
				c.Close()
				return ctx.Err()
			default:
			}

			blocks, err := c.readPack()
			if err != nil {
				c.Close()
				if !c.reconnect {
					return err
				}
				time.Sleep(5 * time.Second)
				break // reconnect
			}

			// Filter blocks below currentBlock (handles unaligned batch start)
			filtered := blocks[:0]
			for _, b := range blocks {
				if b.Number >= currentBlock {
					filtered = append(filtered, b)
				}
			}

			if len(filtered) > 0 {
				if err := handler(filtered); err != nil {
					c.Close()
					return err
				}
				currentBlock = filtered[len(filtered)-1].Number + 1
			}
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

func (c *Client) readPack() ([]Block, error) {
	_, data, err := c.conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	decompressed, err := c.zstdDec.DecodeAll(data, nil)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}

	var blocks []Block
	for _, line := range bytes.Split(decompressed, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var nb rpc.NormalizedBlock
		if err := json.Unmarshal(line, &nb); err != nil {
			return nil, fmt.Errorf("parse block: %w", err)
		}
		blockNum, err := parseHex(nb.Block.Number)
		if err != nil {
			return nil, fmt.Errorf("parse block number: %w", err)
		}
		blocks = append(blocks, Block{Number: blockNum, Data: &nb})
	}
	return blocks, nil
}

func parseHex(s string) (uint64, error) {
	return strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
}
