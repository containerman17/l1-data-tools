// export_golden fetches blocks 1-100 from the ingestion service
// and saves them as zst-compressed JSONL to notes/assets/blocks_1_100.zst
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/containerman17/l1-data-tools/ingestion/evm/client"
	"github.com/joho/godotenv"
	"github.com/klauspost/compress/zstd"
)

const (
	startBlock = 1
	endBlock   = 100
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Find .env relative to this command's location in the repo
	// We expect to be run from the repo root or via go run
	envPath := findEnvFile()
	if envPath != "" {
		if err := godotenv.Load(envPath); err != nil {
			fmt.Printf("Warning: could not load .env from %s: %v\n", envPath, err)
		}
	}

	ingestionURL := os.Getenv("INGESTION_URL")
	if ingestionURL == "" {
		return fmt.Errorf("INGESTION_URL not set")
	}

	// Parse URL to get host:port for websocket
	// INGESTION_URL format: http://host/path or ws://host/path
	wsAddr := parseWSAddr(ingestionURL)
	fmt.Printf("Connecting to %s...\n", wsAddr)

	// Create client with reconnect disabled (one-shot)
	c := client.NewClient(wsAddr, client.WithReconnect(false))

	// Collect all blocks
	var allBlocks []client.Block
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Printf("Fetching blocks %d to %d...\n", startBlock, endBlock)

	err := c.Stream(ctx, startBlock, func(blocks []client.Block) error {
		for _, b := range blocks {
			if b.Number > endBlock {
				cancel() // Signal to stop
				return nil
			}
			allBlocks = append(allBlocks, b)
			if b.Number == endBlock {
				cancel() // Done
				return nil
			}
		}
		return nil
	})

	// Context canceled is expected when we're done
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("stream error: %w", err)
	}

	fmt.Printf("Fetched %d blocks\n", len(allBlocks))

	// Sort by block number
	sort.Slice(allBlocks, func(i, j int) bool {
		return allBlocks[i].Number < allBlocks[j].Number
	})

	// Serialize to JSONL
	var buf bytes.Buffer
	for _, b := range allBlocks {
		data, err := json.Marshal(b.Data)
		if err != nil {
			return fmt.Errorf("marshal block %d: %w", b.Number, err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}

	// Compress with zstd
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		return fmt.Errorf("create zstd encoder: %w", err)
	}
	compressed := encoder.EncodeAll(buf.Bytes(), nil)

	// Write to output file
	outputPath := findOutputPath()
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	if err := os.WriteFile(outputPath, compressed, 0644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	fmt.Printf("Written %d bytes (compressed from %d) to %s\n",
		len(compressed), buf.Len(), outputPath)

	return nil
}

func findEnvFile() string {
	// Try relative paths from likely run locations
	candidates := []string{
		"exporters/snowflake/evm/.env",
		".env",
		"../../.env", // If run from cmd/export_golden
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func findOutputPath() string {
	// Try to find the assets directory
	candidates := []string{
		"exporters/snowflake/evm/notes/assets/blocks_1_100.zst",
		"notes/assets/blocks_1_100.zst",
		"../../notes/assets/blocks_1_100.zst",
	}
	for _, c := range candidates {
		dir := filepath.Dir(c)
		if _, err := os.Stat(dir); err == nil {
			return c
		}
	}
	// Default
	return "exporters/snowflake/evm/notes/assets/blocks_1_100.zst"
}

func parseWSAddr(url string) string {
	// Strip protocol
	// Input: http://100.29.188.167/indexer/.../ws
	// Output: 100.29.188.167/indexer/... (without protocol and trailing /ws)
	for _, prefix := range []string{"http://", "https://", "ws://", "wss://"} {
		if len(url) > len(prefix) && url[:len(prefix)] == prefix {
			url = url[len(prefix):]
			break
		}
	}
	// Strip trailing /ws since the client adds it
	if len(url) > 3 && url[len(url)-3:] == "/ws" {
		url = url[:len(url)-3]
	}
	return url
}
