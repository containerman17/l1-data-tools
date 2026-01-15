# Snowflake UTXO Export

**Date**: January 2025  
**Status**: Implementation

---

## Target Schema

```sql
create or replace TABLE AVALANCHE.PRIMARY.UTXOS (
    UTXO_ID VARCHAR(16777216),
    TRANSACTION_HASH VARCHAR(16777216),
    BLOCK_INDEX NUMBER(38,0),
    OUTPUT_INDEX NUMBER(38,0),
    TIMESTAMP TIMESTAMP_NTZ(9),
    TX_TYPE VARCHAR(16777216),
    OUTPUT_TYPE VARCHAR(16777216),
    ADDRESS VARCHAR(16777216),
    CONSUMING_TRANSACTION_HASH VARCHAR(16777216),
    ASSET_ID VARCHAR(16777216),
    LOCKTIME NUMBER(38,0),
    THRESHOLD NUMBER(38,0),
    PUBLIC_KEY VARCHAR(16777216),
    SIGNATURE VARCHAR(16777216),
    AMOUNT NUMBER(38,0),
    GROUP_ID NUMBER(38,0),
    PAYLOAD VARCHAR(16777216),
    FX_ID VARCHAR(16777216),
    CREATED_ON VARCHAR(16777216),
    CONSUMED_ON VARCHAR(16777216),
    PLATFORM_LOCKTIME TIMESTAMP_NTZ(9),
    STAKED BOOLEAN,
    STAKEABLE_LOCKTIME TIMESTAMP_NTZ(9),
    REWARD BOOLEAN,
    LAST_UPDATED TIMESTAMP_NTZ(9),
    LOAD_TIMESTAMP TIMESTAMP_NTZ(9),
    CONSUMING_TRANSACTION_TIMESTAMP TIMESTAMP_NTZ(9),
    STAKING_START_TIME TIMESTAMP_NTZ(9),
    STAKING_END_TIME TIMESTAMP_NTZ(9),
    NODE_ID VARCHAR(16777216),
    REWARD_ADDRESSES VARIANT,
    CONSUMING_BLOCK_HEIGHT NUMBER(38,0)
);
```

---

## Field Mapping

### ✅ Already Have (21 fields)

| Snowflake | Our Field | Location |
|-----------|-----------|----------|
| `UTXO_ID` | `UTXOId` | `store.go:43` |
| `TRANSACTION_HASH` | `TxHash` | `store.go:44` |
| `BLOCK_INDEX` | `BlockNumber` | `store.go:61` |
| `OUTPUT_INDEX` | `OutputIndex` | `store.go:45` |
| `TIMESTAMP` | `BlockTimestamp` | `store.go:62` |
| `OUTPUT_TYPE` | `UTXOType` | `store.go:56` |
| `CONSUMING_TRANSACTION_HASH` | `ConsumingTxHash` | `store.go:69` |
| `ASSET_ID` | `AssetID` | `store.go:49` |
| `LOCKTIME` | Parsed from output | `p_indexing.go:272` |
| `THRESHOLD` | `Threshold` | `store.go:53` |
| `AMOUNT` | `Amount` | `store.go:48` |
| `GROUP_ID` | `GroupID` | `store.go:82` |
| `PAYLOAD` | `Payload` | `store.go:81` |
| `CREATED_ON` | `CreatedOnChainID` | `store.go:65` |
| `CONSUMED_ON` | `ConsumedOnChainID` | `store.go:66` |
| `PLATFORM_LOCKTIME` | `PlatformLocktime` | `store.go:57` |
| `STAKED` | `Staked` | `store.go:58` |
| `STAKING_START_TIME` | `UTXOStartTimestamp` | `store.go:74` |
| `STAKING_END_TIME` | `UTXOEndTimestamp` | `store.go:75` |
| `CONSUMING_TRANSACTION_TIMESTAMP` | `ConsumingBlockTimestamp` | `store.go:71` |
| `CONSUMING_BLOCK_HEIGHT` | `ConsumingBlockNumber` | `store.go:70` |

### 🟢 Easy to Add (6 fields, ~5 mins each)

