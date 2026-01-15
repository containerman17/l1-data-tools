# Indexer Architecture

## Core Idea

3 chain interfaces. Each indexer implements 1, 2, or all 3 depending on what chains it needs.

```go
type PChainIndexer interface {
    Name() string
    Init(ctx context.Context, baseDir string, networkID uint32) error
    ProcessPChainBatch(ctx context.Context, blocks []PBlock) error
    GetPChainWatermark() (uint64, error)
    RegisterRoutes(mux *http.ServeMux)
}

type XChainIndexer interface {
    Name() string
    Init(ctx context.Context, baseDir string, networkID uint32) error
    // Pre-Cortina: transactions only (no blocks, sequential index)
    ProcessXChainPreCortinaTxs(ctx context.Context, txs []XTx) error
    GetXChainPreCortinaWatermark() (uint64, error)  // returns last processed tx index
    // Post-Cortina: blocks
    ProcessXChainBlocks(ctx context.Context, blocks []XBlock) error
    GetXChainBlockWatermark() (uint64, error)  // returns last processed block height
    RegisterRoutes(mux *http.ServeMux)
}

type CChainIndexer interface {
    Name() string
    Init(ctx context.Context, baseDir string, networkID uint32) error
    ProcessCChainBatch(ctx context.Context, blocks []CBlock) error
    GetCChainWatermark() (uint64, error)
    RegisterRoutes(mux *http.ServeMux)
}
```

## Dependency Injection

RPC client needed by some indexers. Inject via constructor:

```go
import (
    "project/indexers/utxos"
    "project/indexers/pending_rewards"
    "project/indexers/historical_rewards"
)

// utxos needs RPC for reward UTXOs and cross-chain info
u := utxos.New(cachedRPC)

// rewards indexers need RPC for validators/reward data
pr := pending_rewards.New(cachedRPC)
hr := historical_rewards.New(cachedRPC)
```

Indexers store what they need internally. Interface stays clean (no RPC in Init).

## Wiring (No Registration Pattern)

Explicit in main. No global state. No magic init().

```go
// main.go
func main() {
    // Create indexers (inject dependencies via constructor)
    u := utxos.New(cachedRPC)           // implements P, X, C
    pr := pending_rewards.New(cachedRPC) // implements P only
    hr := historical_rewards.New(cachedRPC) // implements P only
    
    // Build slices explicitly
    pIndexers := []indexer.PChainIndexer{u, pr, hr}
    xIndexers := []indexer.XChainIndexer{u}
    cIndexers := []indexer.CChainIndexer{u}
    
    // Create runners
    pRunner := runner.NewPRunner(pBlocksDB, pIndexers, networkID)
    xRunner := runner.NewXRunner(xBlocksDB, xIndexers, networkID)
    cRunner := runner.NewCRunner(cBlocksDB, cIndexers, networkID)
    
    // Init all (sync.Once in UTXOs handles duplicate calls)
    pRunner.Init(ctx, networkDataDir)
    xRunner.Init(ctx, networkDataDir)
    cRunner.Init(ctx, networkDataDir)
    
    // Register routes
    for _, idx := range pIndexers {
        idx.RegisterRoutes(mux)
    }
    
    // Run all 3 runners
    go pRunner.Run(ctx)
    go xRunner.RunPreCortina(ctx)
    go xRunner.RunBlocks(ctx)
    go cRunner.Run(ctx)
}
```

**Why explicit wins:**
- See exactly what's registered, in what order
- No blank imports
- Easy to skip an indexer during testing/debugging
- Pass dependencies via constructor (RPC, config, etc.)

## Cross-Chain Indexers (sync.Once)

UTXOs implements all 3 interfaces but has ONE database.

```go
// indexers/utxos/utxos.go
type UTXOs struct {
    db       *pebble.DB
    rpc      *pchain.CachedClient
    initOnce sync.Once
    initErr  error
}

func New(rpc *pchain.CachedClient) *UTXOs {
    return &UTXOs{rpc: rpc}
}

func (u *UTXOs) Init(ctx context.Context, baseDir string, networkID uint32) error {
    u.initOnce.Do(func() {
        u.db, u.initErr = pebble.Open(baseDir, nil)
    })
    return u.initErr
}

// Called 3x (P, X, C runners), but DB opens once
```

