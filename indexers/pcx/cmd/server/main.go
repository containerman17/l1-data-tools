package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"

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
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/xchain"
	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/sstable/block"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
)

func pebbleOpts() *pebble.Options {
	opts := &pebble.Options{
		Logger: db.QuietLogger(),
	}
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

func main() {
	godotenv.Load()

	rpcURL := flag.String("rpc", getRPCURL(), "Avalanche RPC URL")
	apiAddr := flag.String("api", ":8080", "API server address")
	dataDir := flag.String("data", "./data", "Data directory")
	fresh := flag.String("fresh", "", "Drop indexes: 'all' or indexer name (e.g., 'pending_rewards')")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create RPC client early to get network ID
	rpc := pchain.NewClient(*rpcURL)

	// Get network ID and create network-specific data directory
	networkID, err := rpc.GetNetworkID(ctx)
	if err != nil {
		log.Fatalf("Failed to get network ID: %v", err)
	}
	rpc.SetNetworkID(networkID)
	networkName := "unknown"
	switch networkID {
	case 1:
		networkName = "mainnet"
	case 5:
		networkName = "fuji"
	}
	log.Printf("Connected to %s (network ID: %d)", networkName, networkID)

	// Data directory: ./data/{networkID}/
	networkDataDir := filepath.Join(*dataDir, formatUint(uint64(networkID)))
	pBlocksDir := filepath.Join(networkDataDir, "blocks", "p")
	xBlocksDir := filepath.Join(networkDataDir, "blocks", "x")
	cBlocksDir := filepath.Join(networkDataDir, "blocks", "c")
	rpcCacheDir := filepath.Join(networkDataDir, "rpc_cache")

	if err := os.MkdirAll(pBlocksDir, 0755); err != nil {
		log.Fatalf("Failed to create P-Chain blocks directory: %v", err)
	}
	if err := os.MkdirAll(xBlocksDir, 0755); err != nil {
		log.Fatalf("Failed to create X-Chain blocks directory: %v", err)
	}
	if err := os.MkdirAll(cBlocksDir, 0755); err != nil {
		log.Fatalf("Failed to create C-Chain blocks directory: %v", err)
	}
	if err := os.MkdirAll(rpcCacheDir, 0755); err != nil {
		log.Fatalf("Failed to create RPC cache directory: %v", err)
	}

	// Create cached client for indexers
	cachedRPC, err := pchain.NewCachedClient(rpc, rpcCacheDir)
	if err != nil {
		log.Fatalf("Failed to create cached RPC client: %v", err)
	}
	defer cachedRPC.Close()

	// Handle --fresh flag
	if *fresh != "" {
		if *fresh == "all" {
			log.Println("Fresh mode: dropping ALL indexes...")
			entries, _ := os.ReadDir(networkDataDir)
			for _, entry := range entries {
				if entry.Name() != "blocks" && entry.Name() != "rpc_cache" {
					os.RemoveAll(filepath.Join(networkDataDir, entry.Name()))
				}
			}
		} else {
			log.Printf("Fresh mode: dropping %s index...", *fresh)
			os.RemoveAll(filepath.Join(networkDataDir, *fresh))
		}
	}

	// Open block databases
	pBlocksDB, err := pebble.Open(pBlocksDir, pebbleOpts())
	if err != nil {
		log.Fatalf("Failed to open P-Chain blocks DB: %v", err)
	}
	defer pBlocksDB.Close()

	xBlocksDB, err := pebble.Open(xBlocksDir, pebbleOpts())
	if err != nil {
		log.Fatalf("Failed to open X-Chain blocks DB: %v", err)
	}
	defer xBlocksDB.Close()

	cBlocksDB, err := pebble.Open(cBlocksDir, pebbleOpts())
	if err != nil {
		log.Fatalf("Failed to open C-Chain blocks DB: %v", err)
	}
	defer cBlocksDB.Close()

	// ============ Create Indexers ============
	utxosIndexer := utxos.New()                                   // implements P, X, C (rewards fetched by runner)
	pendingRewardsIndexer := pending_rewards.New(cachedRPC)       // implements P only
	historicalRewardsIndexer := historical_rewards.New(cachedRPC) // implements P only
	assetsIndexer := assets.New()                                 // implements X mainly
	blockchainsIndexer := blockchains.New()                       // implements P mainly
	subnetsIndexer := subnets.New()                               // implements P mainly
	validatorsIndexer := validators.New()                         // implements P only
	listChainIdsIndexer := list_chain_ids.New()                   // implements P, X, C
	networkMonitor := network_stats.NewMonitor(rpc)               // background network stats

	// Build explicit slices
	pIndexers := []indexer.PChainIndexer{
		utxosIndexer,
		pendingRewardsIndexer,
		historicalRewardsIndexer,
		blockchainsIndexer,
		subnetsIndexer,
		validatorsIndexer,
		listChainIdsIndexer,
	}
	xIndexers := []indexer.XChainIndexer{
		utxosIndexer,
		assetsIndexer,
		listChainIdsIndexer,
	}
	cIndexers := []indexer.CChainIndexer{
		utxosIndexer,
		listChainIdsIndexer,
	}

	// Create runners
	pRunner := runner.NewPRunner(pBlocksDB, pIndexers, networkID)
	xRunner := runner.NewXRunner(xBlocksDB, xIndexers, networkID)
	cRunner := runner.NewCRunner(cBlocksDB, cIndexers, networkID)

	// Initialize all indexers
	if err := pRunner.Init(ctx, networkDataDir); err != nil {
		log.Fatalf("Failed to init P-chain indexers: %v", err)
	}
	if err := xRunner.Init(ctx, networkDataDir); err != nil {
		log.Fatalf("Failed to init X-chain indexers: %v", err)
	}
	if err := cRunner.Init(ctx, networkDataDir); err != nil {
		log.Fatalf("Failed to init C-chain indexers: %v", err)
	}

	// Log registered APIs
	log.Printf("Registered indexers:")
	log.Printf("  P-Chain: %d indexers", len(pIndexers))
	log.Printf("  X-Chain: %d indexers", len(xIndexers))
	log.Printf("  C-Chain: %d indexers", len(cIndexers))

	var wg sync.WaitGroup

	// Setup HTTP mux and register routes
	mux := http.NewServeMux()
	// Register all unique indexers
	uniqueIndexers := []interface{ RegisterRoutes(mux *http.ServeMux) }{
		utxosIndexer,
		pendingRewardsIndexer,
		historicalRewardsIndexer,
		assetsIndexer,
		blockchainsIndexer,
		subnetsIndexer,
		validatorsIndexer,
		listChainIdsIndexer,
		networkMonitor,
	}
	for _, idx := range uniqueIndexers {
		idx.RegisterRoutes(mux)
	}

	// Start P-Chain block fetcher
	pFetcher := pchain.NewFetcher(rpc, pBlocksDB)
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("[p-fetcher] starting...")
		if err := pFetcher.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("[p-fetcher] error: %v", err)
		}
	}()

	// Start X-Chain block fetcher
	xRPC := xchain.NewClient(*rpcURL)
	xFetcher := xchain.NewFetcher(xRPC, xBlocksDB, networkID == 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("[x-fetcher] starting...")
		if err := xFetcher.RunPreCortina(ctx); err != nil && err != context.Canceled {
			log.Printf("[x-fetcher] pre-Cortina error: %v", err)
		}
		if err := xFetcher.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("[x-fetcher] error: %v", err)
		}
	}()

	// Start C-Chain block fetcher
	cRPC := cchain.NewClient(*rpcURL)
	cFetcher := cchain.NewFetcher(cRPC, cBlocksDB)
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("[c-fetcher] starting...")
		if err := cFetcher.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("[c-fetcher] error: %v", err)
		}
	}()

	// Start P-chain runner
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("[p-runner] starting...")
		if err := pRunner.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("[p-runner] error: %v", err)
		}
	}()

	// Start X-chain runner (pre-Cortina first, then blocks)
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

	// Start C-chain runner
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("[c-runner] starting...")
		if err := cRunner.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("[c-runner] error: %v", err)
		}
	}()

	// Start Network Stats monitor
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("[network-monitor] starting...")
		if err := networkMonitor.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("[network-monitor] error: %v", err)
		}
	}()

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	// Status endpoint
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		pWatermark, _ := pRunner.GetGlobalWatermark()
		xWatermark, _ := xRunner.GetGlobalBlockWatermark()
		cWatermark, _ := cRunner.GetGlobalWatermark()

		w.Write([]byte(`{"status":"running","pchain":{"latestFetched":` +
			formatUint(pFetcher.LatestFetched()) +
			`,"latestIndexed":` +
			formatUint(pWatermark) +
			`},"xchain":{"latestFetched":` +
			formatUint(xFetcher.LatestFetched()) +
			`,"latestIndexed":` +
			formatUint(xWatermark) +
			`},"cchain":{"latestFetched":` +
			formatUint(cFetcher.LatestFetched()) +
			`,"latestIndexed":` +
			formatUint(cWatermark) +
			`}}`))
	})

	server := &http.Server{Addr: *apiAddr, Handler: mux}

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("[http] listening on %s", *apiAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[http] error: %v", err)
		}
	}()

	<-ctx.Done()
	server.Close()
	wg.Wait()
	log.Println("Shutdown complete")
}

func formatUint(n uint64) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
