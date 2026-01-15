# P-Chain UTXO Indexer - Corners Cut & Caveats

Status: All 8 P-Chain tests passing, but with some shortcuts.

---

## 1. `rewardType` - NOT a problem

**Claim**: We don't track `rewardType` (VALIDATOR, DELEGATOR, VALIDATOR_FEE).

**Reality**: Tests pass because **Glacier's `/utxos` endpoint also doesn't return `rewardType`**.

Looking at `glacier-api/apps/glacier-api/src/models/primary-network/dto/p-chain/mappers.ts`:
- `mapUtxos()` (lines 281-334) - used by UTXO listing - does NOT include rewardType
- `mapUtxosFromTransaction()` (lines 200-279) - used by transaction details - DOES include rewardType

**Verdict**: ✅ We match Glacier. Not a bug.

---

## 2. `stakeableLocktime` - MISSING FIELD (TWO BUGS)

**What is stakeableLocktime?**

P-Chain UTXOs can be wrapped in `stakeable.LockOut`:
- **Outer** `stakeable.LockOut.Locktime` = `stakeableLocktime` (can stake but NOT transfer until this time)
- **Inner** `secp256k1fx.TransferOutput.Locktime` = `platformLocktime` (basic time-lock)

Example from Glacier fixture (mainnet):
```json
{
  "utxoId": "EidRTNhbZ1EoYtoK4pncRA87GWZXcoR1f71hmvAJariFCs29h",
  "utxoType": "STAKE",
  "stakeableLocktime": 1,
  "platformLocktime": 0,
  "staked": false
}
```

**Bug 1 - Wrong utxoType value**: 
- Glacier returns `"STAKE"` when `stakeableLocktime > 0`
- We return `"STAKEABLE_LOCK"` (from struct type name)

**Bug 2 - Wrong field mapping**:
```go
case *stakeable.LockOut:
    utxo.PlatformLocktime = &o.Locktime  // WRONG: o.Locktime is stakeableLocktime!
    utxo.UTXOType = "STAKEABLE_LOCK"     // WRONG: should be "STAKE"
    if inner, ok := o.TransferableOut.(*secp256k1fx.TransferOutput); ok {
        // We never read inner.Locktime (the actual platformLocktime)
    }
```

**Why tests pass**: Test addresses don't have `stakeable.LockOut` UTXOs. These are rare - mostly from early P-Chain genesis/airdrop with vesting schedules.

**Fix needed**:
1. Add `StakeableLocktime *uint64` to `StoredUTXO`
2. Change `utxoType` from `"STAKEABLE_LOCK"` to `"STAKE"` 
3. For `stakeable.LockOut`: 
   - Store `o.Locktime` as `StakeableLocktime`
   - Store `inner.Locktime` as `PlatformLocktime`

**Verdict**: ⚠️ Latent bug. Will fail when test addresses have stakeable-locked UTXOs.

---

## 3. Cross-Chain Address Recovery - ~~HEURISTIC~~ FIXED

**Original Problem**: We guessed addresses for imported UTXOs by looking at ImportTx outputs.
This created stale address index entries if the guess was wrong.

**Fix**: Removed the guessing. ImportTx now only records consumption data.
The source chain indexer (C-Chain/X-Chain) provides correct addresses when it processes the ExportTx.

**Trade-off**: Cross-chain UTXOs are "invisible" until source chain is indexed.
This is better than showing them to the wrong address.

**Verdict**: ✅ Fixed

---

## 4. Asset Metadata - HARDCODED TO AVAX

**Code** (`api.go` lines 413-432):
```go
func (u *UTXOs) assetName(assetID string) string {
    if assetID == getAvaxAssetID(u.networkID) {
        return "Avalanche"
    }
    return "Unknown"
}
```

**Problem**: Non-AVAX assets show as "Unknown" / "???" / denomination 0.

**Why tests pass**: Test addresses only hold AVAX.

**Verdict**: ⚠️ Will break for addresses holding non-AVAX assets.

---

## 5. Performance - O(N) Memory for UTXO Queries

**Code** (`api.go` lines 109-177): `getUTXOsForAddresses` loads ALL UTXOs into memory, then sorts, then paginates.

**Problem**: For addresses with 100k+ UTXOs (some mainnet validators), this will OOM or be extremely slow.

**Why tests pass**: Test addresses have few UTXOs.

**Verdict**: ⚠️ Not production-ready for high-volume addresses.

---

## 6. Transaction Type Coverage - MANUAL SWITCH

**Code** (`indexing.go` lines 50-111): Manual `switch` on transaction types.

**Problem**: If Avalanche adds new P-Chain tx types, we silently skip them (no input consumption indexed).

**Why tests pass**: We added all current types after discovering `IncreaseL1ValidatorBalanceTx` was missing.

**Verdict**: ⚠️ Requires code update for any new tx types.

---

## 7. utxoBytes Checksum - CORRECT

**Code** (`api.go` lines 474-494): SHA256 last 4 bytes.

**Reality**: Tests pass and checksums match Glacier.

**Verdict**: ✅ Correct implementation.

---

## Summary

| Issue | Tests Pass Because | Risk Level |
|-------|-------------------|------------|
| rewardType | Glacier also doesn't return it in /utxos | ✅ None |
| stakeableLocktime | Test addrs have no stakeable-locked UTXOs | ⚠️ Medium |
| utxoType "STAKE" | Test addrs have no stakeable-locked UTXOs | ⚠️ Medium |
| Cross-chain addresses | Fixed - no more guessing | ✅ Fixed |
| Asset metadata | Test addrs only have AVAX | ⚠️ Low |
| O(N) memory | Test addrs have few UTXOs | ⚠️ Medium (mainnet) |
| Tx type coverage | We added all current types | ⚠️ Low (future) |
| utxoBytes checksum | Correct implementation | ✅ None |

---

## How to Find Stakeable-Locked UTXOs

These are rare. Mostly from early P-Chain genesis/airdrop with vesting schedules.
Example address on mainnet that had one (now consumed):
- `avax1ufu4ag3w9rwjvlrdxmav2rys6mx2zkttex823v` - see txHash `2gP3TbgAVFK7UT1rEUh3YVG8JfNkVN8cPRJoKRzHf6s8MU9tmd`

