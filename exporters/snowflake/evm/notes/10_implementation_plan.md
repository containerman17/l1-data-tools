# Implementation Plan: Snowflake EVM Exporter

## Overview
Long-running daemon that exports EVM blocks from ingestion service to Snowflake in 1,000-block batches. Uses smart rate limiting to avoid trickle inserts while catching up quickly when behind.

**Key behaviors:**
- **Catching up mode**: When behind by 1000+ blocks, runs continuously until caught up
- **Steady state mode**: When near tip (partial batch), waits 1 hour before next run
- **Error recovery**: On any error, logs and retries after 5-minute backoff

---

## Implementation Status: ✅ COMPLETE (Daemon Mode)

All core functionality has been implemented as of January 15, 2026.

### Files Delivered

1. **`exporters/snowflake/evm/pkg/snowflake/client.go`** (108 lines)
   - Snowflake connection handling via DSN
   - `GetLastBlock()` - Queries `MAX(BLOCKNUMBER)` from blocks table
   - Connection pool management with Ping validation

2. **`exporters/snowflake/evm/pkg/snowflake/writer.go`** (486 lines)
   - `WriteBatch()` - Atomic transaction writer for all 6 tables
   - Array binding for Blocks, Transactions, Receipts, Logs, Internal Transactions, Messages
   - Transaction rollback on error

3. **`exporters/snowflake/evm/cmd/exporter/main.go`**
   - Full CLI with environment variable configuration
   - **Daemon mode** with smart rate limiting
   - Startup detection from Snowflake blocks table
   - Block streaming with context cancellation
   - Signal handling (SIGINT, SIGTERM)
   - Error recovery with 5-minute backoff
   - Comprehensive logging with batch counts and statistics

4. **`exporters/snowflake/evm/README.md`**
   - Usage instructions
   - Environment variable reference
   - Daemon deployment example

### Build Verification

```bash
go build -o /tmp/snowflake-evm-exporter ./exporters/snowflake/evm/cmd/exporter/
# Output: 44MB binary
```

Runtime validation confirmed - properly validates all required environment variables.

---

---

## Pre-requisites

**MUST FIX FIRST:** Transform package has 2 failing tests (transactions, internal_txs - precision issues). These must pass before implementing the exporter to ensure data correctness.

---

## Behavior Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           DAEMON LOOP                                    │
├─────────────────────────────────────────────────────────────────────────┤
│  1. Get last exported block from Snowflake                              │
│  2. Get latest available from ingestion service                          │
│  3. Fetch up to 1000 blocks                                             │
│  4. Transform to export format                                           │
│  5. Write atomically to Snowflake                                        │
│  6. Decide next action:                                                  │
│     - Full batch (1000 blocks)? → Immediate next run (catching up)      │
│     - Partial batch (<1000)?    → Wait 1 hour (steady state)            │
│     - Already caught up?        → Wait 1 hour                            │
│     - Any error?                → Wait 5 minutes (backoff)              │
└─────────────────────────────────────────────────────────────────────────┘
```

### Rate Limiting Logic

| Scenario | Wait Time | Rationale |
|----------|-----------|-----------|
| Full batch (1000 blocks) | 0 | Still catching up, continue immediately |
| Partial batch (<1000 blocks) | 1 hour | Near chain tip, avoid trickle inserts |
| Already caught up (0 blocks) | 1 hour | Wait for more blocks to accumulate |
| Any error | 5 minutes | Backoff before retry |

**Environment Variables for Tuning:**
- `PARTIAL_BATCH_WAIT` - Wait time after partial batch (default: 1h)
- `ERROR_BACKOFF` - Wait time after error (default: 5m)

### 1. Startup: Get Last Exported Block

Query only the `blocks` table to find the last exported block:

```sql
SELECT COALESCE(MAX(block_number), -1) FROM ${prefix}blocks;
```

**Why only blocks table?**
- Empty blocks exist (0 transactions, 0 logs)
- Other tables may have lower MAX values - this is expected, not an error
- All tables are written atomically, so if blocks has block N, all related data for block N is present

**Result:** `fromBlock = MAX + 1` (or 0 if table is empty)

### 2. Get Latest Ingested Block

Call `client.Info()` to get `latestBlock` from ingestion service:
```go
info, err := client.Info(ctx)
latestBlock := info.LatestBlock
```

If `fromBlock > latestBlock`, exit cleanly (already caught up).

### 3. Fetch 1,000 Blocks

- Create child context with cancel: `ctx, cancel := context.WithCancel(parentCtx)`
- Target block: `targetBlock = min(fromBlock + 999, latestBlock)`
- Connect and stream:

```go
var batch []*rpc.NormalizedBlock

