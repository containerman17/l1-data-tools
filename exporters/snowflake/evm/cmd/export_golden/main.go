// export_golden fetches blocks from the ingestion service based on chain config
// and saves them as zst-compressed JSONL to notes/assets/{PREFIX}_BLOCKS.jsonl.zst
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/containerman17/l1-data-tools/ingestion/evm/client"
	"github.com/joho/godotenv"
	"github.com/klauspost/compress/zstd"
)

// Known chain configurations
// Comment out chains that don't have their indexer synced yet
var chainConfigs = map[string]struct {
	filePrefix string
	startBlock uint64
	endBlock   uint64
}{
	// C-Chain: blocks 75000000-75100000 (commented - indexer not synced)
	// "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5": {
	// 	filePrefix: "C_",
	// 	startBlock: 75000000,
	// 	endBlock:   75100000,
	// },
	// Gunzilla: blocks 14000000-14100000
	"2M47TxWHGnhNtq6pM5zPXdATBtuqubxn5EPFgFmEawCQr9WFML": {
		filePrefix: "GUNZILLA_",
		startBlock: 14000000,
		endBlock:   14100000,
	},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Find .env relative to this command's location in the repo
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
	wsAddr := parseWSAddr(ingestionURL)
	fmt.Printf("Connecting to %s...\n", wsAddr)

	// Create client
	c := client.NewClient(wsAddr, client.WithReconnect(false))

	// Get chain info
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	info, err := c.Info(ctx)
	cancel()
	if err != nil {
		return fmt.Errorf("get chain info: %w", err)
	}

	fmt.Printf("Chain ID: %s\n", info.ChainID)
	fmt.Printf("Latest block: %d\n", info.LatestBlock)

	// Get chain config
	cfg, ok := chainConfigs[info.ChainID]
	if !ok {
		return fmt.Errorf("unknown chain ID: %s (no golden file config)", info.ChainID)
	}

	// Check if output file already exists
	outputPath := findOutputPath(cfg.filePrefix)
	if _, err := os.Stat(outputPath); err == nil {
		fmt.Printf("Output file already exists: %s (skipping)\n", outputPath)
		return nil
	}

	// Check if latest block is sufficient
	if info.LatestBlock < cfg.endBlock {
		return fmt.Errorf("indexer not synced: latest=%d, need=%d", info.LatestBlock, cfg.endBlock)
	}

	fmt.Printf("Fetching sparse blocks (every 100th) from %d to %d for %s...\n", cfg.startBlock, cfg.endBlock, cfg.filePrefix)

	// Collect only blocks where blockNumber % 100 == 0 (sparse sampling)
	// This matches the Snowflake golden data query: MOD(BLOCKNUMBER, 100) = 0
	var allBlocks []client.Block
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	err = c.Stream(ctx, cfg.startBlock, func(blocks []client.Block) error {
		for _, b := range blocks {
			if b.Number > cfg.endBlock {
				cancel() // Signal to stop
				return nil
			}
			// Only keep blocks where blockNumber % 100 == 0
			if b.Number%100 == 0 {
				allBlocks = append(allBlocks, b)
				if len(allBlocks)%100 == 0 {
					fmt.Printf("  collected %d sparse blocks...\n", len(allBlocks))
				}
			}
			if b.Number == cfg.endBlock {
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

func findOutputPath(prefix string) string {
	filename := prefix + "BLOCKS.jsonl.zst"
	candidates := []string{
		"exporters/snowflake/evm/notes/assets/" + filename,
		"notes/assets/" + filename,
		"../../notes/assets/" + filename,
	}
	for _, c := range candidates {
		dir := filepath.Dir(c)
		if _, err := os.Stat(dir); err == nil {
			return c
		}
	}
	// Default
	return "exporters/snowflake/evm/notes/assets/" + filename
}

func parseWSAddr(url string) string {
	// Strip protocol
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
