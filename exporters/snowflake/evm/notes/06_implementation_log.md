# Implementation Log: Snowflake EVM Transformer

**Date**: 2026-01-14  
**Status**: In Progress (Paused)  
**Last Updated**: 2026-01-14T02:53:39Z

---

## Summary

Implementing a stateless EVM data transformer that converts `NormalizedBlock` data into Snowflake-compatible CSV rows. The implementation uses golden file verification against existing Snowflake exports (blocks 1-100).

---

## Current Test Status

| Table | Status | Remaining Issues |
|-------|--------|------------------|
| `C_BLOCKS` | ✅ PASS | - |
| `C_TRANSACTIONS` | ⚠️ 80/81 | 1 tx has tiny precision diff in TransactionCost |
| `C_RECEIPTS` | ✅ PASS | - |
| `C_LOGS` | ✅ PASS | - |
| `C_INTERNAL_TRANSACTIONS` | ❌ FAIL | Multiple field differences (see below) |
| `C_MESSAGES` | ✅ PASS | - |

---

## Key Discoveries and Fixes Applied

### 1. BLOCKRECEIPTHASH vs BLOCKRECEIPTSROOT (✅ Fixed)

Golden data has unexpected mappings:

| Snowflake Column | Mapped From |
|------------------|-------------|
| `BLOCKRECEIPTHASH` | `ReceiptsRoot` |
| `BLOCKRECEIPTSROOT` | `StateRoot` |
| `BLOCKSTATEROOT` | `StateRoot` |

### 2. BLOCKEXTRADATA Encoding (✅ Fixed)

- Golden: Base64-encoded bytes
- Fix: Added `hexToBase64()` helper in `blocks.go`

### 3. Empty Address Handling (✅ Fixed)

Contract creation transactions have no `To` address:
- Golden: `0x`
- Fix: Added `normalizeAddress()` helper, applied in `transactions.go` and `internal_txs.go`

### 4. TransactionCost Formula (✅ Fixed)

- Discovered: `TransactionCost = gas × gasPrice + value` (not just gas × gasPrice)
- Fixed in `transactions.go`

### 5. MaxFeePerGas / MaxPriorityFeePerGas Defaults (✅ Fixed)

For pre-EIP-1559 transactions, these fields default to `GasPrice` in golden data.

---

## Remaining Issues to Fix

### A. TransactionCost Precision (1 tx mismatch)

Transaction `0x580bb509...`:
- Generated: `98990130000000000000`
- Golden: `98990129999999990000`
- Diff: 10000 wei (tiny precision issue, possibly big.Int calculation)

**Plan**: May need to investigate if this is acceptable rounding or if there's a calculation order issue.

### B. Internal Transactions (100 mismatches)

Debug output revealed multiple issues:

#### B1. Header Backticks
- Golden: `` `FROM` ``, `` `TO` ``
- Generated: `FROM`, `TO`
- **Plan**: Handle in test comparison normalization (can't use backticks in Go struct tags)

#### B2. VALUE Empty vs 0
- Golden: `'0'` for empty values
- Generated: `''` (empty string)
- **Plan**: Modify `hexToBigIntStr()` to return `"0"` for empty input instead of `""`

#### B3. OUTPUT Empty vs 0x
- Golden: `'0x'` for empty output
- Generated: `''` (empty string)
- **Plan**: Add normalization in `FlattenCallTrace` for empty Output

#### B4. TRACE_POSITION Calculation
- Golden has different position values than our calculation
- Example: We output `0`, golden has `1`
- **Plan**: Research how original producer calculates this (may need to grep `avalanche-data-producer`)

#### B5. GAS / GASUSED Differences
- Our values come directly from trace
- Golden has different values (possibly from receipt or different calculation)
- Example: We output `8000000`, golden has `7776512`
- **Plan**: Research original producer to understand source of these values

---

## Files Modified

### pkg/transform/

| File | Status |
|------|--------|
| `types.go` | ✅ Complete |
| `helpers.go` | ✅ Complete (normalizeAddress, hexToBase64 added) |
| `blocks.go` | ✅ Complete |
| `transactions.go` | ✅ Complete |
| `receipts.go` | ✅ Complete |
| `logs.go` | ✅ Complete |
| `internal_txs.go` | ⏳ Needs fixes for B2-B5 above |
| `messages.go` | ✅ Complete |
| `transform.go` | ✅ Complete |
| `transform_test.go` | ⏳ Needs header backtick normalization for internal_txs |

### internal/csv/

| File | Status |
|------|--------|
| `csv.go` | ✅ Complete |

---

## Debug Tools

- `cmd/debug_compare/main.go` - Field-by-field comparison between generated and golden CSVs (very useful!)

---

## Next Session Plan

1. **Fix internal_txs empty values**:
   - Update `hexToBigIntStr()` to return `"0"` not `""`
   - Normalize empty Output to `"0x"`

2. **Fix test comparison for headers**:
   - Strip backticks from golden CSV headers when comparing internal_txs

3. **Research internal_txs fields**:
   - Use `grep` on `avalanche-data-producer` to find how GAS, GASUSED, TRACE_POSITION are calculated
   - May need to look at `utilities.go` for `FlatCall` struct population

4. **Investigate transaction cost precision**:
   - Check if the 10000 wei difference is acceptable or needs fixing

5. **Run final verification**:
   - All tests should pass
   - Update task.md to mark completion

---

## References

- `notes/02_data_structures.md` - Snowflake schema definitions
- `notes/04_audit_findings.md` - Known issues in original producer
- `ingestion/evm/rpc/rpc/types.go` - Input data structures
- `notes/assets/*.csv` - Golden files for verification
- `~/avalanche-data-producer/` - Original producer for reference (if available)
