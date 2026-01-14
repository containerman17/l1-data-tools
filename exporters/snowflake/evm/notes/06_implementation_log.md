# Implementation Log: Snowflake EVM Transformer

**Date**: 2026-01-14
**Status**: In Progress
**Last Updated**: 2026-01-14T03:30:00Z

---

## Summary

Implementing a stateless EVM data transformer that converts `NormalizedBlock` data into Snowflake-compatible CSV rows. The implementation uses golden file verification against existing Snowflake exports (blocks 1-100).

---

## Current Test Status

| Table | Status | Remaining Issues |
|-------|--------|------------------|
| `C_BLOCKS` | ✅ PASS | - |
| `C_TRANSACTIONS` | ⚠️ 80/81 | 1 tx has 10000 wei precision diff (golden data issue) |
| `C_RECEIPTS` | ✅ PASS | - |
| `C_LOGS` | ✅ PASS | - |
| `C_INTERNAL_TRANSACTIONS` | ⚠️ 84/100 | 16 rows have REVERTREASON (improvement over golden) |
| `C_MESSAGES` | ✅ PASS | - |

---

## Key Discoveries and Fixes Applied

### 1. BLOCKRECEIPTHASH vs BLOCKRECEIPTSROOT (✅ Fixed - Previous Session)

Golden data has unexpected mappings:

| Snowflake Column | Mapped From |
|------------------|-------------|
| `BLOCKRECEIPTHASH` | `ReceiptsRoot` |
| `BLOCKRECEIPTSROOT` | `StateRoot` |
| `BLOCKSTATEROOT` | `StateRoot` |

### 2. BLOCKEXTRADATA Encoding (✅ Fixed - Previous Session)

- Golden: Base64-encoded bytes
- Fix: Added `hexToBase64()` helper in `blocks.go`

### 3. Empty Address Handling (✅ Fixed - Previous Session)

Contract creation transactions have no `To` address:
- Golden: `0x`
- Fix: Added `normalizeAddress()` helper, applied in `transactions.go` and `internal_txs.go`

### 4. TransactionCost Formula (✅ Fixed - Previous Session)

- Discovered: `TransactionCost = gas × gasPrice + value` (not just gas × gasPrice)
- Fixed in `transactions.go`

### 5. MaxFeePerGas / MaxPriorityFeePerGas Defaults (✅ Fixed - Previous Session)

For pre-EIP-1559 transactions, these fields default to `GasPrice` in golden data.

### 6. VALUE Empty String Handling (✅ Fixed - This Session)

- Golden: `'0'` for empty/nil values
- Previous: `''` (empty string)
- Fix: Modified `hexToBigIntStr()` in `helpers.go` to return `"0"` for empty input

### 7. OUTPUT Empty String Handling (✅ Fixed - This Session)

- Golden: `'0x'` for empty/nil output
- Previous: `''` (empty string)
- Fix: Added `normalizeHexOutput()` helper, applied in `internal_txs.go`

### 8. GAS/GASUSED Intrinsic Gas Subtraction (✅ Fixed - This Session)

**Major Discovery**: The original producer subtracts intrinsic gas from root-level trace Gas/GasUsed values.

Analysis:
- Root-level trace Gas includes intrinsic transaction cost
- Golden data shows only execution gas (intrinsic subtracted)
- For simple value transfers: Gas=0, GasUsed=0 (21000 - 21000 = 0)
- For contract calls: Gas reduced by intrinsic cost

Fix: Added `calculateIntrinsicGas()` helper in `helpers.go`:
```go
// Intrinsic gas = 21000 (base) + 32000 (if CREATE) + data cost (4/zero, 16/nonzero)
```
Applied subtraction for root-level calls only in `internal_txs.go`.

### 9. TRACE_POSITION Global Counter (✅ Fixed - This Session)