err := client.Stream(ctx, fromBlock, func(blocks []client.Block) error {
    for _, b := range blocks {
        batch = append(batch, b.Data)
        if b.Number >= targetBlock {
            cancel() // Stop streaming
            return nil
        }
    }
    return nil
})

// Context cancellation returns ctx.Err(), which is expected
if err != nil && !errors.Is(err, context.Canceled) {
    return err
}
```

### 4. Transform Batch

Use existing `transform.Transform()` for each block:

```go
var allBatches []*transform.ExportBatch
for _, block := range batch {
    exported, err := transform.Transform(block)
    if err != nil {
        return fmt.Errorf("transform block %d: %w", block.Block.Number, err)
    }
    allBatches = append(allBatches, exported)
}
```

### 5. Atomic Write Using Array Binding

**Key insight:** Snowflake Go driver supports array binding via `sf.Array()`. No CSV files needed.

```go
// Prepare column slices
blockNumbers := make([]int64, 0)
blockHashes := make([]string, 0)
// ... etc for all columns

for _, batch := range allBatches {
    for _, row := range batch.Blocks {
        blockNumbers = append(blockNumbers, row.BlockNumber)
        blockHashes = append(blockHashes, row.BlockHash)
        // ... etc
    }
}

// Begin transaction
tx, err := db.BeginTx(ctx, nil)
if err != nil {
    return err
}
defer tx.Rollback()

// Insert blocks
_, err = tx.ExecContext(ctx, 
    `INSERT INTO `+prefix+`blocks (block_number, block_hash, ...) VALUES (?, ?, ...)`,
    sf.Array(&blockNumbers),
    sf.Array(&blockHashes),
    // ... etc
)
if err != nil {
    return fmt.Errorf("insert blocks: %w", err)
}

// Insert transactions, receipts, logs, internal_txs, messages...
// (same pattern for each table)

