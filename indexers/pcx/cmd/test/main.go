// cmd/test - Development tool for testing a single indexer.
// Usage: go run ./cmd/test pending_rewards --fresh
//
// This tool:
// 1. Optionally drops the indexer's data (--fresh)
// 2. Starts ONLY the specified indexer
// 3. Waits for it to sync
// 4. Runs its test cases against Glacier
// 5. Exits with status code 0 (all pass) or 1 (failures)
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/cchain"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/db"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexers/assets"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexers/blockchains"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexers/historical_rewards"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexers/list_chain_ids"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexers/network_stats"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexers/pending_rewards"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexers/subnets"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexers/utxos"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexers/validators"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/pchain"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/runner"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/selftest"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/xchain"
	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/sstable/block"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
)

func pebbleOpts() *pebble.Options {
	opts := &pebble.Options{Logger: db.QuietLogger()}
	opts.ApplyCompressionSettings(func() pebble.DBCompressionSettings {
		return pebble.UniformDBCompressionSettings(block.BalancedCompression)
	})
	opts.L0CompactionThreshold = 8
	opts.L0StopWritesThreshold = 24
	opts.LBaseMaxBytes = 512 << 20
	opts.MemTableSize = 64 << 20
	opts.CompactionConcurrencyRange = func() (int, int) { return 4, 8 }
	return opts
}

func getRPCURL() string {
	if url := os.Getenv("RPC_URL"); url != "" {
		return url
	}
	return "https://api.avax.network"
}

// indexerInfo holds an indexer and its chain requirements.
type indexerInfo struct {
	indexer  interface{}
	testable indexer.Testable
	needsP   bool
	needsX   bool
	needsC   bool
}