| Snowflake | Implementation |
|-----------|----------------|
| `TX_TYPE` | Add `TxType string` to `StoredUTXO`. Set in `ProcessPChainBatch` switch: `"AddValidator"`, `"ExportTx"`, etc. |
| `FX_ID` | Add `FxID string`. Set in `buildUTXO` switch: `"secp256k1fx"`, `"nftfx"`, `"propertyfx"`. |
| `REWARD` | Add `IsReward bool`. Set `true` in `processRewardTx()`. |
| `STAKEABLE_LOCKTIME` | Add `StakeableLocktime *uint64`. Extract from `stakeable.LockOut.Locktime`. |
| `LAST_UPDATED` | Add `LastUpdated int64`. Set `time.Now().Unix()` on every save. |
| `ADDRESS` | Already have `Addresses[]`. For export: use `Addresses[0]` or comma-join. |

### 🟡 Medium Complexity (4 fields, ~30-60 mins each)

| Snowflake | Implementation |
|-----------|----------------|
| `NODE_ID` | Available in `AddValidatorTx.NodeID`. Pass to `indexOutputs()` for stake outputs. |
| `REWARD_ADDRESSES` | Extract from `AddValidatorTx.RewardsOwner`. Store as `[]string`. |
| `PUBLIC_KEY` | Extend P-Chain credential extraction. Use ECDSA recovery from `ptx.Creds`. |
| `SIGNATURE` | Same as `PUBLIC_KEY`. Already done for C/X chains. |

### 🔵 Not Our Concern (1 field)

| Snowflake | Notes |
|-----------|-------|
| `LOAD_TIMESTAMP` | Set by Snowflake during ingestion. |

---

## Implementation Plan

### Step 1: Add Fields to StoredUTXO

```go
// store.go - add to StoredUTXO struct
TxType            string   `json:"txType,omitempty"`
FxID              string   `json:"fxId,omitempty"`
IsReward          bool     `json:"isReward,omitempty"`
StakeableLocktime *uint64  `json:"stakeableLocktime,omitempty"`
LastUpdated       int64    `json:"lastUpdated,omitempty"`
NodeID            string   `json:"nodeId,omitempty"`
RewardAddresses   []string `json:"rewardAddresses,omitempty"`
```

### Step 2: Update buildUTXO for FxID

```go
// p_indexing.go - in buildUTXO switch
case *secp256k1fx.TransferOutput:
    utxo.FxID = "secp256k1fx"
case *stakeable.LockOut:
    utxo.FxID = "secp256k1fx"
    utxo.StakeableLocktime = &o.Locktime
case *nftfx.TransferOutput, *nftfx.MintOutput:
    utxo.FxID = "nftfx"
case *propertyfx.OwnedOutput, *propertyfx.MintOutput:
    utxo.FxID = "propertyfx"
```

### Step 3: Update ProcessPChainBatch for TxType and Staking Data

```go
// p_indexing.go - in switch on unsigned
var txType string
var nodeID ids.NodeID
var rewardAddresses []string

case *ptxs.AddValidatorTx:
    txType = "AddValidator"
    nodeID = t.NodeID()
    rewardAddresses = extractAddresses(t.RewardsOwner)
    // ... existing code
case *ptxs.ExportTx:
    txType = "ExportTx"
    // ... existing code
// ... other cases
```

### Step 4: Update processRewardTx

```go
// p_indexing.go - in processRewardTx
utxo.IsReward = true
```

### Step 5: Add Export Command

Create `cmd/export/main.go` that:
1. Scans all UTXOs from Pebble
2. Writes CSV/JSON in Snowflake-compatible format
3. Handles field transformations (timestamp → TIMESTAMP_NTZ, etc.)

---

## Effort Estimate

| Task | Time |
|------|------|
| Add easy fields (6) | 30 mins |
| Add medium fields (4) | 2 hours |
| Export command | 1 hour |
| Testing | 30 mins |
| **Total** | **~4 hours** |

---

## ⚠️ IMPORTANT CONSTRAINTS

> [!CAUTION]
> **Do NOT change the HTTP API responses!**  
> Store new fields in the database, but do NOT expose them in HTTP API responses.
> The API must remain identical before and after these changes.
> New fields are internal (for Snowflake export only).

---

## Implementation Progress (Jan 9, 2025)

### ✅ Completed

1. **Added 7 new fields to `StoredUTXO`** (`store.go:87-96`):
   - `TxType` - Transaction type (AddValidatorTx, ExportTx, etc.)
   - `FxID` - FX that created output (secp256k1fx, nftfx, propertyfx)
   - `IsReward` - Boolean for reward UTXOs
   - `StakeableLocktime` - Locktime for StakeableLockOut
   - `NodeID` - Validator/delegator node ID
   - `RewardAddresses` - Staking reward destination addresses
   - `LastUpdated` - Unix timestamp of last update

