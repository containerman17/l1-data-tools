package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/containerman17/l1-data-tools/evm-ingestion/api"
	"github.com/containerman17/l1-data-tools/evm-ingestion/consts"
	"github.com/containerman17/l1-data-tools/evm-ingestion/metrics"
	"github.com/containerman17/l1-data-tools/evm-ingestion/rpc"
	"github.com/containerman17/l1-data-tools/evm-ingestion/storage"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load() // Load .env if present

	// Required: RPC URL and Chain ID
	rpcURL := os.Getenv("RPC_URL")
	if rpcURL == "" {
		log.Fatal("RPC_URL environment variable is required")
	}
	chainID := os.Getenv("CHAIN_ID")
	if chainID == "" {
		log.Fatal("CHAIN_ID environment variable is required (32-byte Avalanche chain ID)")
	}

	// Optional: other settings with defaults
	pebblePath := getEnvOrDefault("PEBBLE_PATH", "./data/pebble")
	serverAddr := getEnvOrDefault("SERVER_ADDR", consts.ServerListenAddr)
	maxParallelism := getEnvIntOrDefault("MAX_PARALLELISM", consts.RPCDefaultMaxParallelism)
	lookahead := getEnvIntOrDefault("LOOKAHEAD", 100)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Fetch EVM chainID from RPC
	evmChainID, err := fetchChainID(rpcURL)
	if err != nil {
		log.Fatalf("Failed to fetch chainID from RPC: %v", err)
	}
	log.Printf("Connected to EVM chain %d (Avalanche chain %s)", evmChainID, chainID)

	// Initialize storage
	store, err := storage.NewPebbleStorage(pebblePath)
	if err != nil {
		log.Fatalf("Failed to open storage: %v", err)
	}
	defer store.Close()
	log.Printf("Storage opened at %s", pebblePath)

	// Initialize API server
	server := api.NewServer(store, chainID)

	// Initialize metrics
	chainLabel := fmt.Sprintf("chain-%d", evmChainID)
	metrics.InitChain(chainLabel)

	// Create controller and fetcher
	chainCfg := rpc.ChainConfig{
		ChainID:        evmChainID,
		Name:           chainLabel,
		URL:            rpcURL,
		MaxParallelism: maxParallelism,
	}
	controller := rpc.NewController(chainCfg)

	fetcher, err := rpc.NewFetcher(rpc.FetcherConfig{
		Controller: controller,
		ChainID:    evmChainID,
		ChainName:  chainLabel,
		Ctx:        ctx,
	})
	if err != nil {
		log.Fatalf("Failed to create fetcher: %v", err)
	}

	// Start compactor
	compactor := storage.NewCompactor(store)
	compactor.Start(ctx)
	log.Printf("Compactor started")

	// Start ingestion loop
	go runIngestion(ctx, fetcher, store, server, chainLabel, lookahead)
	log.Printf("Ingestion started for chain %s", chainID)

	// Start API server (auto-finds available port starting from base)
	actualAddr, err := server.Start(serverAddr)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
	log.Printf("Server listening on %s", actualAddr)

	// Start metrics server
	metrics.StartServer(consts.MetricsListenAddr)

	// Block forever
	select {}
}

