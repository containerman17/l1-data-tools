# Implementation Plan: Snowflake EVM Exporter

## Overview
One-shot CLI tool that exports EVM blocks from ingestion service to Snowflake in 1,000-block batches, auto-continuing until caught up. Designed for cron job orchestration.

---

## Pre-requisites

**MUST FIX FIRST:** Transform package has 2 failing tests (transactions, internal_txs - precision issues). These must pass before implementing the exporter to ensure data correctness.

---

## Behavior Flow

```
1. Check blocks table MAX → 2. Get ingestion latest → 3. Fetch 1K blocks → 4. Transform → 5. Write → 6. Repeat
```

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

### 6. Auto-Continue Loop

```go
for {
    // Refresh latest block from ingestion
    info, err := client.Info(ctx)
    if err != nil {
        return err
    }
    
    if fromBlock > info.LatestBlock {
        log.Println("Caught up to latest block", info.LatestBlock)
        return nil
    }
    
    // Fetch, transform, write 1,000 blocks...
    
    fromBlock = targetBlock + 1
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
| `SNOWFLAKE_USER` | Yes | Snowflake username |
| `SNOWFLAKE_PASSWORD` | Yes | Snowflake password |
| `SNOWFLAKE_DATABASE` | Yes | Target database |
| `SNOWFLAKE_SCHEMA` | Yes | Target schema |
| `SNOWFLAKE_WAREHOUSE` | Yes | Compute warehouse |
| `SNOWFLAKE_ROLE` | No | Role to use (uses default if not set) |
| `SNOWFLAKE_TABLE_PREFIX` | Yes | Table prefix (e.g., "cchain_", "dfk_") |
| `INGESTION_URL` | Yes | Address of ingestion service (e.g., "localhost:9090") |
| `BATCH_SIZE` | No | Blocks per transaction (default: 1000) |

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Successfully caught up to latest block |
| 1 | Snowflake connection/query error |
| 2 | Ingestion service error |
| 3 | Transformation error |

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

#### 2. Main Loop
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
        // 1. Get last exported block
        lastBlock, err := sfClient.GetLastBlock(ctx)
        if err != nil {
            return err
        }
        fromBlock := lastBlock + 1

        // 2. Get latest available
        info, err := ingClient.Info(ctx)
        if err != nil {
            return err
        }
        if fromBlock > int64(info.LatestBlock) {
            log.Printf("Caught up: exported through block %d", lastBlock)
            return nil
        }

        // 3. Calculate batch range
        targetBlock := min(fromBlock+int64(cfg.BatchSize)-1, int64(info.LatestBlock))
        log.Printf("Exporting blocks %d-%d", fromBlock, targetBlock)

        // 4. Stream blocks
        blocks, err := fetchBlocks(ctx, ingClient, uint64(fromBlock), uint64(targetBlock))
        if err != nil {
            return err
        }

        // 5. Transform
        batches, err := transformBlocks(blocks)
        if err != nil {
            return err
        }

        // 6. Write atomically
        if err := sfClient.WriteBatch(ctx, batches); err != nil {
            return err
        }

        log.Printf("Wrote %d blocks (%d txs, %d logs)", 
            len(batches),
            countTransactions(batches),
            countLogs(batches))
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

### Phase 1: Snowflake Client
- [ ] Implement `snowflake.Client` with connection handling
- [ ] Implement `GetLastBlock()` query
- [ ] Implement `WriteBatch()` with array binding
- [ ] Add unit tests with mocked DB

### Phase 2: Export Loop
- [ ] Implement block fetching with context cancellation
- [ ] Implement main export loop
- [ ] Add logging (block ranges, row counts, timing)

### Phase 3: CLI
- [ ] Load all config from environment variables
- [ ] Implement main.go with signal handling
- [ ] Integration test with real Snowflake (manual)

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
| Snowflake connection fails | Exit with error, cron retries |
| Ingestion service unavailable | Exit with error, cron retries |
| Transform error | Exit with error, log block number |
| Transaction commit fails | Rollback (automatic), exit with error |
| Duplicate PK on insert | Transaction fails, exit with error |
| Network drop mid-batch | Transaction never commits, safe to retry |

**Recovery:** Just run again. The tool queries Snowflake for the last exported block on startup, so it automatically resumes from the correct position.

---

## Open Items

1. **Snowflake DDL ownership**: Who creates tables? Assume pre-existing for now.
2. **CREATE STAGE privilege**: Array binding may auto-stream to temp stage. User needs `CREATE STAGE` on schema.
3. **Performance tuning**: 1,000 blocks is a starting point. May adjust based on observed Snowflake performance.
