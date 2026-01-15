# Historical Balances Strategy

Goal: Verify that `/v1/networks/{network}/blockchains/{blockchainId}/balances` with the `blockTimestamp` parameter correctly reflects the state of the blockchain at that specific point in time.

## Test Case Strategy

To ensure correctness and parity with Glacier, we need to test the following edge cases:

### 1. UTXO Lifecycle
- **Before Creation (T-1)**: Balance should NOT include the UTXO.
- **At Creation (T)**: Balance DOES include the UTXO.
- **After Creation (T+1)**: Balance DOES include the UTXO.
- **Before Consumption (T-1)**: Balance DOES include the UTXO.
- **At Consumption (T)**: Balance DOES include the UTXO (Glacier's behavior at T is usually inclusive of the block's effects).
- **After Consumption (T+1)**: Balance should NOT include the UTXO.

### 2. Staking Lifecycle (P-Chain)
- **Before Staking Start (T-1)**: Balance should be in `unlockedUnstaked` (or wherever it came from).
- **At Staking Start (T)**: Balance should move to `lockedStaked` (or `pendingStaked` if T < start_time).
- **During Staking**: Balance should be in `lockedStaked`.
- **At Staking End (T)**: Balance should move back to `unlockedUnstaked` (or `lockedNotStakeable` if platformLocktime still applies).
- **After Staking End (T+1)**: Balance should be in `unlockedUnstaked`.

### 3. Cross-Chain (Atomic Memory)
- **Export (T-1)**: Balance is in native chain.
- **Export (T)**: Balance is in `atomicMemoryUnlocked` or `atomicMemoryLocked`.
- **Import (T+1)**: Balance is NO LONGER in source chain's `atomicMemory`, it is now in destination chain.

## Target Addresses & Timestamps

### Small Addr: `fuji1udpqdsrf5hydtl96d7qexgvkvc8tgn0d3fgrym`
- UTXO `251U6QjaFnXQCFcuyqKgFb2F8k6kRkfBBByVujn2dMEPxaDHS9`
  - Created at `1765343147`
  - Consumed at `1765343209`
- **Test Timestamps**:
  - `1765343146`: Before creation (Balance -1)
  - `1765343147`: At creation (Balance +1)
  - `1765343208`: Before consumption (Balance +1)
  - `1765343209`: At consumption (Balance +1 / Transition)
  - `1765343210`: After consumption (Balance -1)

### Active Staker: `fuji16cvmzlnfysmu204kfp23994skw9ykre88eu7fk`
- UTXO `WeRGbyV8hXxjutktcn7FRincjbsYRKyp5Z1BhAbKaHLvuoaaK`
  - Staking Start: `1750839103`
  - Staking End: `1750925740`
- **Test Timestamps**:
  - `1750839102`: Before staking (Status: Unlocked/Unstaked)
  - `1750839103`: Staking started (Status: LockedStaked)
  - `1750925739`: Staking ending soon (Status: LockedStaked)
  - `1750925740`: Staking ended (Status: UnlockedUnstaked)

## Strategy for Implementation

1. **`getUTXOsForAddresses`**:
   - Add optional `blockTimestamp` parameter.
   - If provided, filter `StoredUTXO` items:
     - `CreatedTimestamp <= blockTimestamp`
     - `ConsumingTimestamp == nil || ConsumingTimestamp > blockTimestamp`
   - This effectively recreates the set of UTXOs that were "live" at that moment.

2. **Aggregation**:
   - Pass `blockTimestamp` to `aggregatePChainBalances` etc.
   - Use `blockTimestamp` instead of `time.Now().Unix()` for `isLocked`, `isPending`, `isEnded` checks.
   - This ensures that if we are looking at the past, we use the past's "now" to determine if something was locked OR if a staking period had ended *by that time*.
