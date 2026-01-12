# Proposed Design: Snowflake EVM Exporter

## Executive Summary

A CLI tool that subscribes to the `evm-ingestion` WebSocket service, transforms `NormalizedBlock` data into Snowflake-compatible format, and uploads to 6 target tables atomically using Snowflake transactions.

---

## Architecture Overview

```
┌─────────────────────┐     ┌──────────────────────┐     ┌─────────────────┐
│   EVM Ingestion     │     │  Snowflake Exporter  │     │    Snowflake    │
│     Service         │────▶│       (this tool)    │────▶│    Database     │
│  (WebSocket API)    │     │                      │     │                 │
└─────────────────────┘     └──────────────────────┘     └─────────────────┘
        │                            │                           │
   NormalizedBlock              Transform &               6 Tables:
   (Block, Receipts,            Batch Upload              - blocks
    Traces)                                               - transactions
                                                          - receipts
                                                          - logs
                                                          - internal_txs
                                                          - messages
```

---

## Data Flow

### Input: NormalizedBlock (from evm-ingestion)

```go
type NormalizedBlock struct {
    Block    Block                 // Block header + transactions
    Traces   []TraceResultOptional // Internal transactions (call traces)
    Receipts []Receipt             // Transaction receipts with logs
}
```

### Output: 6 Snowflake Tables

| Source Data | Target Table | Key Fields |
|-------------|--------------|------------|
| `Block` | `blocks` | block_number, hash, timestamp, miner, gas_used, etc. |
| `Block.Transactions[]` | `transactions` | tx_hash, block_number, from, to, value, gas, input, etc. |
| `Receipts[]` | `receipts` | tx_hash, gas_used, cumulative_gas, status, contract_address |
| `Receipt.Logs[]` | `logs` | log_index, tx_hash, block_number, address, topics, data |
| `Traces[].Result.Calls` (flattened) | `internal_txs` | tx_hash, block_number, from, to, value, call_type, depth |
| `core.TransactionToMessage()` | `messages` | tx_hash, msg_from, msg_to, msg_gas_price (effective) |

---

## Component Design

### 1. Progress Tracker (Snowflake Query)

```go
type ProgressTracker interface {
    GetLastProcessedBlock(ctx context.Context) (uint64, error)
}
```

**Implementation:**
```sql
SELECT MAX(block_number) FROM blocks WHERE chain_id = ?
```

No local SQLite needed - Snowflake is the single source of truth.

### 2. EVM Ingestion Client

Reuse existing `evm-ingestion-client`:

```go
client := client.NewClient("ingestion-server:9090")
err := client.Stream(ctx, fromBlock, func(blocks []client.Block) error {
    // Process batch
    return nil
})
```

**Key Capability:** The ingestion service already supports `?from=N` parameter to start from any block.

### 3. Data Transformer

```go
type Transformer interface {
    Transform(block *rpc.NormalizedBlock) (*ExportBatch, error)
}

type ExportBatch struct {
    Block        BlockRow
    Transactions []TransactionRow
    Receipts     []ReceiptRow
    Logs         []LogRow
    InternalTxs  []InternalTxRow
    Messages     []MessageRow     // 1:1 with transactions
}
```

**Trace Flattening:** Recursive `CallTrace.Calls` must be flattened with depth tracking.

### 4. Snowflake Writer

```go
type SnowflakeWriter interface {
    WriteBatch(ctx context.Context, batches []*ExportBatch) error
}
```

**Atomic Write Strategy:**
```sql
BEGIN TRANSACTION;

INSERT INTO blocks VALUES (...);
INSERT INTO transactions VALUES (...), (...), ...;
INSERT INTO receipts VALUES (...), (...), ...;
INSERT INTO logs VALUES (...), (...), ...;
INSERT INTO internal_txs VALUES (...), (...), ...;
INSERT INTO messages VALUES (...), (...), ...;

COMMIT;
```

All 6 inserts succeed together or all rollback.

---

## CLI Interface

```bash
# Basic usage - catch up from last processed block to latest
snowflake-evm-exporter --chain-id 43114 --ingestion-url ws://localhost:9090

# Export specific range (for testing/validation)
snowflake-evm-exporter --chain-id 43114 --from-block 1000 --to-block 1302

# Dry run - output to CSV instead of Snowflake
snowflake-evm-exporter --chain-id 43114 --from-block 1000 --to-block 1100 --output csv

# Compare mode - export and compare against production
snowflake-evm-exporter --chain-id 43114 --from-block 1000 --to-block 1100 --compare
```

### Configuration (Environment Variables)

