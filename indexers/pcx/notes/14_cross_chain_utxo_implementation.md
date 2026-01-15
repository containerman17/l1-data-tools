# C-Chain Atomic Transaction Parsing & Cross-Chain UTXO Fix

## Problem Summary

Cross-chain UTXOs (exported from C-Chain, imported to P-Chain) had incorrect data:
- `blockNumber` and `blockTimestamp` showed P-Chain import block instead of C-Chain export block
- `utxoBytes` was missing entirely
- Consumption data was being lost when C-Chain indexed after P-Chain
- `platformLocktime` was incorrectly included as `null` for cross-chain UTXOs

## Root Causes & Fixes

### 1. Codec Struct Mismatch (atomic.go)

**Problem:** The `atomicTx` struct had an **unexported** embedded interface:
```go
type atomicTx struct {
    unsignedAtomicTx `serialize:"true"` // WRONG - unexported
}
```

Go's codec cannot marshal/unmarshal unexported fields. Coreth uses an **exported** embedded interface.

**Fix:** Changed to `UnsignedAtomicTx` (exported).

**Files:**
- `indexers/utxos/atomic.go` - Export interface name
- `indexers/utxos/indexing.go` - Update reference to `tx.UnsignedAtomicTx`

---

### 2. Overwrite vs Merge (indexing.go)

**Problem:** C-Chain ExportTx processing used `saveUTXO()` which **overwrites** the entire UTXO record.

**Scenario:**
1. P-Chain indexes first: ImportTx creates UTXO with consumption data
2. C-Chain indexes later: ExportTx calls `saveUTXO()`, replacing entire record
3. Result: Creation data present, consumption data **lost**

**Fix:** Changed C-Chain ExportTx to use `upsertUTXO()` which **merges** updates into existing record.

**Code location:** `indexers/utxos/indexing.go:processCChainAtomicTx()` - case `*unsignedExportTx`

---

### 3. Silent Error Swallowing

**Problem:** Multiple violations of "fail fast and loud" principle:

| Location | Issue | Impact |
|----------|-------|--------|
| `indexing.go:441` | `continue` on atomic tx parse error | Silently skips blocks with invalid atomic txs |
| `atomic.go:132` | `continue` on tx marshal error | Silently skips txs in batch |
| `runner/c_runner.go:180` | Silent fallback to old format | Hides decode errors |

**Fix:** All now return errors immediately:
- `return fmt.Errorf("parse atomic txs at C-Chain block %d: %w", ...)`
- `return nil, fmt.Errorf("marshal atomic tx %d for ID: %w", ...)`
- `return nil, fmt.Errorf("decode C-Chain block %d: %w (delete data/*/blocks/c and re-sync)", ...)`

---

### 4. platformLocktime Field Handling (api.go)

**Problem:** `toAPIResponse()` always included `platformLocktime` in the response map, even for cross-chain UTXOs where it should be omitted.

**Glacier's behavior:**
- P-Chain UTXOs: Include `platformLocktime: 0` (or actual value)
- Cross-chain UTXOs: Omit `platformLocktime` field entirely

**Fix:** Only add `platformLocktime` to result map if not nil:
```go
if stored.PlatformLocktime != nil {
    result["platformLocktime"] = *stored.PlatformLocktime
}
```

**Code location:** `indexers/utxos/api.go:toAPIResponse()`

---

### 5. P-Chain ImportTx Overwriting Creation Data (indexing.go)

**Problem:** P-Chain ImportTx was unconditionally setting creation fields (`blockNumber`, `blockTimestamp`, etc.) even when C-Chain had already indexed the UTXO.

**Fix:** Check if UTXO already has creation data before setting fallback values:
```go
existing := u.loadUTXO(utxoID)
hasCreationData := existing != nil && existing.BlockTimestamp > 0

if !hasCreationData {
    // Only set fallback creation data if source chain hasn't indexed yet
    updates["blockNumber"] = heightStr
    updates["blockTimestamp"] = consumingTimestamp
}
```

**Code location:** `indexers/utxos/indexing.go:processImportTx()`

---

## Files Changed

| File | Lines | Changes |
|------|-------|---------|
| `atomic.go` | 142 | Export `UnsignedAtomicTx`, fail on marshal errors |
| `indexing.go` | 551 | Upsert for C-Chain ExportTx, conditional fallback for ImportTx, fail on parse errors |
| `api.go` | 482 | Conditional `platformLocktime` in `toAPIResponse()` |
| `store.go` | 221 | Change `PlatformLocktime` to `*uint64`, remove custom `MarshalJSON` (redundant) |
| `runner/c_runner.go` | 229 | Remove silent decode fallback |
| `cmd/test/main.go` | 383 | Add C-Chain support, better sync logging |

## Test Results

✅ **All tests pass**

Cross-chain UTXO now has complete, correct data:
- `blockNumber: "48746327"` - C-Chain export block
- `blockTimestamp: 1765267096` - C-Chain timestamp
- `utxoBytes: 0x0000937de3...` - Generated from C-Chain ExportTx
- `consumingTxHash: 2sjdBah6...` - P-Chain ImportTx
- `consumingBlockNumber: "250286"` - P-Chain block
- `consumingBlockTimestamp: 1765267108` - P-Chain timestamp
- `createdOnChainId: yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp` - C-Chain Fuji
- `platformLocktime` - omitted (as it should be for cross-chain UTXOs)

---

## Notes for Future

### JSON Encoding Strategy

We tried using custom `MarshalJSON()` on `StoredUTXO` to omit nil fields, but it was **redundant** because:
1. Database storage uses standard JSON encoding
2. API responses use `toAPIResponse()` which builds a custom map
3. The map-building approach gives full control over which fields to include

**Lesson:** When building API responses, manually constructing the map is simpler than custom `MarshalJSON` for selective field inclusion.

### Pointer Fields for Optional Data

Using `*uint64`, `*string`, `*int64` for optional fields allows us to distinguish "not set" from "set to zero/empty". This is critical for:
- `platformLocktime` - nil for cross-chain UTXOs, &0 or &value for P-Chain UTXOs
- Consumption fields - nil when unspent, set when consumed
- Staking times - nil for non-staked UTXOs

### Upsert Pattern for Cross-Chain Data

The upsert design is essential for handling chain desync:
- Different chains process at different speeds
- UTXO creation (ExportTx) and consumption (ImportTx) happen on different chains
- Order independence: either chain can process first, upsert merges the data

**Critical:** Only upsert fields you have - don't overwrite with fallback values if real data might exist.

### Error Handling

Never silently continue on errors in the indexing path. Every error should:
1. Log the full context (block height, tx ID, etc.)
2. Return the error up to the caller
3. Cause the indexer to stop and alert

Data corruption from silent errors is worse than downtime.

### Test Coverage

Added `store_test.go` with tests for:
- Custom JSON marshaling behavior
- Nil pointer handling
- Optional field omission

These tests catch regressions in the API response format.
