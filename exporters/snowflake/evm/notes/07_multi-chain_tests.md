# Multi-Chain Test Infrastructure

**Date**: 2026-01-15
**Status**: 4/6 Tests Passing

---

## Current Test Results (Gunzilla)

| Test | Status | Issue |
|------|--------|-------|
| `TestTransformBlocks` | ✅ PASS | - |
| `TestTransformTransactions` | ❌ FAIL | 936 rows with TransactionCost precision diff (~4 wei) |
| `TestTransformReceipts` | ✅ PASS | - |
| `TestTransformLogs` | ✅ PASS | - |
| `TestTransformInternalTxs` | ❌ FAIL | 13182 rows with Gas/Value calculation diffs |
| `TestTransformMessages` | ✅ PASS | - |

---

## Quick Start Commands

```bash
# Run tests (Gunzilla)
cd /home/ubuntu/l1-data-tools/exporters/snowflake/evm
export INGESTION_URL=http://100.29.188.167/indexer/2M47TxWHGnhNtq6pM5zPXdATBtuqubxn5EPFgFmEawCQr9WFML/ws
go test -v -count=1 ./pkg/transform/...

# Export golden blocks
cd /home/ubuntu/l1-data-tools
go run ./exporters/snowflake/evm/cmd/export_golden/...
```

---

## Compressed Asset Files

All golden files are zstd compressed (total ~10MB vs ~220MB uncompressed):

```
notes/assets/
├── GUNZILLA_BLOCKS.jsonl.zst       # 3.6MB - Input data (1001 sparse blocks)
├── GUNZILLA_*.csv.zst              # Golden CSV files from Snowflake
├── C_*.csv.zst                     # C-Chain golden CSVs (no JSONL input yet)
└── README.md                       # Snowflake query documentation
```

---

## Known Chain IDs

| Chain | Blockchain ID | Status |
|-------|---------------|--------|
| Gunzilla | `2M47TxWHGnhNtq6pM5zPXdATBtuqubxn5EPFgFmEawCQr9WFML` | ✅ Active |
| C-Chain | `2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5` | ⏳ Indexer not synced |

---

## Remaining Issues to Fix

1. **TransactionCost** - Precision difference of a few wei
2. **Internal Txs Gas/GasUsed** - Intrinsic gas subtraction logic differs from Snowflake's producer
3. **Internal Txs Value** - Similar precision issue

See `06_implementation_log.md` for full technical details.