// Commit all 6 tables atomically
if err := tx.Commit(); err != nil {
    return fmt.Errorf("commit transaction: %w", err)
}
```

**Empty blocks:** Always inserted (will have 0 rows in other tables for that block).

**Duplicate handling:** INSERT will fail on PK conflict - this surfaces configuration errors immediately rather than silently corrupting data.

### 6. Daemon Loop with Rate Limiting

```go
for {
    // Refresh latest block from ingestion
    info, err := client.Info(ctx)
    if err != nil {
        log.Printf("Error getting ingestion info: %v, retrying in %v", err, cfg.ErrorBackoff)
        sleep(ctx, cfg.ErrorBackoff)
        continue
    }
    
    if fromBlock > info.LatestBlock {
        log.Printf("Caught up to block %d, waiting %v", info.LatestBlock, cfg.PartialBatchWait)
        sleep(ctx, cfg.PartialBatchWait)
        continue
    }
    
    // Fetch, transform, write up to 1,000 blocks...
    blocksWritten, err := processNextBatch(ctx, ...)
    if err != nil {
        log.Printf("Batch failed: %v, retrying in %v", err, cfg.ErrorBackoff)
        sleep(ctx, cfg.ErrorBackoff)
        continue
    }
    
    // Rate limiting decision
    if blocksWritten < cfg.BatchSize {
        // Partial batch = near chain tip, wait before trickle insert
        log.Printf("Partial batch (%d blocks), waiting %v", blocksWritten, cfg.PartialBatchWait)
        sleep(ctx, cfg.PartialBatchWait)
    }
    // Full batch = catching up, loop immediately
    
    fromBlock += int64(blocksWritten)
}
```

---

## CLI Interface

```bash
snowflake-evm-exporter
```

No command-line flags. All configuration via environment variables.

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `SNOWFLAKE_ACCOUNT` | Yes | Snowflake account identifier |
| `SNOWFLAKE_USER` | Yes | Snowflake service account username |
| `SNOWFLAKE_PRIVATE_KEY` | Yes | Base64-encoded RSA private key (PEM format) |
| `SNOWFLAKE_DATABASE` | Yes | Target database |
| `SNOWFLAKE_SCHEMA` | Yes | Target schema |
| `SNOWFLAKE_WAREHOUSE` | Yes | Compute warehouse |
| `SNOWFLAKE_ROLE` | No | Role to use (uses default if not set) |
| `SNOWFLAKE_TABLE_PREFIX` | Yes | Table prefix (e.g., "cchain_", "dfk_") |
| `INGESTION_URL` | Yes | Address of ingestion service (e.g., "localhost:9090") |
| `BATCH_SIZE` | No | Blocks per transaction (default: 1000) |
| `PARTIAL_BATCH_WAIT` | No | Wait time after partial batch (default: 1h) |
| `ERROR_BACKOFF` | No | Wait time after error (default: 5m) |

### Authentication

The exporter uses **Key Pair Authentication** (RSA), the industry standard for Snowflake service accounts/daemons.

**Setup:**
1. Generate RSA key pair: `openssl genrsa 2048 | openssl pkcs8 -topk8 -nocrypt -out key.pem`
2. Extract public key: `openssl rsa -in key.pem -pubout -out key.pub`
3. Register with Snowflake: `ALTER USER svc_user SET RSA_PUBLIC_KEY='...'`
4. Base64-encode for env var: `cat key.pem | base64 -w0`

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Clean shutdown via SIGINT/SIGTERM |
| 1 | Configuration error (missing env vars) |

**Note:** The daemon runs until terminated. It handles all operational errors (Snowflake, ingestion, transformation) internally with retry logic.

---

## Package Structure

```
exporters/snowflake/evm/
├── cmd/
│   └── exporter/
│       └── main.go           # CLI entry point
├── pkg/
│   ├── transform/            # Existing transform package
│   │   ├── transform.go
│   │   ├── types.go
│   │   └── ...
│   └── snowflake/            # NEW: Snowflake client
│       ├── client.go         # Connection wrapper
│       ├── writer.go         # Batch writer with array binding
│       └── writer_test.go
└── notes/
    └── ...
```

### Key Components

#### 1. `snowflake.Client`
```go
type Client struct {
    db     *sql.DB
    prefix string
}

func New(cfg Config) (*Client, error)
func (c *Client) GetLastBlock(ctx context.Context) (int64, error)
func (c *Client) WriteBatch(ctx context.Context, batches []*transform.ExportBatch) error
func (c *Client) Close() error
```

#### 2. Main Daemon Loop
```go
func Run(ctx context.Context, cfg Config) error {
    // Connect to Snowflake
    sfClient, err := snowflake.New(cfg.Snowflake)
    if err != nil {
        return fmt.Errorf("snowflake connect: %w", err)
    }
    defer sfClient.Close()

    // Create ingestion client
    ingClient := client.NewClient(cfg.IngestionURL)

    for {
        blocksWritten, err := runOneBatch(ctx, sfClient, ingClient, cfg)
        if err != nil {
            // Recoverable error - log and retry after backoff
            log.Printf("Batch error: %v, retrying in %v", err, cfg.ErrorBackoff)
            if !sleep(ctx, cfg.ErrorBackoff) {
                return nil // Context cancelled, clean shutdown
            }
            continue
        }

        // Rate limiting based on batch size
        if blocksWritten < cfg.BatchSize {
            // Partial batch or caught up - wait before next attempt
            log.Printf("Partial batch (%d blocks), waiting %v", blocksWritten, cfg.PartialBatchWait)
            if !sleep(ctx, cfg.PartialBatchWait) {
                return nil // Context cancelled, clean shutdown
            }
        }
        // Full batch = catching up, loop immediately
    }
}

