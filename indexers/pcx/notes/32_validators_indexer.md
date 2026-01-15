# Validators Indexer Implementation

## What
Implement `GET /v1/networks/{network}/validators` and `GET /v1/networks/{network}/validators/{nodeId}` endpoints. This indexer tracks validator registration transactions on the P-Chain to build a registry of validators with their staking details.

## Why
Validators are the backbone of the Avalanche network. This indexer allows users to list all validators and get details about specific validators (by nodeId), matching the functionality provided by Glacier API.

---

## Current State (2024-12-21)

### Files Created
```
indexers/validators/
├── validators.go      # Main struct, Init, Close, GetPChainWatermark
├── store.go           # ValidatorRecord, DelegatorRecord, Pebble storage ops
├── p_indexing.go      # ProcessPChainBatch - handles all validator txs
├── api.go             # HTTP handlers for list/get validators
└── selftest.go        # Test cases (currently LocalOnly due to ordering mismatch)
```

### What Works
- Basic indexing of all validator transaction types
- Storage in Pebble with indexes by nodeId and subnetId
- API endpoints return data
- Tests pass with `LocalOnly: true` (no Glacier comparison)

### What Doesn't Work Yet
- **Ordering mismatch**: We sort by startTimestamp, Glacier sorts by blockIndex
- **Missing live fields**: potentialRewards, delegatorCount, stakePercentage, etc.
- **Tests skip Glacier comparison** due to above issues

---

## Open Questions / Decisions Needed

### 1. Fields We Will SKIP (require external monitoring)
These fields require infrastructure we don't have:
- `uptimePerformance` - requires node uptime monitoring
- `validatorHealth` - requires node health checks
- `avalancheGoVersion` - requires node version polling
- `geolocation` - requires IP geolocation lookup

**Decision**: Return `null` or omit these fields entirely.

### 2. startTimestamp Discrepancy
Observed 30-second difference between our indexed startTimestamp and Glacier's.
- Our value: `1766148607`
- Glacier value: `1766148637`

**TODO**: Investigate if this is a parsing issue or data source difference.

---

## Progress Checklist
- [x] Research Glacier API response schema via curl
- [x] Create indexer structure (`indexers/validators/`)
- [x] Implement storage layer with Pebble
- [x] Implement P-chain indexing for validator transactions
- [x] Implement list validators API endpoint
- [x] Implement get single validator API endpoint
- [x] Implement selftest.go with test cases
- [x] Register indexer in cmd/server and cmd/test
- [x] Initial test run - basic structure works
- [ ] **Fix ordering to match Glacier (blockIndex DESC, then txHash)**
- [ ] **Add RPC client to validators indexer**
- [ ] **Implement potentialRewards via GetCurrentValidators RPC**
- [ ] **Calculate stakePercentage and delegationCapacity**
- [ ] **Update selftest.go for full Glacier parity**

---

## Research Findings

### Ordering/Pagination (from ~/glacier-api/)

**Glacier's ordering logic:**
- **Default sort field**: `blockIndex` (NOT startTimestamp!)
- **Default sort order**: `DESC`
- **Compound key**: `(sortByField, transactionHash)` for stable pagination
- **Available sortBy options**: `blockIndex`, `delegationCapacity`, `delegationFee`, `timeRemaining`, `uptimePerformance`

**Key SQL from Glacier** (`~/glacier-api/libs/database/src/queries/network-details/get-validator-details.sql`):
```sql
ORDER BY
    (CASE WHEN :sortBy = 'blockIndex' THEN v.block_index ... END) DESC,
    v.transaction_hash DESC
```

