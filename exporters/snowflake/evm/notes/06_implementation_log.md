# Implementation Log: Snowflake EVM Transformer

**Date**: 2026-01-15
**Status**: 4/6 Tests Passing (Gunzilla)
**Last Updated**: 2026-01-15T02:05:00Z

---

## Current Test Status

| Test | Status | Diff Count |
|------|--------|------------|
| `TestTransformBlocks` | ✅ PASS | 0 |
| `TestTransformTransactions` | ❌ FAIL | 936 rows |
| `TestTransformReceipts` | ✅ PASS | 0 |
| `TestTransformLogs` | ✅ PASS | 0 |
| `TestTransformInternalTxs` | ❌ FAIL | 3337 rows |
| `TestTransformMessages` | ✅ PASS | 0 |

---

## Key Commands

```bash
# Run tests (Gunzilla)
cd /home/ubuntu/l1-data-tools/exporters/snowflake/evm
export INGESTION_URL=http://100.29.188.167/indexer/2M47TxWHGnhNtq6pM5zPXdATBtuqubxn5EPFgFmEawCQr9WFML/ws
go test -v -count=1 ./pkg/transform/...

# Export sparse golden blocks
cd /home/ubuntu/l1-data-tools
go run ./exporters/snowflake/evm/cmd/export_golden/...
```

---

## Research Findings from avalanche-data-producer

### Location: `~/avalanche-data-producer`

### TransactionCost Calculation
**File:** `entities/subnet/entities.go:91`
```go
TransactionCost: trx.Cost().String(),
```
Uses Go-Ethereum's native `trx.Cost()` which is `gas * gasPrice + value`. **We do the same.**

### Internal Transactions (FlatCall)
**File:** `utils/utilities.go:73-103`
```go
func (c *CallFrame) TransformCall(callIndex string) *FlatCall {
    call := FlatCall{
        Value:   "0",
        Gas:     "0",
        GasUsed: "0",
        // ...
    }
    if len(c.Value) > 0 {
        call.Value = (hexutil.MustDecodeBig(c.Value)).String()
    }
    if len(c.Gas) > 0 {
        call.Gas = (hexutil.MustDecodeBig(c.Gas)).String()
    }
    if len(c.GasUsed) > 0 {
        call.GasUsed = (hexutil.MustDecodeBig(c.GasUsed)).String()
    }
}
```

**KEY FINDING:** Producer does **NO intrinsic gas subtraction**! They just decode hex values directly to decimal.

---

## Fixes Applied

### 1. Removed Intrinsic Gas Subtraction (FIXED)
- **File:** `pkg/transform/internal_txs.go`
- **Change:** Removed lines 64-79 that subtracted intrinsic gas
- **Impact:** Reduced internal tx mismatches from 13182 → 3337

### 2. BlockGasCost returns empty for 0x0 (FIXED)
- **File:** `helpers.go`
- **Change:** `hexToInt64Str()` returns "" for 0x0 values

### 3. TransactionType keeps 0 value (FIXED)
- **File:** `transactions.go`  
- **Change:** Use `hexToInt64StrKeepZero()` for TransactionType

### 4. Output normalization removed (FIXED)
- **File:** `helpers.go`
- **Change:** `normalizeHexOutput()` returns input as-is

---

## Remaining Precision Issues

### TransactionCost (936 rows)
**Example:**
- Golden: `50060000745118064`
- Generated: `50060000745118060`
- Diff: 4 wei

**Analysis:** Both use `gas * gasPrice + value`. The 4 wei diff might be:
- Rounding in producer
- Different RPC data source
- Float precision somewhere

### Internal Txs Value (3337 rows)
**Example:**
- Golden: `1984339999999999900000`
- Generated: `1984340000000000000000`
- Diff: ~100000 wei (0.0001 ETH)

**Analysis:** Suspicious precision loss. Producer uses `hexutil.MustDecodeBig()`, we use `parseHexBigInt()` - should be equivalent.

---

## Chain Configuration

| Chain | Blockchain ID | File Prefix | Block Range | Status |
|-------|---------------|-------------|-------------|--------|
| C-Chain | `2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5` | `C_` | 75000000-75100000 | Not synced |
| Gunzilla | `2M47TxWHGnhNtq6pM5zPXdATBtuqubxn5EPFgFmEawCQr9WFML` | `GUNZILLA_` | 14000000-14100000 | ✅ Active |

---

## Files Modified This Session

### ingestion/evm/client/client.go
- Added `InfoResponse` struct and `Info()` method

### exporters/snowflake/evm/cmd/export_golden/main.go
- Chain-aware, sparse block sampling (every 100th)

### exporters/snowflake/evm/pkg/transform/transform_test.go
- Chain detection, zstd decompression, chain-specific golden files

### exporters/snowflake/evm/pkg/transform/helpers.go
- `hexToInt64Str()` returns "" for 0x0
- `hexToInt64StrKeepZero()` added
- `normalizeHexOutput()` returns input as-is

### exporters/snowflake/evm/pkg/transform/transactions.go
- Use `hexToInt64StrKeepZero()` for TransactionType

### exporters/snowflake/evm/pkg/transform/internal_txs.go
- **REMOVED** intrinsic gas subtraction (producer doesn't do this)

---

## Asset Files

**Location:** `notes/assets/`

| File | Size | Description |
|------|------|-------------|
| `GUNZILLA_BLOCKS.jsonl.zst` | 3.6MB | Input: 1001 sparse blocks |
| `GUNZILLA_*.csv.zst` | ~6MB total | Golden CSVs from Snowflake |
| `C_*.csv.zst` | ~15MB total | C-Chain golden CSVs (no JSONL input yet) |

---

## Next Steps

1. **Investigate precision issues** - May need to compare exact hex values in trace data vs what producer outputs
2. **Consider accepting small diffs** - The precision issues are tiny (4 wei, 0.0001 ETH) and may be acceptable
3. **Test with C-Chain** when indexer catches up to block 75M
4. **Add chain-specific handling** if C-Chain has different quirks than Gunzilla