2. **Updated P-Chain indexing** (`p_indexing.go`):
   - [x] Extract `txType` for all 15+ transaction types
   - [x] Extract `nodeID` and `rewardAddresses` for staking txs
   - [x] Set `FxID` in `buildUTXO` switch for each output type
   - [x] Set `StakeableLocktime` for `stakeable.LockOut`
   - [x] Upsert `txType` on all outputs
   - [x] Upsert `nodeID` and `rewardAddresses` on staking outputs
   - [x] Set `IsReward = true` and `TxType = "RewardValidatorTx"` in `processRewardTx()`

3. **Added upsert cases** (`store.go:277-295`):
   - Added switch cases for all 7 new Snowflake fields

4. **Code compiles successfully** - `go build ./...` passes

### ⏳ TODO

- [ ] Set `LastUpdated` on every save (low priority)
- [x] Create export command (`cmd/export/main.go`) ✅
- [ ] Test that HTTP API responses remain unchanged

### 🐛 Issues Encountered & Fixed

1. **RewardsOwner access**: Initially tried calling `t.RewardsOwner()` as a method, but it's actually a field (`fx.Owner` interface). Fixed by using `t.RewardsOwner` directly.

2. **extractOwnerAddresses type**: Changed parameter type from `verify.Verifiable` to `any` to handle `fx.Owner` interface.

3. **Missing brace**: Added upsert cases but forgot closing brace for `for` loop. Fixed.

---

## Export Command Usage

Created `cmd/export/main.go` for exporting UTXOs to Snowflake-compatible format.

### Build
```bash
go build ./cmd/export/...
```

### Usage
```bash
# Export P-Chain UTXOs to CSV
go run ./cmd/export/... -data data/1/utxos -output utxos_p_chain.csv -chain p

# Export to JSON
go run ./cmd/export/... -data data/1/utxos -output utxos_p_chain.json -chain p

# Export X-Chain with limit
go run ./cmd/export/... -data data/1/utxos -output utxos_x_chain.csv -chain x -limit 1000
```

### Options
| Flag | Default | Description |
|------|---------|-------------|
| `-data` | `data/1/utxos` | Path to UTXO Pebble database |
| `-output` | `utxos_export.csv` | Output file (`.csv` or `.json`) |
| `-chain` | `p` | Chain to export: `p`, `x`, or `c` |
| `-limit` | `0` | Max rows (0 = unlimited) |

### Output Format
Matches Snowflake schema exactly:
- All timestamps in ISO 8601 / RFC3339 format
- Booleans as `true`/`false`
- `REWARD_ADDRESSES` as JSON array
- First address used for `ADDRESS` column

---

## Reference CSV Analysis (Jan 9, 2025)

Analyzed `notes/assets/33_10_utxos_from_jan_8th_2026.csv` (mainnet data) to verify format.

### Key Findings

| Field | Reference CSV Value | My Implementation | Fix Needed |
|-------|---------------------|-------------------|------------|
| `FX_ID` | `spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ` | Same (using constants) | ✅ Fixed |
| `TIMESTAMP` | `2026-01-08 08:35:01.000` | `2026-01-08T08:35:01Z` | ⚠️ Use space, add ms |
| `OUTPUT_TYPE` | Empty for rewards | `"TRANSFER"` | ⚠️ May need to be empty |
| `PLATFORM_LOCKTIME` | `1970-01-01 00:00:00.000` for 0 | RFC3339 | ⚠️ Different format |
| `REWARD_ADDRESSES` | JSON with line breaks | JSON compact | ⚠️ Format may need fixing |
| `PUBLIC_KEY/SIGNATURE` | Base64 (for consumed) | ✅ Already implemented |
| `NODE_ID` | `NodeID-xxxx` | ✅ Already implemented |
| `TX_TYPE` | `RewardValidatorTx`, `AddPermissionlessDelegatorTx` | ✅ Already implemented |

### FX Type IDs (Avalanche)
```
secp256k1fx = spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ
nftfx       = qd2U4HDWUvMrVUeTcCHp6xH3Qpnn1XbU5MDdnBoiifFqvgXwT  
propertyfx  = 2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w
```

### Fixes Applied
- [x] **FX_ID**: Updated `buildUTXO()` to use actual Avalanche FX type ID constants instead of string names (`store.go:34-36`, `p_indexing.go`)
- [ ] Match timestamp format exactly (`YYYY-MM-DD HH:MM:SS.000`) - only affects export, not storage
 