func main() {
	godotenv.Load()

	// Simple arg parsing: <indexer_name> [--fresh]
	var indexerName string
	fresh := false

	for _, arg := range os.Args[1:] {
		if arg == "--fresh" {
			fresh = true
		} else if !hasPrefix(arg, "-") {
			indexerName = arg
		}
	}

	if indexerName == "" {
		fmt.Println("Usage: go run ./cmd/test <indexer_name> [--fresh]")
		fmt.Println("Available indexers: utxos, pending_rewards, historical_rewards, list_chain_ids")
		os.Exit(1)
	}

	rpcURL := getRPCURL()
	apiAddr := ":8080"
	dataDir := "./data"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create RPC client and get network ID
	rpc := pchain.NewClient(rpcURL)
	networkID, err := rpc.GetNetworkID(ctx)
	if err != nil {
		log.Fatalf("Failed to get network ID: %v", err)
	}
	rpc.SetNetworkID(networkID)

	// Self-tests only work on testnet (fuji)
	if networkID != 5 {
		log.Fatalf("Self-tests only supported on testnet (fuji). Current network ID: %d", networkID)
	}
	log.Printf("Connected to fuji (network ID: %d)", networkID)

	// Data directories
	networkDataDir := filepath.Join(dataDir, fmt.Sprintf("%d", networkID))
	pBlocksDir := filepath.Join(networkDataDir, "blocks", "p")
	xBlocksDir := filepath.Join(networkDataDir, "blocks", "x")
	cBlocksDir := filepath.Join(networkDataDir, "blocks", "c")
	rpcCacheDir := filepath.Join(networkDataDir, "rpc_cache")

	if err := os.MkdirAll(pBlocksDir, 0755); err != nil {
		log.Fatalf("Failed to create P blocks directory: %v", err)
	}
	if err := os.MkdirAll(xBlocksDir, 0755); err != nil {
		log.Fatalf("Failed to create X blocks directory: %v", err)
	}
	if err := os.MkdirAll(cBlocksDir, 0755); err != nil {
		log.Fatalf("Failed to create C blocks directory: %v", err)
	}
	if err := os.MkdirAll(rpcCacheDir, 0755); err != nil {
		log.Fatalf("Failed to create RPC cache directory: %v", err)
	}

	// Create cached client
	cachedRPC, err := pchain.NewCachedClient(rpc, rpcCacheDir)
	if err != nil {
		log.Fatalf("Failed to create cached RPC client: %v", err)
	}
	defer cachedRPC.Close()

	// Build indexer registry
	indexers := map[string]indexerInfo{
		"utxos": {
			indexer:  utxos.New(),
			testable: utxos.New(),
			needsP:   true,
			needsX:   true,
			needsC:   true,
		},
		"pending_rewards": {
			indexer:  pending_rewards.New(cachedRPC),
			testable: pending_rewards.New(cachedRPC),
			needsP:   true,
		},
		"historical_rewards": {
			indexer:  historical_rewards.New(cachedRPC),
			testable: historical_rewards.New(cachedRPC),
			needsP:   true,
		},
		"assets": {
			indexer:  assets.New(),
			testable: assets.New(),
			needsX:   true,
		},
		"blockchains": {
			indexer:  blockchains.New(),
			testable: blockchains.New(),
			needsP:   true,
		},
		"subnets": {
			indexer:  subnets.New(),
			testable: subnets.New(),
			needsP:   true,
		},
		"list_chain_ids": {
			indexer:  list_chain_ids.New(),
			testable: list_chain_ids.New(),
			needsP:   true,
			needsX:   true,
			needsC:   true,
		},
		"network_stats": {
			indexer:  network_stats.NewMonitor(rpc),
			testable: network_stats.NewMonitor(rpc),
			needsP:   false, // Doesn't need block sync, polls node directly
		},
		"validators": {
			indexer:  validators.New(),
			testable: validators.New(),
			needsP:   true,
		},
	}

	info, ok := indexers[indexerName]
	if !ok {
		log.Fatalf("Unknown indexer: %s", indexerName)
	}

	// Fresh mode: drop indexer data
	if fresh {
		indexerDataDir := filepath.Join(networkDataDir, indexerName)
		log.Printf("Dropping %s data: %s", indexerName, indexerDataDir)
		if err := os.RemoveAll(indexerDataDir); err != nil {
			log.Printf("Warning: failed to remove %s: %v", indexerDataDir, err)
		}
	}

	// Open blocks DBs
	pBlocksDB, err := pebble.Open(pBlocksDir, pebbleOpts())
	if err != nil {
		log.Fatalf("Failed to open P-Chain blocks DB: %v", err)
	}
	defer pBlocksDB.Close()

	var xBlocksDB *pebble.DB
	var xFetcher *xchain.Fetcher
	var xRunner *runner.XRunner

	if info.needsX {
		xBlocksDB, err = pebble.Open(xBlocksDir, pebbleOpts())
		if err != nil {
			log.Fatalf("Failed to open X-Chain blocks DB: %v", err)
		}
		defer xBlocksDB.Close()
	}

	var cBlocksDB *pebble.DB
	var cFetcher *cchain.Fetcher
	var cRunner *runner.CRunner

	if info.needsC {
		cBlocksDB, err = pebble.Open(cBlocksDir, pebbleOpts())
		if err != nil {
			log.Fatalf("Failed to open C-Chain blocks DB: %v", err)
		}
		defer cBlocksDB.Close()
	}

	// Create P-Chain runner
	var pIndexers []indexer.PChainIndexer
	if info.needsP {
		if pIdx, ok := info.indexer.(indexer.PChainIndexer); ok {
			pIndexers = append(pIndexers, pIdx)
		}
	}
	pRunner := runner.NewPRunner(pBlocksDB, pIndexers, networkID)

	// Initialize P-Chain
	if err := pRunner.Init(ctx, networkDataDir); err != nil {
		log.Fatalf("Failed to init P-Chain indexer: %v", err)
	}

	// Setup X-Chain if needed
	if info.needsX && xBlocksDB != nil {
		xRPC := xchain.NewClient(rpcURL)
		xFetcher = xchain.NewFetcher(xRPC, xBlocksDB, false) // Fuji

		if xIndexer, ok := info.indexer.(indexer.XChainIndexer); ok {
			xRunner = runner.NewXRunner(xBlocksDB, []indexer.XChainIndexer{xIndexer}, networkID)
			if err := xRunner.Init(ctx, networkDataDir); err != nil {
				log.Fatalf("Failed to init X-Chain indexer: %v", err)
			}
		}
	}

	// Setup C-Chain if needed
	if info.needsC && cBlocksDB != nil {
		// Get C-Chain RPC URL (NewClient adds /ext/bc/C/rpc)
		cRPCURL := os.Getenv("C_RPC_URL")
		if cRPCURL == "" {
			cRPCURL = rpcURL
		}
		cRPC := cchain.NewClient(cRPCURL)

		// Create C-Chain fetcher
		cFetcher = cchain.NewFetcher(cRPC, cBlocksDB)

		// Create C-Chain runner with the utxos indexer as CChainIndexer
		if cIndexer, ok := info.indexer.(indexer.CChainIndexer); ok {
			cRunner = runner.NewCRunner(cBlocksDB, []indexer.CChainIndexer{cIndexer}, networkID)
			if err := cRunner.Init(ctx, networkDataDir); err != nil {
				log.Fatalf("Failed to init C-Chain indexer: %v", err)
			}
		}
	}

	// Get test cases
	tests := info.testable.TestCases()
	if len(tests) == 0 {
		log.Fatalf("No test cases defined for %s", indexerName)
	}
	log.Printf("Loaded %d test cases for %s", len(tests), indexerName)

	// Start fetcher (if not already synced)
	pFetcher := pchain.NewFetcher(rpc, pBlocksDB)

	var wg sync.WaitGroup

	// Start P-Chain fetcher
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("[p-fetcher] starting...")
		if err := pFetcher.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("[p-fetcher] error: %v", err)
		}
	}()

	// Start P-Chain runner
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("[p-runner] starting...")
		if err := pRunner.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("[p-runner] error: %v", err)
		}
	}()

	// Start X-Chain if needed
	if xFetcher != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Println("[x-fetcher] starting pre-Cortina...")
			if err := xFetcher.RunPreCortina(ctx); err != nil && err != context.Canceled {
				log.Printf("[x-fetcher] pre-Cortina error: %v", err)
			}
			log.Println("[x-fetcher] starting blocks...")
			if err := xFetcher.Run(ctx); err != nil && err != context.Canceled {
				log.Printf("[x-fetcher] error: %v", err)
			}
		}()
	}
	if xRunner != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Println("[x-runner] starting pre-Cortina...")
			if err := xRunner.RunPreCortina(ctx); err != nil && err != context.Canceled {
				log.Printf("[x-runner] pre-Cortina error: %v", err)
			}
			log.Println("[x-runner] starting blocks...")
			if err := xRunner.RunBlocks(ctx); err != nil && err != context.Canceled {
				log.Printf("[x-runner] error: %v", err)
			}
		}()
	}

	// Start C-Chain fetcher and runner if needed
	if cFetcher != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Println("[c-fetcher] starting...")
			if err := cFetcher.Run(ctx); err != nil && err != context.Canceled {
				log.Printf("[c-fetcher] error: %v", err)
			}
		}()
	}
	if cRunner != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Println("[c-runner] starting...")
			if err := cRunner.Run(ctx); err != nil && err != context.Canceled {
				log.Printf("[c-runner] error: %v", err)
			}
		}()
	}

	// Start indexer background loop if it exists (e.g. network_stats)
	if loopable, ok := info.indexer.(interface{ Run(context.Context) error }); ok {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Println("[indexer-loop] starting...")
			if err := loopable.Run(ctx); err != nil && err != context.Canceled {
				log.Printf("[indexer-loop] error: %v", err)
			}
		}()
	}

	// HTTP server (needed for API tests)
	mux := http.NewServeMux()
	if idx, ok := info.indexer.(interface{ RegisterRoutes(mux *http.ServeMux) }); ok {
		idx.RegisterRoutes(mux)
	}
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	server := &http.Server{Addr: apiAddr, Handler: mux}
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("[http] listening on %s", apiAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[http] error: %v", err)
		}
	}()

	// Wait for sync then run tests
	go func() {
		// Wait for server ready
		localBase := "http://localhost" + apiAddr
		log.Printf("Waiting for indexer API to be ready at %s...", localBase)
		client := &http.Client{Timeout: 5 * time.Second}
		for {
			resp, err := client.Get(localBase + "/health")
			if err == nil {
				resp.Body.Close()
				break
			}
			time.Sleep(200 * time.Millisecond)
		}

		var lastLog time.Time

		// Wait for P-Chain sync
		if info.needsP {
			log.Println("Waiting for P-Chain to sync...")
			lastLog = time.Now()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				fetched := pFetcher.LatestFetched()
				indexed, err := pRunner.GetGlobalWatermark()
				if err != nil {
					log.Printf("Error getting P-Chain watermark: %v", err)
					time.Sleep(time.Second)
					continue
				}

				if fetched > 0 && indexed > 0 && fetched-indexed <= 5 {
					log.Printf("P-Chain synced: indexed=%d, fetched=%d", indexed, fetched)
					break
				}

				if time.Since(lastLog) > 3*time.Second {
					remaining := int64(fetched) - int64(indexed)
					log.Printf("P-Chain syncing: indexed=%d, fetched=%d, remaining=%d", indexed, fetched, remaining)
					lastLog = time.Now()
				}

				time.Sleep(500 * time.Millisecond)
			}
		}

		// Wait for X-Chain sync if needed
		if info.needsX && xFetcher != nil && xRunner != nil {
			log.Println("Waiting for X-Chain pre-Cortina to sync...")
			lastLog = time.Now()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				if xFetcher.IsPreCortinaDone() {
					// Check if runner also done with pre-Cortina
					wm, err := info.indexer.(indexer.XChainIndexer).GetXChainPreCortinaWatermark()
					count := xFetcher.PreCortinaCount()
					if err == nil && wm+1 >= count {
						log.Printf("X-Chain pre-Cortina synced: %d txs", count)
						break
					}
					if time.Since(lastLog) > 3*time.Second {
						log.Printf("X-Chain pre-Cortina syncing: processed=%d, fetched=%d", wm, count)
						lastLog = time.Now()
					}
				} else if time.Since(lastLog) > 3*time.Second {
					log.Printf("X-Chain pre-Cortina fetching in progress...")
					lastLog = time.Now()
				}
				time.Sleep(time.Second)
			}

			log.Println("Waiting for X-Chain blocks to sync...")
			lastLog = time.Now()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				fetched := xFetcher.LatestFetched()
				indexed, err := xRunner.GetGlobalBlockWatermark()
				if err != nil {
					log.Printf("Error getting X-Chain watermark: %v", err)
					time.Sleep(time.Second)
					continue
				}

				if fetched > 0 && indexed >= fetched {
					log.Printf("X-Chain blocks synced: indexed=%d, fetched=%d", indexed, fetched)
					break
				}

				if time.Since(lastLog) > 3*time.Second {
					remaining := int64(fetched) - int64(indexed)
					log.Printf("X-Chain blocks syncing: indexed=%d, fetched=%d, remaining=%d", indexed, fetched, remaining)
					lastLog = time.Now()
				}

				time.Sleep(500 * time.Millisecond)
			}
		}

		// Wait for C-Chain sync if needed (only up to target block for this test)
		if info.needsC && cFetcher != nil && cRunner != nil {
			// The cross-chain UTXO in our test was created at C-Chain block 48746327
			targetCBlock := uint64(48746327)
			log.Printf("Waiting for C-Chain to sync to block %d...", targetCBlock)
			cSyncStart := time.Now()
			cStartIndexed, err := cRunner.GetGlobalWatermark()
			if err != nil {
				log.Fatalf("Failed to get initial C-Chain watermark: %v", err)
			}
			lastLog = time.Now()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				indexed, err := cRunner.GetGlobalWatermark()
				if err != nil {
					log.Printf("Error getting C-Chain watermark: %v", err)
					time.Sleep(time.Second)
					continue
				}

				if indexed >= targetCBlock {
					log.Printf("C-Chain synced to target: indexed=%d", indexed)
					break
				}

				if time.Since(lastLog) > 3*time.Second {
					remaining := int64(targetCBlock) - int64(indexed)
					elapsed := time.Since(cSyncStart).Seconds()
					processed := indexed - cStartIndexed
					rate := float64(processed) / elapsed
					etaSec := float64(remaining) / rate
					log.Printf("C-Chain syncing: indexed=%d, target=%d, remaining=%d | %.0f blk/s | ETA %.0fs",
						indexed, targetCBlock, remaining, rate, etaSec)
					lastLog = time.Now()
				}

				time.Sleep(500 * time.Millisecond)
			}
		}

		// Run tests
		fmt.Printf("\n=== Running %s tests ===\n\n", indexerName)
		passed := selftest.RunTests(localBase, tests)

		// Clean shutdown
		server.Close()
		cancel()

		if !passed {
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	wg.Wait()
	log.Println("Done")
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
