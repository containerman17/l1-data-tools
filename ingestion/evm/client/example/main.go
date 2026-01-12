package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/rpc"

	"github.com/gorilla/websocket"
	"github.com/klauspost/compress/zstd"
)

const (
	connectTimeout = 5 * time.Second
	readTimeout    = 5 * time.Second
	reconnectDelay = 2 * time.Second
)

func main() {
	fmt.Println("Starting example client")
	addr := flag.String("addr", "localhost:9090", "Server address")
	fromBlock := flag.Uint64("from", 1, "Starting block number")
	flag.Parse()

	ctx := context.Background()

	// Stats tracking
	startTime := time.Now()
	totalBlocks := uint64(0)
	totalTxs := uint64(0)
	lastLogTime := time.Now()
	lastLogBlocks := uint64(0)
	lastBlock := *fromBlock - 1

	// Reconnection loop
	for {
		nextBlock := lastBlock + 1
		fmt.Printf("[%s] Connecting to %s from block %d...\n",
			time.Now().Format("15:04:05"), *addr, nextBlock)

		err := streamBlocks(ctx, *addr, nextBlock, func(blockNum uint64, block *rpc.NormalizedBlock) error {
			// Validate order
			if blockNum != lastBlock+1 {
				log.Fatalf("FATAL: Expected block %d, got %d", lastBlock+1, blockNum)
			}
			lastBlock = blockNum
			totalBlocks++
			totalTxs += uint64(len(block.Block.Transactions))

			// Log once per second if new blocks were processed
			if time.Since(lastLogTime) >= time.Second {
				recentBlocks := totalBlocks - lastLogBlocks
				if recentBlocks > 0 {
					now := time.Now()
					recentElapsed := now.Sub(lastLogTime).Seconds()
					recentRate := float64(recentBlocks) / recentElapsed

					totalElapsed := now.Sub(startTime).Seconds()
					avgRate := float64(totalBlocks) / totalElapsed

					fmt.Printf("Block %d | %.1f blk/s recent | %.1f blk/s avg | %d txs in block | %d total txs\n",
						blockNum, recentRate, avgRate, len(block.Block.Transactions), totalTxs)

					lastLogBlocks = totalBlocks
				}
				lastLogTime = time.Now()
			}
			return nil
		})

		if err != nil {
			fmt.Printf("\n=== FINAL STATS ===\n")
			fmt.Printf("Last block: %d\n", lastBlock)
			fmt.Printf("Total blocks: %d\n", totalBlocks)
			fmt.Printf("Total transactions: %d\n", totalTxs)
			fmt.Printf("===================\n\n")
			fmt.Printf("[%s] Disconnected: %v (last processed: block %d). Reconnecting in %v...\n",
				time.Now().Format("15:04:05"), err, lastBlock, reconnectDelay)
			time.Sleep(reconnectDelay)
		}
	}
}

// parseBlockNumber parses hex block number string to uint64
func parseBlockNumber(hexNum string) (uint64, error) {
	numStr := strings.TrimPrefix(hexNum, "0x")
	return strconv.ParseUint(numStr, 16, 64)
}

func streamBlocks(ctx context.Context, addr string, fromBlock uint64, handler func(uint64, *rpc.NormalizedBlock) error) error {
	// Connect via WebSocket
	url := fmt.Sprintf("ws://%s/ws?from=%d", addr, fromBlock)

	dialer := websocket.Dialer{
		HandshakeTimeout: connectTimeout,
	}

	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}
	defer conn.Close()

	// Create zstd decoder for decompressing frames
	zstdDec, err := zstd.NewReader(nil)
	if err != nil {
		return fmt.Errorf("zstd decoder failed: %w", err)
	}
	defer zstdDec.Close()

	currentBlock := fromBlock

	// Stream blocks
	for {
		// Set read deadline for each message
		conn.SetReadDeadline(time.Now().Add(readTimeout))

		_, data, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read failed: %w", err)
		}

		// Decompress
		decompressed, err := zstdDec.DecodeAll(data, nil)
		if err != nil {
			return fmt.Errorf("decompress failed: %w", err)
		}

		// Parse JSONL - each line is a NormalizedBlock
		for _, line := range bytes.Split(decompressed, []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}

			var block rpc.NormalizedBlock
			if err := json.Unmarshal(line, &block); err != nil {
				return fmt.Errorf("parse block failed: %w", err)
			}

			blockNum, err := parseBlockNumber(block.Block.Number)
			if err != nil {
				return fmt.Errorf("parse block number failed: %w", err)
			}

			// Filter blocks below our fromBlock (handles unaligned batch)
			if blockNum < currentBlock {
				continue
			}

			if err := handler(blockNum, &block); err != nil {
				return err
			}
			currentBlock = blockNum + 1
		}
	}
}
