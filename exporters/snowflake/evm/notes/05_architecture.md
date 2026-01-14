> [!CAUTION]
> **NEVER CUT CORNERS.** If tests don't pass, if data is missing or incorrect, if something seems wrong or data appears inconsistent—**STOP and ask the user for input.** Do not proceed with assumptions, workarounds, or incomplete implementations. This is critical for data integrity.

# Architecture: Stateless EVM Transformer

**Date**: 2026-01-14  
**Status**: Approved

---

## Overview

A stateless Go package that transforms raw `NormalizedBlock` data into structured objects for 6 Snowflake tables. Tested using golden files (blocks 1-100).

---

## Directory Structure

```
exporters/snowflake/evm/
├── pkg/
│   └── transform/
│       ├── blocks.go              # BlockRow transformation
│       ├── transactions.go        # TransactionRow transformation
│       ├── receipts.go            # ReceiptRow transformation
│       ├── logs.go                # LogRow transformation
│       ├── internal_txs.go        # InternalTxRow (trace flattening)
│       ├── messages.go            # MessageRow transformation
│       ├── types.go               # Type definitions for all 6 row types
│       └── transform.go           # Main Transform([]NormalizedBlock) → ExportBatch
│
├── cmd/
│   └── export_golden/     # One-off: fetch blocks 1-100 → zst
│       └── main.go
│
├── internal/
│   └── csv/               # Test-only: structs → CSV strings
│       └── csv.go
│
└── notes/
    └── assets/
        ├── blocks_1_100.zst           # Raw NormalizedBlock data (compressed)
        ├── C_BLOCKS_1_100.csv
        ├── C_TRANSACTIONS_1_100.csv
        ├── C_RECEIPTS_1_100.csv
        ├── C_LOGS_1_100.csv
        ├── C_INTERNAL_TRANSACTIONS_1_100.csv
        └── C_MESSAGES_1_100.csv
```

---

## Components

### 1. Main Package (`exporters/snowflake/evm`)

**Responsibility**: Pure data transformation, no I/O.

- **Input**: `[]rpc.NormalizedBlock`
- **Output**: `ExportBatch` containing typed Go structs

```go
type ExportBatch struct {
    Blocks       []BlockRow
    Transactions []TransactionRow
    Receipts     []ReceiptRow
    Logs         []LogRow
    InternalTxs  []InternalTxRow
    Messages     []MessageRow
}

func Transform(blocks []rpc.NormalizedBlock) (*ExportBatch, error)
```

### 2. `cmd/export_golden`

**Responsibility**: One-time CLI to create golden test data.

1. Connect to `INGESTION_URL` using `ingestion/evm/client`
2. Fetch blocks 1-100
3. Write as zst-compressed JSONL to `notes/assets/blocks_1_100.zst`
4. Disconnect and exit

### 3. `internal/csv`

**Responsibility**: Test utility to serialize structs to CSV strings.

```go
func BlocksToCSV(rows []BlockRow) string
func TransactionsToCSV(rows []TransactionRow) string
// ... etc for all 6 types
```

**Why not parse CSVs?** Parsing golden CSVs could introduce bugs in the test harness itself. Instead, we generate CSV from our objects and compare byte-for-byte.

---

## Testing Strategy

```
blocks_1_100.zst
      │
      ▼ decompress + parse
[]NormalizedBlock
      │
      ▼ Transform()
ExportBatch (Go structs)
      │
      ▼ internal/csv
CSV strings (in memory)
      │
      ▼ compare
C_*_1_100.csv files
```

1. **Load**: Read `blocks_1_100.zst` → decompress → parse `[]NormalizedBlock`
2. **Transform**: Call `Transform()` to get structured objects
3. **Serialize**: Use `internal/csv` to generate CSV strings
4. **Compare**: Byte-for-byte match against golden CSV files

---

## 6 Table Transformations

| File | Input | Output |
|------|-------|--------|
| `blocks.go` | `Block` | `BlockRow` |
| `transactions.go` | `Block.Transactions[]` | `[]TransactionRow` |
| `receipts.go` | `Receipts[]` | `[]ReceiptRow` |
| `logs.go` | `Receipt.Logs[]` | `[]LogRow` |
| `internal_txs.go` | `Traces[].Result.Calls` (recursive) | `[]InternalTxRow` |
| `messages.go` | `Block.Transactions[]` + baseFee | `[]MessageRow` |

---

## Key Design Decisions

1. **Flat package**: No nested `pkg/transform/`. Simple ~8 file package at root level.
2. **Stateless**: No Snowflake connection, no local state. Pure transformation.
3. **Golden file testing**: Pre-fetch blocks once, reuse in tests.
4. **CSV generation, not parsing**: Avoids "testing the test" problem.
5. **internal/csv**: Test utility only, not exported.