func runIngestion(ctx context.Context, fetcher *rpc.Fetcher, store storage.Storage, server *api.Server, chainLabel string, lookahead int) {
	// Retry loop - if streaming fails, restart from last saved block
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Determine starting block: individual blocks > batches > meta > block 1
		currentBlock := uint64(1)
		if latest, ok := store.LatestBlock(); ok {
			currentBlock = latest + 1
			log.Printf("Resuming from individual blocks at block %d", currentBlock)
		} else if latestBatch, ok := store.LatestBatch(); ok {
			currentBlock = latestBatch + 1
			log.Printf("Resuming from batches at block %d", currentBlock)
		} else if meta := store.GetMeta(); meta > 0 {
			currentBlock = meta + 1
			log.Printf("Resuming from meta at block %d", currentBlock)
		} else {
			log.Printf("Starting from block 1")
		}

		blocksCh := make(chan *rpc.NormalizedBlock, lookahead)

		// Start streaming
		go func() {
			if err := fetcher.StreamBlocks(ctx, currentBlock, lookahead, blocksCh); err != nil {
				log.Printf("Stream error: %v", err)
			}
			close(blocksCh)
		}()

		// Track stats
		startTime := time.Now()
		startBlock := currentBlock
		lastLogTime := time.Now()

		for block := range blocksCh {
			// Extract block number from the block itself
			blockNum, err := parseBlockNumber(block.Block.Number)
			if err != nil {
				log.Printf("Failed to parse block number: %v", err)
				break
			}

			// Verify ordering
			if blockNum != currentBlock {
				log.Printf("Block number mismatch: expected %d, got %d", currentBlock, blockNum)
				break
			}

			data, err := json.Marshal(block)
			if err != nil {
				log.Printf("Failed to marshal block %d: %v", blockNum, err)
				break
			}

			if err := store.SaveBlock(blockNum, data); err != nil {
				log.Printf("Failed to save block %d: %v", blockNum, err)
				break
			}

			server.UpdateLatestBlock(blockNum)
			metrics.BlocksTotal.WithLabelValues(chainLabel).Inc()
			metrics.LastIngestedBlock.WithLabelValues(chainLabel).Set(float64(blockNum))
			currentBlock++

			// Log progress every 5 seconds
			if time.Since(lastLogTime) >= 5*time.Second {
				lastLogTime = time.Now()

				totalElapsed := time.Since(startTime)
				totalBlocks := currentBlock - startBlock
				avgBlocksPerSec := float64(totalBlocks) / totalElapsed.Seconds()

				latestBlock, _ := fetcher.GetLatestBlock(ctx)
				blocksRemaining := int64(latestBlock) - int64(currentBlock) + 1
				if blocksRemaining < 0 {
					blocksRemaining = 0
				}
				metrics.BlocksBehind.WithLabelValues(chainLabel).Set(float64(blocksRemaining))
				metrics.ChainHead.WithLabelValues(chainLabel).Set(float64(latestBlock))
				eta := time.Duration(float64(blocksRemaining)/avgBlocksPerSec) * time.Second

				log.Printf("block %d | %.1f blk/s avg | %d behind, eta %s | p=%d p95=%dms",
					currentBlock-1,
					avgBlocksPerSec, blocksRemaining, formatDuration(eta),
					fetcher.Controller().CurrentParallelism(),
					fetcher.Controller().P95Latency().Milliseconds())
			}
		}

		// Stream ended - wait and retry
		log.Printf("Ingestion stopped, restarting in 5s...")
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// fetchChainID gets the chain ID from the RPC endpoint
func fetchChainID(rpcURL string) (uint64, error) {
	reqBody, _ := json.Marshal(rpc.JSONRPCRequest{
		Jsonrpc: "2.0",
		Method:  "eth_chainId",
		Params:  []interface{}{},
		ID:      1,
	})

	resp, err := http.Post(rpcURL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return 0, fmt.Errorf("RPC request failed: %w", err)
	}
	defer resp.Body.Close()

	var rpcResp rpc.JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	if rpcResp.Error != nil {
		return 0, fmt.Errorf("RPC error: %s", rpcResp.Error.Message)
	}

	var hexChainID string
	if err := json.Unmarshal(rpcResp.Result, &hexChainID); err != nil {
		return 0, fmt.Errorf("failed to unmarshal chainID: %w", err)
	}

	return parseBlockNumber(hexChainID)
}

func parseBlockNumber(hexNum string) (uint64, error) {
	numStr := strings.TrimPrefix(hexNum, "0x")
	return strconv.ParseUint(numStr, 16, 64)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	d = d.Round(time.Minute)
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	hours := d / time.Hour
	d -= hours * time.Hour
	minutes := d / time.Minute

	if days > 0 {
		return fmt.Sprintf("%dd%dh%dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func getEnvIntOrDefault(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultValue
}