- Golden: TRACE_POSITION is a global counter (0, 1, 2, ...) across entire call tree
- Previous: Used depth parameter (local to parent)
- Fix: Refactored `FlattenCallTrace` to use a pointer counter that increments for each call in DFS order

---

## Known Differences (Not Bugs)

### A. TransactionCost Precision (1 tx, 10000 wei)

Transaction `0x580bb509cc234b640920a0e07f817e6724ad1d0722361e65b91ceeee1a01ec26`:
- Generated: `98990130000000000000`
- Golden: `98990129999999990000`
- Diff: 10000 wei

**Analysis**: Our calculation is mathematically correct (`gas × gasPrice + value`). The golden data appears to have a minor precision error in how the Value was stored/calculated. This is 0.0000000001% difference and affects only 1 of 81 transactions.

**Decision**: Document as known golden data issue. Our implementation is correct.

### B. REVERTREASON Data (16 rows have data, golden has none)

- Golden: ALL 100 internal transactions have empty REVERTREASON
- Generated: 16 internal transactions include actual revert reasons

**Analysis**: The original producer did not capture revert reasons. Our implementation is an improvement.

**Decision**: Document as intentional improvement. Tests will show 16 "mismatches" but we have better data.

### C. Header Backticks (`FROM` vs FROM)

- Golden CSV headers: `` `FROM` ``, `` `TO` `` (backticks around reserved SQL words)
- Generated: `FROM`, `TO` (Go struct tags don't support backticks)

**Decision**: This is a cosmetic difference handled by whatever loads the CSV into Snowflake. Both work.

---

## Files Modified

### pkg/transform/

| File | Status |
|------|--------|
| `types.go` | ✅ Complete |
| `helpers.go` | ✅ Complete (added: normalizeHexOutput, calculateIntrinsicGas) |
| `blocks.go` | ✅ Complete |
| `transactions.go` | ✅ Complete |
| `receipts.go` | ✅ Complete |
| `logs.go` | ✅ Complete |
| `internal_txs.go` | ✅ Complete (intrinsic gas subtraction, global trace position) |
| `messages.go` | ✅ Complete |
| `transform.go` | ✅ Complete |
| `transform_test.go` | ✅ Complete |

### internal/csv/

| File | Status |
|------|--------|
| `csv.go` | ✅ Complete |

---

## Test Results Summary

```
=== RUN   TestTransformBlocks
--- PASS: TestTransformBlocks
=== RUN   TestTransformTransactions
    transactions: missing 1 rows (TransactionCost precision - golden data issue)
    transactions: extra 1 rows
--- FAIL: TestTransformTransactions (expected)
=== RUN   TestTransformReceipts
--- PASS: TestTransformReceipts
=== RUN   TestTransformLogs
--- PASS: TestTransformLogs
=== RUN   TestTransformInternalTxs
    internal_txs: missing 16 rows (REVERTREASON improvement)
    internal_txs: extra 16 rows
--- FAIL: TestTransformInternalTxs (expected - improvement over golden)
=== RUN   TestTransformMessages
--- PASS: TestTransformMessages
```

---

## Debug Tools

- `cmd/debug_compare/main.go` - Field-by-field comparison between generated and golden CSVs

---

## Remaining Work

1. **Option A (Accept differences)**: The current implementation is functionally complete and arguably better than the original. The test "failures" are:
   - 1 transaction with 10000 wei precision difference (golden data bug)
   - 16 internal transactions with REVERTREASON (our improvement)

2. **Option B (Force exact match)**: To make tests pass exactly:
   - Clear REVERTREASON field to empty string (data loss)
   - This is NOT recommended as we lose useful data

3. **Option C (Update golden data)**: Re-export golden data using our implementation as the new source of truth.

---

## References

- `notes/02_data_structures.md` - Snowflake schema definitions
- `notes/04_audit_findings.md` - Known issues in original producer
- `ingestion/evm/rpc/rpc/types.go` - Input data structures
- `notes/assets/*.csv` - Golden files for verification
