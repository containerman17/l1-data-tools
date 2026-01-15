# Subnets Indexer Implementation

## What
Implement `GET /v1/networks/{network}/subnets` and `GET /v1/networks/{network}/subnets/{subnetId}` endpoints. This indexer will track `CreateSubnetTx` transactions on the P-Chain to build a registry of subnets.

## Why
Subnets are a core part of the Avalanche ecosystem. This indexer allows users to list all subnets and get details about specific subnets, matching the functionality provided by Glacier API.

## Progress
- [x] Create indexer structure and storage (`indexers/subnets/`)
- [x] Implement `TransferSubnetOwnershipTx` to track latest ownership
- [x] Implement proper sorting (descending by block index) for list endpoint
- [x] Handle `ConvertSubnetToL1Tx` for L1 subnets
- [x] Verify with `go run ./cmd/test subnets`

## Technical Reference: Subnet Lifecycle & L1 Conversion

### Key P-Chain Transactions to Index

| Transaction Type | Relevance | Key Payload Fields |
| :--- | :--- | :--- |
| **`CreateSubnetTx`** | **Birth**: First appearance of a Subnet. | `Owner`: The control keys/threshold for the subnet. |
| **`ConvertSubnetToL1Tx`** | **Evolution**: Converts a subnet to an **L1** (ACP-77). | `SubnetID`, `ChainID`, `Address` (Manager address), `Validators` (Initial L1 stakers). |
| **`TransformSubnetTx`** | **Permissionless**: Turns a private subnet into a permissionless one. | `AssetID` (Staking token), `MinValidatorStake`, `MaximumSupply`. |
| **`CreateChainTx`** | **Activity**: Creates a blockchain on a subnet. | `SubnetID`, `ChainName`, `VMID`, `GenesisData`. |
| **`TransferSubnetOwnershipTx`** | **Control**: Changes who manages the subnet. | `SubnetID`, `Owner` (New ownership structure). |

### Determining "Still Alive" Status
A subnet is practically "alive" if it has active validators. In your indexer logic:
*   **Active if**: Has ≥ 1 validator where `now() < EndTime`.
*   **Validator Sources**: 
    *   `AddSubnetValidatorTx` (Permissioned)
    *   `AddPermissionlessValidatorTx` (Transformed)
    *   `RegisterL1ValidatorTx` (L1/Etna)
*   **Removal Signals**:
    *   `RemoveSubnetValidatorTx` (Manual)
    *   `DisableL1ValidatorTx` (L1 specific)
    *   **Expiration**: Automatic via `End` timestamp.

### L1 Conversion (ACP-77 / Etna)
The **`ConvertSubnetToL1Tx`** is the explicit signal that a Subnet is now an L1. 
*   **Manager Address**: Contained in the `Address` field.
*   **P-Chain Rent**: Once an L1, keep tracking `IncreaseL1ValidatorBalanceTx` to ensure validators have balance to stay registered on the P-Chain.
*   **Weight Management**: `SetL1ValidatorWeightTx` updates the power of L1 validators.

---

## Known Issues (2024-12-21)

### ✅ Issue 2: Pagination Not Implemented - FIXED
**Symptom**: `pageSize=1` returns all subnets instead of 1.
**Fix Applied**: Added `pageSize` parameter to `listSubnets()` and parsed in `handleListSubnets`.

### ✅ Issue 3: Blockchains Array Shows `null` - FIXED
**Symptom**: Some subnets show `blockchains: null` instead of `blockchains: []`.
**Fix Applied**: Ensured `listSubnetBlockchains()` returns empty slice; added nil check in `getSubnet()`.

---

### ✅ Issue 1: ConvertSubnetToL1Tx Not Updating Subnet - FIXED
**Symptom**: `isL1: false` for subnets that Glacier shows as `isL1: true` (Beam at block 105088, Expired at block 248824).
**Fix**: Used in-memory `pendingSubnets` map to cache subnet metadata during batch processing. `getSubnetForUpdate()` checks cache first before DB.

### ✅ Issue 4: subnetOwnershipInfo Not Updated - FIXED
**Symptom**: Beam subnet shows original 5 owners instead of Glacier's 7 owners.
**Resolution**: Node RPC `platform.getSubnet` confirms our indexer is CORRECT (7 keys, threshold 2). Glacier API returns stale/incorrect data. Test updated with SkipFields.

---

## Fix Plan

### Step 1: Add Import for io
The `store.go` now uses `io.Closer` but may be missing the import.

### Step 2: Verify ConvertSubnetToL1Tx Case is Hit
Add temporary logging to confirm the switch case is matched and the save is called.

### Step 3: Check Batch Commit Order
The batch is committed at the end of `ProcessPChainBatch`. Ensure the modified `meta` is being saved correctly.

### Step 4: Run Tests
```bash
go run ./cmd/test subnets --fresh
```

Expected: All 6 tests should pass after fixes.

---

## Debug Log (2024-12-21 07:05)

**Finding**: All `ConvertSubnetToL1Tx` calls fail with `pebble: not found`:
```
[subnets] ConvertSubnetToL1Tx for subnet ie1wUBR2... at block 198392
[subnets] ERROR: could not find subnet ie1wUBR2...: pebble: not found
```

**Root Cause Confirmed**: `pebble.Batch.Get()` does NOT read uncommitted writes from the same batch. The `CreateSubnetTx` write is in the batch but `ConvertSubnetToL1Tx` cannot read it.

**Fix**: Use an in-memory map (`pendingSubnets`) during `ProcessPChainBatch` to cache newly created subnets, then check this map before falling back to DB.

---

## Node Verification (2024-12-21 07:08)

Queried node directly with `platform.getSubnet` for Beam subnet:
```json
{
  "controlKeys": [
    "P-fuji1pcaxpgflxxus0rxc2fnch4yjh2cze8t3h0uqjs",
    "P-fuji1yqc66ar59xudvgjt0k9y8xqzxu7x4798zqg8f3",
    ... (7 total)
  ],
  "threshold": "2"
}
```

**Conclusion**: Node shows 7 keys with threshold 2. Our local indexer is CORRECT. Glacier API shows 5 addresses with threshold 1, which is INCORRECT.

The remaining test difference for Beam is due to Glacier returning stale/incorrect data, not our implementation.