func runOneBatch(ctx context.Context, sfClient *snowflake.Client, ingClient *client.Client, cfg Config) (int, error) {
    // 1. Get last exported block
    lastBlock, err := sfClient.GetLastBlock(ctx)
    if err != nil {
        return 0, fmt.Errorf("get last block: %w", err)
    }
    fromBlock := lastBlock + 1

    // 2. Get latest available
    info, err := ingClient.Info(ctx)
    if err != nil {
        return 0, fmt.Errorf("get ingestion info: %w", err)
    }
    if fromBlock > int64(info.LatestBlock) {
        log.Printf("Caught up: exported through block %d", lastBlock)
        return 0, nil // No blocks to write
    }

    // 3. Calculate batch range
    targetBlock := min(fromBlock+int64(cfg.BatchSize)-1, int64(info.LatestBlock))
    log.Printf("Exporting blocks %d-%d", fromBlock, targetBlock)

    // 4. Stream blocks
    blocks, err := fetchBlocks(ctx, ingClient, uint64(fromBlock), uint64(targetBlock))
    if err != nil {
        return 0, fmt.Errorf("fetch blocks: %w", err)
    }

    // 5. Transform
    batch := transform.Transform(blocks)

    // 6. Write atomically
    if err := sfClient.WriteBatch(ctx, batch); err != nil {
        return 0, fmt.Errorf("write batch: %w", err)
    }

    blocksWritten := len(blocks)
    log.Printf("Wrote %d blocks (%d txs, %d logs)", 
        blocksWritten, len(batch.Transactions), len(batch.Logs))
    
    return blocksWritten, nil
}

