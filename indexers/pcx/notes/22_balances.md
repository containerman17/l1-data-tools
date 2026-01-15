# P-Chain Stake Consumption & Balance Alignment

This document summarizes the final consolidated logic for P-chain UTXO indexing, focusing on how stake outputs and rewards are handled to align with both node behavior and Glacier's API.

## 1. The Core Discovery: Principal is Never Consumed

The most critical finding was that **staking principal is returned, not consumed**.

- **Incorrect Assumption**: Stake outputs are "spent" and replaced by reward UTXOs when staking ends.
- **Actual Behavior**: 
    1. The **Stake Output (Principal)** remains a valid, unspent UTXO with its original `txID` and `outputIndex`. It simply becomes spendable after the staking period ends.
    2. The **RewardValidatorTx** generates **NEW additional reward UTXOs**. It does NOT consume the original stake outputs.
    3. The owner receives: **Principal (original UTXO) + Rewards (new UTXOs).**

> [!IMPORTANT]
> This is why my initial implementation caused a balance shortage (it was incorrectly "consuming" the principal).

## 2. P-Chain Balance Rules

To match `platform.getBalance` and Glacier exactly, the following categorization is used:

| State | Condition | Category |
|-------|-----------|----------|
| **Staking Active** | `staked: true` AND `endTime > now` | `unlockedStaked` (or `lockedStaked`) |
| **Staking Ended** | `staked: true` AND `endTime <= now` | **`unlockedUnstaked`** (Principal returned!) |
| **New Reward** | `staked: false` (from RewardTx) | `unlockedUnstaked` |
| **Atomic** | `consumedOnChainID != P-Chain` | `atomicMemoryUnlocked` |

## 3. UTXO Listing Quirks (Glacier Alignment)

Glacier has specific requirements for the `/utxos` list that differ from balance logic:

1. **Include Active Staked UTXOs**: Even though they aren't "unlocked" for spending, Glacier includes them in the UTXO list.
2. **Hide Consumption Metadata**: For any UTXO where `staked: true`, we suppress the following fields in the JSON response even if they exist internally:
    - `consumingTxHash`
    - `consumingBlockNumber`
    - `consumingBlockTimestamp`
3. **Filter out Completed Stakers**: Once `endTime <= now`, the "staked" version of the UTXO is filtered out from active staker lists and effectively treated as a normal UNSTAKED UTXO.

## 4. Final Implementation Summary

### 📂 `indexers/utxos/p_indexing.go`
- **Logic**: Removed `markConsumed` for stake outputs in `processRewardTx`.
- **Logic**: Track stake output IDs during staking tx processing to allow correct categorization but maintain their "unspent" status.

### 📂 `indexers/utxos/api.go`
- **`getUTXOsForAddresses`**: Added `includeConsumedStaked` param. When true (for UTXO listings), it includes active stakers. When false (for balances), it excludes them to prevent double-counting or miscategorization of "locked" funds.
- **`aggregatePChainBalances`**: Implementation of the "Staking Ended → unlockedUnstaked" logic.
- **`toPChainResponse`**: Suppression of consumption fields for staked UTXOs.

## 5. Verification Results (22/22)

All automated tests now pass:
- ✅ **Balance match**: Local balance matches Node/Glacier exactly (3rd decimal point accuracy).
- ✅ **UTXO count match**: 27 UTXOs returned for the medium address, matching the node.
- ✅ **Categories**: Correct distribution between unlocked, staked, and atomic memory.

---
*Last Updated: 2025-12-20*