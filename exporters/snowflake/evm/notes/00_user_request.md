# User Requirements: Snowflake EVM Exporter

## Overview

CLI tool that downloads EVM chain data from the `ingestion/evm/rpc` service and exports it to Snowflake. Designed to run once a day (or more frequently) as a catch-up process.

---

## Data Source

- Consumes data from `ingestion/evm/rpc` via WebSocket subscription
- EVM ingestion supports fetching data **up to a specific block number**
- Data format: `NormalizedBlock` containing:
  - **Block** data
  - **Transactions** (embedded in block)
  - **Receipts** (with logs)
  - **Traces** (internal transactions via `debug_traceBlockByNumber`)

---

## Target Tables in Snowflake (6 tables)

1. **Blocks**
2. **Transactions**
3. **Receipts** (gas used, status, contract creation info)
4. **Logs** (events emitted during transaction execution)
5. **Internal Transactions** (from traces/call traces)
6. **Messages** (EVM `core.Message` representation - effective gas price, 1:1 with transactions)

---

## Key Design Decision: No Local State Required

Instead of maintaining local SQLite state to track progress:
- Query Snowflake directly to find the **last processed block number**
- Use any table (e.g., Blocks) to get `MAX(block_number)`
- Start ingestion from `block_number + 1`

**Rationale:** Simpler architecture, single source of truth, eliminates local state synchronization issues.

---

## Consistency Requirement

**All 6 tables must stop at exactly the same block number.**

### Research Findings on Snowflake Atomic Writes:

Snowflake supports ACID transactions with the following options:

1. **Explicit Transactions**: `BEGIN TRANSACTION` ... `COMMIT` - all statements succeed or all rollback
2. **INSERT ALL Statement**: Insert into multiple tables atomically in a single SQL statement
3. **READ COMMITTED Isolation**: Data read during a transaction is committed at read time

**Conclusion:** ✅ Atomic writes across 5 tables are achievable using explicit transactions.

### Consistency Check Strategy:
- Query any single table (e.g., Blocks) for `MAX(block_number)`
- All tables are guaranteed to have data up to the same block if writes are transactional

---

## Processing Logic

### Block Range Strategy
- **Start Block**: Query Snowflake for last processed block → start from `block_number + 1`
- **End Block**: Latest available block from EVM ingestion (or specified manually)
- **No time-based granularity needed**: Process block-to-block, not by time periods

### Export Function Signature (conceptual)
```
ExportRange(fromBlock, toBlock) → exports data for blocks [fromBlock, toBlock] inclusive
```

Example: `ExportRange(1000, 1302)` → exports all data for blocks 1000 through 1302

### Upload Frequency
- Daily cron job is the primary use case
- Could run more frequently (e.g., every minute) since granularity doesn't matter
- Each run processes from last completed block to current available block

---

## Validation Strategy (Development Phase)

### Goal
Verify that data exported by this tool **exactly matches** production database data.

### Approach
1. **Sample random block ranges** from production Snowflake database
2. **Export same ranges** using this tool (to CSV or directly to a staging area)
3. **Perform 1:1 comparison** to verify exact match
4. Repeat for various chains and block ranges

### Querying Canonical Data
- Query production Snowflake with block range: `WHERE block_number >= X AND block_number <= Y`
- Compare against tool output for same range

---

## Production Migration Path

### Goal
Replace the existing production code that incrementally populates Snowflake.

### Migration Steps
1. Validate tool output matches production data (as above)
2. Run tool in parallel with production code
3. Verify ongoing consistency
4. Stop production code
5. Switch to this tool as the sole data source

### Post-Migration
This tool becomes the primary mechanism for writing EVM chain data to Snowflake.

---

## Open Questions

1. ~~Does Snowflake support atomic writes across multiple tables?~~ **Yes - via explicit transactions or INSERT ALL**
2. What is the exact Snowflake schema for each of the 5 tables?
3. Which chains need to be supported initially?
4. Are there any rate limits or batch size considerations for Snowflake inserts?
5. How to handle ICM message extraction (specific parsing logic needed)?
6. Error handling and retry strategy for partial failures?