## Directory Structure

```
data/{network}/
├── blocks/
│   ├── p/              # P-chain blocks (fetcher output)
│   ├── x/              # X-chain blocks
│   └── c/              # C-chain blocks
├── utxos/              # ONE shared DB for all chains
├── validators/         # P-chain only
├── rewards/            # P-chain only
└── ...
```

Nuke `data/utxos/` → rebuilds UTXOs for all 3 chains.
Nuke `data/validators/` → only rebuilds P-chain validators.

## File Organization

Package per indexer inside `indexers/` folder:

```
indexer/
  api.go              # Interfaces + block types (no registration)

indexers/
  utxos/
    utxos.go          # struct + New() + Init() with sync.Once
    store.go          # DB: encode/decode, save, load
    indexing.go       # P-chain logic + X/C stubs
    api.go            # HTTP: GET /utxos, /balances

  pending_rewards/
    pending_rewards.go
    indexing.go       # Cache busting on staking changes
    api.go            # HTTP: GET /rewards:listPending

  historical_rewards/
    historical_rewards.go
    indexing.go       # Track staking txs and rewards
    api.go            # HTTP: GET /rewards

runner/
  p_runner.go         # Feeds []PChainIndexer
  x_runner.go         # Feeds []XChainIndexer (pre-Cortina + blocks)
  c_runner.go         # Feeds []CChainIndexer
```

Each file 150-300 lines, does one thing.

## Cross-Chain UTXOs

Single `utxos/` DB shared by P, X, C handlers.

```go
type StoredUTXO struct {
    TxID                 ids.ID
    OutputIndex          uint32
    Amount               uint64
    AssetID              ids.ID
    Addresses            []ids.ShortID
    Locktime             uint64
    Threshold            uint32
    Staked               bool
    BlockHeight          uint64
    BlockTimestamp       int64
    ConsumingTxID        ids.ID    // Empty if unspent
    ConsumingBlockHeight uint64
    StakeStartTime       int64
    StakeEndTime         int64
    UTXOType             string    // "TRANSFER" or "STAKEABLE_LOCK"
    CreatedOnChainID     string    // Where born (differs for cross-chain)
}
```

**ExportTx (X→P):**
- Create UTXO with `CreatedOnChainID = X-chain-id`

**ImportTx (P from X):**
- Set `ConsumingTxID`, `ConsumingBlockHeight`

No "atomic memory" abstraction. One query for balance. No cross-DB joins.

## Runners

```go
// runner/p_runner.go
type PRunner struct {
    blocksDB  *pebble.DB
    indexers  []indexer.PChainIndexer
    networkID uint32
}

func NewPRunner(blocksDB *pebble.DB, indexers []PChainIndexer, networkID uint32) *PRunner
func (r *PRunner) Init(ctx context.Context, baseDir string) error
func (r *PRunner) Run(ctx context.Context) error

// runner/x_runner.go
type XRunner struct { ... }
func NewXRunner(blocksDB *pebble.DB, indexers []XChainIndexer, networkID uint32) *XRunner
func (r *XRunner) Init(ctx context.Context, baseDir string) error
func (r *XRunner) RunPreCortina(ctx context.Context) error  // Pre-Cortina txs
func (r *XRunner) RunBlocks(ctx context.Context) error      // Post-Cortina blocks

// runner/c_runner.go
type CRunner struct { ... }
func NewCRunner(blocksDB *pebble.DB, indexers []CChainIndexer, networkID uint32) *CRunner
func (r *CRunner) Init(ctx context.Context, baseDir string) error
func (r *CRunner) Run(ctx context.Context) error
```

3 runners (X has 2 modes), each feeds its indexer slice. Runners handle:
- Reading from blocks DB
- Batching
- Watermark tracking per indexer
- Parallel block parsing (P-chain)
