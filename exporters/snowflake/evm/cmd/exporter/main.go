package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/containerman17/l1-data-tools/exporters/snowflake/evm/pkg/snowflake"
	"github.com/containerman17/l1-data-tools/exporters/snowflake/evm/pkg/transform"
	"github.com/containerman17/l1-data-tools/ingestion/evm/client"
	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/rpc"
)

type Config struct {
	Snowflake        snowflake.Config
	IngestionURL     string
	BatchSize        int
	PartialBatchWait time.Duration
	ErrorBackoff     time.Duration
}

func loadConfig() (*Config, error) {
	cfg := &Config{
		Snowflake: snowflake.Config{
			Account:          getEnv("SNOWFLAKE_ACCOUNT", ""),
			User:             getEnv("SNOWFLAKE_USER", ""),
			PrivateKeyBase64: getEnv("SNOWFLAKE_PRIVATE_KEY", ""),
			Database:         getEnv("SNOWFLAKE_DATABASE", ""),
			Schema:           getEnv("SNOWFLAKE_SCHEMA", ""),
			Warehouse:        getEnv("SNOWFLAKE_WAREHOUSE", ""),
			Role:             getEnv("SNOWFLAKE_ROLE", ""),
			TablePrefix:      getEnv("SNOWFLAKE_TABLE_PREFIX", ""),
		},
		IngestionURL:     getEnv("INGESTION_URL", ""),
		BatchSize:        getIntEnv("BATCH_SIZE", 1000),
		PartialBatchWait: getDurationEnv("PARTIAL_BATCH_WAIT", time.Hour),
		ErrorBackoff:     getDurationEnv("ERROR_BACKOFF", 5*time.Minute),
	}

	requiredVars := []struct {
		name  string
		value string
	}{
		{"SNOWFLAKE_ACCOUNT", cfg.Snowflake.Account},
		{"SNOWFLAKE_USER", cfg.Snowflake.User},
		{"SNOWFLAKE_PRIVATE_KEY", cfg.Snowflake.PrivateKeyBase64},
		{"SNOWFLAKE_DATABASE", cfg.Snowflake.Database},
		{"SNOWFLAKE_SCHEMA", cfg.Snowflake.Schema},
		{"SNOWFLAKE_WAREHOUSE", cfg.Snowflake.Warehouse},
		{"SNOWFLAKE_TABLE_PREFIX", cfg.Snowflake.TablePrefix},
		{"INGESTION_URL", cfg.IngestionURL},
	}

	for _, v := range requiredVars {
		if v.value == "" {
			return nil, fmt.Errorf("required environment variable %s is not set", v.name)
		}
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getIntEnv(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}

// sleep waits for the specified duration or until context is cancelled.
// Returns true if duration elapsed, false if context was cancelled.
func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

func run(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	sfClient, err := snowflake.New(cfg.Snowflake)
	if err != nil {
		return fmt.Errorf("connect to Snowflake: %w", err)
	}
	defer sfClient.Close()

	ingClient := client.NewClient(cfg.IngestionURL, client.WithReconnect(false))

	log.Printf("Starting EVM exporter daemon (batch_size=%d, partial_wait=%v, error_backoff=%v)",
		cfg.BatchSize, cfg.PartialBatchWait, cfg.ErrorBackoff)

	for {
		// Check for context cancellation before starting a batch
		if ctx.Err() != nil {
			log.Println("Shutdown requested, exiting daemon loop")
			return nil
		}

		blocksWritten, err := runOneBatch(ctx, sfClient, ingClient, cfg)
		if err != nil {
			// Recoverable error - log and retry after backoff
			log.Printf("Batch error: %v, retrying in %v", err, cfg.ErrorBackoff)
			if !sleep(ctx, cfg.ErrorBackoff) {
				log.Println("Shutdown requested during error backoff")
				return nil
			}
			continue
		}

		// Rate limiting based on batch size
		if blocksWritten < cfg.BatchSize {
			// Partial batch or caught up - wait before next attempt
			log.Printf("Partial batch (%d blocks), waiting %v before next run", blocksWritten, cfg.PartialBatchWait)
			if !sleep(ctx, cfg.PartialBatchWait) {
				log.Println("Shutdown requested during partial batch wait")
				return nil
			}
		}
		// Full batch = catching up, loop immediately
	}
}

func runOneBatch(ctx context.Context, sfClient *snowflake.Client, ingClient *client.Client, cfg *Config) (int, error) {
	// 1. Get last exported block
	fromBlock, err := sfClient.GetLastBlock(ctx)
	if err != nil {
		return 0, fmt.Errorf("get last exported block: %w", err)
	}

	if fromBlock == -1 {
		log.Println("No blocks found in Snowflake, starting from block 0")
		fromBlock = 0
	} else {
		fromBlock++
	}

	// 2. Get latest available
	info, err := ingClient.Info(ctx)
	if err != nil {
		return 0, fmt.Errorf("get ingestion info: %w", err)
	}

	if fromBlock > int64(info.LatestBlock) {
		log.Printf("Caught up: exported through block %d (ingestion at %d)", fromBlock-1, info.LatestBlock)
		return 0, nil // No blocks to write
	}

	// 3. Calculate batch range
	targetBlock := min(fromBlock+int64(cfg.BatchSize)-1, int64(info.LatestBlock))
	log.Printf("Fetching blocks %d-%d (latest available: %d)", fromBlock, targetBlock, info.LatestBlock)

	// 4. Stream blocks
	blocks, err := fetchBlocks(ctx, ingClient, uint64(fromBlock), uint64(targetBlock))
	if err != nil {
		return 0, fmt.Errorf("fetch blocks %d-%d: %w", fromBlock, targetBlock, err)
	}

	if len(blocks) == 0 {
		return 0, fmt.Errorf("no blocks received for range %d-%d", fromBlock, targetBlock)
	}

	// 5. Transform
	batch := transform.Transform(blocks)

	// 6. Write atomically
	if err := sfClient.WriteBatch(ctx, batch); err != nil {
		return 0, fmt.Errorf("write batch: %w", err)
	}

	blocksWritten := len(blocks)
	log.Printf("Wrote %d blocks (%d txs, %d logs)", blocksWritten, len(batch.Transactions), len(batch.Logs))

	return blocksWritten, nil
}

func fetchBlocks(ctx context.Context, ingClient *client.Client, fromBlock, toBlock uint64) ([]rpc.NormalizedBlock, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var blocks []rpc.NormalizedBlock

	err := ingClient.Stream(streamCtx, fromBlock, func(blockBatch []client.Block) error {
		for _, b := range blockBatch {
			blocks = append(blocks, *b.Data)
			if b.Number >= toBlock {
				cancel()
				break
			}
		}
		return nil
	})

	if err != nil && !errors.Is(err, context.Canceled) {
		return nil, err
	}

	return blocks, nil
}

func main() {
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("Received signal %v, initiating shutdown...", sig)
		cancel()
	}()

	if err := run(ctx); err != nil {
		log.Fatalf("Exporter failed: %v", err)
		os.Exit(1)
	}

	log.Println("Exporter shutdown complete")
}