**Our fix needed:**
1. Change index key from `vs:{subnetId}:{startTs}:{txHash}` to `vs:{subnetId}:{blockIndex}:{txHash}`
2. Store `blockHeight` in ValidatorRecord (currently we store it but don't use for sorting)
3. Iterate DESC by default
4. Use txHash as secondary sort for stable ordering

### Live Fields (from pending_rewards pattern)

The `pending_rewards` indexer fetches live data in the **API handler** (not the indexer):

```go
// In indexers/pending_rewards/api.go handleListPending():
validatorsJSON, err := p.rpc.GetCurrentValidators(r.Context(), []string{nodeIds})
```

This returns a struct with all live fields including:
- `potentialReward` (already calculated by the node)
- `delegators` array with their details
- `weight`, `startTime`, `endTime`

**Our approach:**
1. Store indexed data (from block processing) in Pebble
2. In API handler, call `GetCurrentValidators` RPC to get live fields
3. Merge indexed data with live data for response

### potentialRewards Calculation (from ~/avalanchego/)

**Good news**: We don't need to calculate it ourselves!

The `platform.getCurrentValidators` RPC returns `potentialReward` for each validator and delegator. The node pre-calculates this when a validator transitions from pending to current state.

**From avalanchego/vms/platformvm/service.go:839:**
```go
potentialReward := avajson.Uint64(currentStaker.PotentialReward)
```

**The formula (FYI, already implemented in node):**
```
Reward = RemainingSupply * (StakedAmount/ExistingSupply) * MintingRate * (Duration/MaxDuration)
```

---

## Implementation Plan

### Phase 1: Fix Ordering
**Files to modify**: `store.go`, `p_indexing.go`

1. Update `saveValidator()` to use `blockHeight` instead of `startTimestamp` in index keys:
   ```go
   // OLD: nodeKey := []byte(fmt.Sprintf("vn:%s:%020d:%s", rec.NodeID, rec.StartTimestamp, rec.TxHash))
   // NEW: nodeKey := []byte(fmt.Sprintf("vn:%s:%020d:%s", rec.NodeID, rec.BlockHeight, rec.TxHash))
   ```

2. Update `listValidators()` to iterate in DESC order (currently uses `iter.Last()` + `iter.Prev()` which is correct)

3. Test against Glacier with specific validators

### Phase 2: Add Live Fields via RPC
**Files to modify**: `validators.go`, `api.go`

1. Add `rpc *pchain.CachedClient` field to `Validators` struct
2. Update `New()` to accept `*pchain.CachedClient` parameter
3. Update `cmd/server/main.go` and `cmd/test/main.go`:
   ```go
   // OLD: validatorsIndexer := validators.New()
   // NEW: validatorsIndexer := validators.New(cachedRPC)
   ```

4. In `handleListValidators()` and `handleGetValidator()`:
   - For active validators, call `GetCurrentValidators` RPC
   - Merge RPC response with indexed data
   - Calculate derived fields

### Phase 3: Full Field Support
- `stakePercentage`: `validator.weight / sum(all_validators.weight) * 100`
- `delegationCapacity`: `min(5 * stake, 3e15) - weight` (from Glacier SQL)
- `delegatorCount`: `len(validator.delegators)`
- `amountDelegated`: `sum(delegator.weight for each delegator)`
- `potentialRewards.validationRewardAmount`: from RPC `potentialReward`
- `potentialRewards.delegationRewardAmount`: sum of delegator potentialRewards after fee split

---

## API Response Schema (from Glacier)

### Validation Status Types
- `active` - Currently validating (startTimestamp <= now < endTimestamp)
- `pending` - Not yet started (now < startTimestamp)
- `completed` - Finished naturally (endTimestamp <= now) with rewards
- `removed` - Removed before endTimestamp (permissioned subnet validators)

### Common Fields (all statuses)
```json
{
  "txHash": "string",           // Validator registration tx ID
  "nodeId": "string",           // NodeID-...
  "subnetId": "string",         // Primary or subnet ID
  "amountStaked": "string",     // nAVAX as string
  "startTimestamp": number,     // Unix timestamp
  "endTimestamp": number,       // Unix timestamp
  "delegationFee": "string",    // Optional, primary network only (e.g., "2.0000000000000000")
  "blsCredentials": {           // Optional, permissionless validators only
    "publicKey": "string",
    "proofOfPossession": "string"
  }
}
```

### Active Validator Fields
```json
{
  "validationStatus": "active",
  "stakePercentage": number,        // From RPC (calculate from total)
  "validatorHealth": {...},         // SKIP - requires external monitoring
  "delegatorCount": number,         // From RPC delegators array
  "amountDelegated": "string",      // Sum from RPC delegators
  "potentialRewards": {             // From RPC potentialReward field
    "validationRewardAmount": "string",
    "delegationRewardAmount": "string",
    "rewardAddresses": ["string"]
  },
  "uptimePerformance": number,      // SKIP - requires external monitoring
  "avalancheGoVersion": "string",   // SKIP - requires external monitoring
  "delegationCapacity": "string",   // Calculate: min(5*stake, 3e15) - weight
  "geolocation": {...}              // SKIP - requires IP geolocation
}
```

### Completed Validator Fields
```json
{
  "validationStatus": "completed",
  "delegatorCount": number,
  "amountDelegated": "string",
  "rewards": {
    "validationRewardAmount": "string",   // From reward UTXOs in blocks DB
    "delegationRewardAmount": "string",
    "rewardAddresses": ["string"],
    "rewardTxHash": "string"
  }
}
```

### Pending Validator Fields
```json
{
  "validationStatus": "pending"
  // Only common fields
}
```

### Removed Validator Fields
```json
{
  "validationStatus": "removed",
  "removeTxHash": "string",
  "removeTimestamp": number
}
```

---

## P-Chain Transactions to Index

| Transaction Type | When | Fields |
|-----------------|------|--------|
| `AddValidatorTx` | Primary network validator | nodeId, stake, start/end, delegationFee, rewardsOwner |
| `AddDelegatorTx` | Primary network delegator | nodeId, stake, start/end, rewardsOwner |
| `AddPermissionlessValidatorTx` | Post-Durango validator | nodeId, stake, start/end, fee, BLS, subnet |
| `AddPermissionlessDelegatorTx` | Post-Durango delegator | nodeId, stake, start/end, subnet |
| `AddSubnetValidatorTx` | Permissioned subnet validator | nodeId, subnet, stake, start/end |
| `RemoveSubnetValidatorTx` | Remove permissioned validator | nodeId, subnet |
| `RewardValidatorTx` | Validation complete | stakingTxId -> rewards |

## Storage Schema (Pebble)

**Updated for blockIndex ordering:**
```
v:{txHash}                          -> ValidatorRecord (main record)
vn:{nodeId}:{blockHeight}:{txHash}  -> "" (index: by nodeId, sorted by block)
vs:{subnetId}:{blockHeight}:{txHash} -> "" (index: by subnet, sorted by block)
d:{txHash}                          -> DelegatorRecord
dn:{validatorTxHash}:{txHash}       -> "" (index: delegators by validator)
sv:{stakingTxHash}                  -> validatorTxHash (mapping for rewards)
```

## Query Parameters to Support

List validators:
- `pageSize` (default 10, max 100)
- `pageToken` (pagination - base64 encoded offset for now)
- `nodeIds` (comma-separated, substring match)
- `validationStatus` (active/pending/completed/removed)
- `subnetId` (filter by subnet)
- `sortBy` (blockIndex, delegationCapacity, delegationFee, timeRemaining, uptimePerformance)
- `sortOrder` (asc/desc, default desc)

Get single validator:
- `pageSize`, `pageToken` (same node can have multiple validations)
- `validationStatus` (filter)
- `sortOrder`

---

## Test Commands

```bash
# Build check
go build -o /tmp/server ./cmd/server && go build -o /tmp/test ./cmd/test

# Run validators test (fresh)
go run ./cmd/test validators --fresh

# Compare with Glacier directly
curl -s "https://glacier-api.avax.network/v1/networks/fuji/validators?pageSize=1&subnetId=11111111111111111111111111111111LpoYY" | jq .
curl -s "http://localhost:8080/v1/networks/fuji/validators?pageSize=1&subnetId=11111111111111111111111111111111LpoYY" | jq .
```

## Reference Files

- **Glacier validators query**: `~/glacier-api/libs/database/src/queries/network-details/get-validator-details.sql`
- **pending_rewards pattern**: `indexers/pending_rewards/api.go` (lines 89-101 for RPC call)
- **avalanchego reward calc**: `~/avalanchego/vms/platformvm/reward/calculator.go`
- **GetCurrentValidators RPC**: `~/avalanchego/vms/platformvm/service.go` (lines 708-970)
