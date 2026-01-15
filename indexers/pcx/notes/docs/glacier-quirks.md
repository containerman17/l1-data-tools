# Glacier API Quirks & Implementation Details

This document tracks specific behaviors of the Glacier API that differ from raw node RPCs or require special handling to emulate correctly.

## P-Chain UTXOs

### 1. Staked UTXO Listing vs. Balances
Glacier includes active staked UTXOs in the `/utxos` list, even though they are not immediately spendable. However, for balance calculations, these are categorized as `unlockedStaked` (if the staking period is active) or `unlockedUnstaked` (if the staking period has ended).

- **Implementation**: We use a flag `includeConsumedStaked` in `getUTXOsForAddresses` to toggle this behavior between the UTXO listing endpoint and the balance aggregation endpoint.

### 2. Output Metadata Suppression
For any UTXO where `staked: true`, Glacier omits internal consumption metadata to keep the response clean, even if the indexer tracks this data.

- **Suppressed Fields**:
  - `consumingTxHash`
  - `consumingBlockNumber`
  - `consumingBlockTimestamp`

### 3. Principal Return Logic
A common misconception is that staking UTXOs are "consumed" by the reward transaction. In reality:
- The **Stake Output (Principal)** remains a valid, unspent UTXO with its original `txID`.
- Glacier correctly tracks this as an unspent UTXO that becomes `unlockedUnstaked` once the `endTime` is reached.
- Our local implementation must NOT mark these as consumed when processing `RewardValidatorTx`.

---

## X-Chain UTXOs

### 1. Output Index Offsets
In certain X-Chain transactions (like `CreateAssetTx` or `OperationTx`), the UTXO indices in Glacier may include offsets based on the number of non-UTXO generating outputs in the transaction.

- **Implementation**: We manually calculate the offset in `x_indexing.go` by checking for the presence of specific transaction fields (like `States` in `CreateAssetTx`).

---

## Subnets

### 1. Stale `ownerAddresses` After Ownership Transfer
Glacier may return stale `ownerAddresses` and `threshold` values for subnets that have undergone a `TransferSubnetOwnershipTx`.

**Example**: Beam Subnet (`ie1wUBR2bQDPkGCRf2CBVzmP55eSiyJsFYqeGXnTYt2r33aKW`)
- **Glacier returns**: 5 addresses, threshold 1
- **Node RPC (`platform.getSubnet`) returns**: 7 addresses, threshold 2
- **Verified**: 2024-12-21 via Fuji node RPC

**Workaround**: The `subnetOwnershipInfo` field appears to be correct in Glacier. Our test skips `ownerAddresses` and `threshold` fields for this subnet and relies on our own indexed values which match the node.

---
*Last Updated: 2025-12-21*