| Variable | Description |
|----------|-------------|
| `SNOWFLAKE_ACCOUNT` | Snowflake account identifier |
| `SNOWFLAKE_USER` | Snowflake username |
| `SNOWFLAKE_PASSWORD` | Snowflake password |
| `SNOWFLAKE_DATABASE` | Target database |
| `SNOWFLAKE_SCHEMA` | Target schema |
| `SNOWFLAKE_WAREHOUSE` | Compute warehouse |
| `INGESTION_URL` | EVM ingestion WebSocket URL |
| `CHAIN_ID` | Avalanche chain ID (32-byte) |

---

## Batching Strategy

### Problem
Inserting one block at a time is inefficient for large catch-up operations.

### Solution
Buffer blocks and write in batches:

```go
const (
    BatchSize     = 100   // blocks per transaction
    MaxBatchBytes = 10MB  // or size limit
)
```

**Trade-off:** Larger batches = fewer transactions = faster, but larger rollback scope on failure.

### Recommended Approach
- **Historical sync:** Batch 100 blocks per Snowflake transaction
- **Real-time:** Single block per transaction (for latency)

---

## Validation Mode

### For Development/Testing

1. **Export to CSV:**
   ```bash
   snowflake-evm-exporter --from-block 1000 --to-block 1100 --output csv --output-dir ./export/
   ```
   Produces: `blocks.csv`, `transactions.csv`, `receipts.csv`, `logs.csv`, `internal_txs.csv`, `messages.csv`

2. **Compare with Production:**
   ```bash
   snowflake-evm-exporter --from-block 1000 --to-block 1100 --compare
   ```
   - Queries production Snowflake for same range
   - Generates diff report
   - Returns exit code 0 if identical, 1 if differences found

### Comparison Fields
- Row counts per table
- Checksum of sorted row hashes
- Sample row-by-row comparison on mismatch

---

## Messages Table

### Purpose
The `messages` table contains the EVM `core.Message` representation of each transaction. This is generated by `core.TransactionToMessage()` and provides:

- **Effective gas price**: For EIP-1559 transactions, this is the actual price paid (after base fee calculation), not the max fee specified
- **Sender address**: Recovered from transaction signature
- **Recipient address**: Same as transaction `to` field

### Key Difference from Transactions
| Field | transactions | messages |
|-------|-------------|----------|
| Gas Price | `transactionGasPrice` (raw, what tx specified) | `messageGasPrice` (effective, what was actually paid) |

For pre-EIP-1559 transactions, these values are identical. For EIP-1559, messages has the computed effective price.

---

## Error Handling

### Retry Strategy
- **Transient failures (network, timeout):** Exponential backoff, retry batch
- **Data errors (schema mismatch):** Fail fast, log block number, require manual intervention
- **Snowflake transaction failure:** Rollback, log batch range, retry from batch start

### Idempotency
- Use `INSERT ... IF NOT EXISTS` or check for existing blocks before insert
- On retry, skip already-processed blocks
- Alternative: `MERGE` statements for upsert semantics

---

## Performance Considerations

### Expected Throughput
- EVM ingestion provides ~100-500 blocks/second during historical sync
- Snowflake batch insert: ~1000+ rows/second per table
- Bottleneck likely: transformation (trace flattening) or Snowflake insert latency

### Optimizations
1. **Parallel transformation:** Process multiple blocks concurrently
2. **Bulk inserts:** Use Snowflake `PUT` + `COPY INTO` for large historical loads
3. **Connection pooling:** Reuse Snowflake connections

---

## Implementation Phases

### Phase 1: Core Pipeline
- [ ] Snowflake connection and progress tracking
- [ ] EVM ingestion client integration
- [ ] Basic transformer (blocks, transactions, logs)
- [ ] Atomic batch writer
- [ ] CLI with `--from-block` and `--to-block`

### Phase 2: Internal Transactions & Messages
- [ ] Trace flattening for internal transactions
- [ ] Messages table (trivial - 1:1 with transactions)
- [ ] Full 6-table atomic writes

### Phase 3: Validation
- [ ] CSV export mode
- [ ] Production comparison mode
- [ ] Automated diff reporting

### Phase 4: Production Readiness
- [ ] Error handling and retry logic
- [ ] Metrics and logging
- [ ] Cron job configuration
- [ ] Documentation

---

## Open Items for Discussion

1. **Exact Snowflake schema** - Need DDL for all 5 tables
2. **ICM contract addresses** - Which contracts to monitor for each supported chain?
3. **Batch size tuning** - What's optimal for Snowflake insert performance?
4. **Historical backfill strategy** - Full resync or assume production data is correct?
5. **Chain configuration** - How to specify which chains to export and their ingestion endpoints?