// sleep returns true if duration elapsed, false if context was cancelled
func sleep(ctx context.Context, d time.Duration) bool {
    select {
    case <-time.After(d):
        return true
    case <-ctx.Done():
        return false
    }
}
```

---

## Data Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                           STARTUP                                    │
├─────────────────────────────────────────────────────────────────────┤
│  Snowflake: SELECT MAX(block_number) FROM ${prefix}blocks          │
│  Result: lastBlock = 18128463                                        │
│  → fromBlock = 18128464                                              │
├─────────────────────────────────────────────────────────────────────┤
│  Ingestion: GET /info                                                │
│  Result: latestBlock = 18138500                                      │
│  → Need to export 10,036 blocks                                      │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         BATCH LOOP                                   │
├─────────────────────────────────────────────────────────────────────┤
│  Iteration 1: blocks 18128464-18129463 (1000 blocks)                │
│  Iteration 2: blocks 18129464-18130463 (1000 blocks)                │
│  ...                                                                 │
│  Iteration 11: blocks 18138464-18138500 (37 blocks)                 │
│  → Exit: caught up                                                   │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      PER-BATCH FLOW                                  │
├─────────────────────────────────────────────────────────────────────┤
│  1. WebSocket: ws://ingestion/ws?from=18128464                      │
│     → Receive ~100 blocks per frame                                  │
│     → Cancel context when block 18129463 received                    │
│                                                                      │
│  2. Transform each NormalizedBlock → ExportBatch                     │
│     → 1000 BlockRows                                                 │
│     → ~30,000 TransactionRows                                        │
│     → ~30,000 ReceiptRows                                            │
│     → ~100,000 LogRows                                               │
│     → ~50,000 InternalTxRows                                         │
│     → ~30,000 MessageRows                                            │
│                                                                      │
│  3. Snowflake Transaction:                                           │
│     BEGIN;                                                           │
│     INSERT INTO blocks ... (array binding)                           │
│     INSERT INTO transactions ... (array binding)                     │
│     INSERT INTO receipts ... (array binding)                         │
│     INSERT INTO logs ... (array binding)                             │
│     INSERT INTO internal_txs ... (array binding)                     │
│     INSERT INTO messages ... (array binding)                         │
│     COMMIT;                                                          │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Implementation Phases

### Phase 0: Fix Transform Tests (MUST DO FIRST)
- [ ] Fix precision issues in transactions transform
- [ ] Fix precision issues in internal_txs transform
- [ ] All 6 transform tests passing

**Note:** Transform tests are currently skipped when no chain is configured. Precision issues need investigation.

### Phase 1: Snowflake Client ✅ COMPLETE
- [x] Implement `snowflake.Client` with connection handling
- [x] Implement `GetLastBlock()` query
- [x] Implement `WriteBatch()` with array binding
- [ ] Add unit tests with mocked DB

**Status:** All core functionality implemented in `pkg/snowflake/client.go` and `pkg/snowflake/writer.go`.

### Phase 2: Export Loop ✅ COMPLETE
- [x] Implement block fetching with context cancellation
- [x] Implement main export loop
- [x] Add logging (block ranges, row counts, timing)

**Status:** Full export loop implemented in `cmd/exporter/main.go`.

### Phase 3: CLI ✅ COMPLETE
- [x] Load all config from environment variables
- [x] Implement main.go with signal handling
- [ ] Integration test with real Snowflake (manual)

**Status:** CLI fully functional with environment variable configuration and signal handling.

### Additional Tasks Completed
- [x] Added Snowflake Go driver dependency
- [x] Created README.md with usage instructions
- [x] Verified successful build and runtime validation

---

## Snowflake DDL (Reference)

Tables must be created before running the exporter. Example DDL:

```sql
CREATE TABLE ${prefix}blocks (
    block_number BIGINT NOT NULL,
    block_hash VARCHAR(66) NOT NULL,
    block_timestamp BIGINT NOT NULL,
    block_base_fee_per_gas VARCHAR(78),
    block_gas_limit BIGINT NOT NULL,
    block_gas_used BIGINT NOT NULL,
    block_parent_hash VARCHAR(66) NOT NULL,
    block_receipts_root VARCHAR(66) NOT NULL,
    block_size BIGINT NOT NULL,
    block_state_root VARCHAR(66) NOT NULL,
    block_transaction_len BIGINT NOT NULL,
    block_extra_data TEXT,
    partition_date DATE NOT NULL,
    PRIMARY KEY (block_number)
);

-- Similar for transactions, receipts, logs, internal_txs, messages
-- See types.go for full column lists
```

---

## Error Handling

| Scenario | Behavior |
|----------|----------|
| Snowflake connection fails on startup | Exit with error (configuration issue) |
| Snowflake query/write fails during operation | Log error, wait 5 min, retry |
| Ingestion service unavailable | Log error, wait 5 min, retry |
| Transform error | Log error, wait 5 min, retry |
| Transaction commit fails | Rollback (automatic), log, wait 5 min, retry |
| Duplicate PK on insert | Transaction fails, log, wait 5 min, retry |
| Network drop mid-batch | Transaction never commits, retry picks up from same block |
| SIGINT/SIGTERM received | Clean shutdown after current sleep/batch completes |

**Recovery:** Automatic. The daemon continuously retries on errors, querying Snowflake for the last exported block each attempt to auto-resume from the correct position.

---

## Open Items

1. **Snowflake DDL ownership**: Who creates tables? Assume pre-existing for now.
2. **CREATE STAGE privilege**: Array binding may auto-stream to temp stage. User needs `CREATE STAGE` on schema.
3. **Performance tuning**: 1,000 blocks is a starting point. May adjust based on observed Snowflake performance.

---

## Remaining Work

### Must Have
- [ ] Integration test with real Snowflake instance
- [ ] Validate array binding performance at scale (~100K rows per batch)

### Nice to Have
- [ ] Add unit tests with mocked DB for `pkg/snowflake`
- [ ] Add Prometheus metrics for monitoring
- [ ] Add exponential backoff (currently fixed 5-minute backoff)

