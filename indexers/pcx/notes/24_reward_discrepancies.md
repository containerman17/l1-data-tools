# Reward Indexer Debugging

## Discovered Discrepancies

After expanding the self-tests for `historical_rewards`, several issues were identified when comparing local results with the Avalanche Glacier API:

1. **`startTimestamp` Differences**: Local indexer used the `StartTime` from the `AddValidatorTx` (the intended start time), whereas Glacier uses the actual **block timestamp** where the staking transaction was accepted. This can differ by several minutes.
2. **Missing Rewards in blocks with multiple `RewardValidatorTx`**: Some blocks (especially `StandardBlock` post-Banff) can contain multiple `RewardValidatorTx` entries. The initial implementation only processed the first one.
3. **Missing `VALIDATOR_FEE`**: Delegators pay a fee to the validator. These are separate reward UTXOs. Glacier groups them by the recipient address. Our indexer was missing these because they were tracked under the validator's node ID but not explicitly associated with the validator's reward address during delegator reward processing.

## Current Fixes

1. **Timestamp Alignment**: Updated `historical_rewards/indexing.go` to use the block timestamp as the `startTimestamp`.
2. **Multi-Reward Support**: 
    - Updated `pchain/fetcher.go` to iterate through all transactions in a block and pre-fetch rewards for all `RewardValidatorTx` found.
    - Updated the block storage format to pack multiple reward sets into a single entry.
    - Updated `runner/p_runner.go` to decode and attach multiple reward sets to the `PBlock`.
3. **Validator Fees**: Added a `VALIDATOR_FEE` record type in the database and a `validator_fee_owners` table to track who should receive delegation fees for each NodeID.

---

## 2024-12-20: Fixed Missing Rewards & Amount Discrepancies ✅

### Problem
Test for `fuji1ur873jhz9qnaqv5qthk5sn3e8nj3e0kmafyxut` failed:
- **Glacier returned 10 rewards**, local returned only **5**
- Some amounts were ~2% higher than expected

### Debugging Journey

1. **Initial Hypothesis**: Reward amounts included validator fee  
   - Confirmed: `GetRewardUTXOs` returns **2 UTXOs** (delegator reward + validator fee)
   - Old code summed ALL UTXOs instead of matching by owner address

2. **Added Address Matching** (`api.go`)
   - Parse `reward_addrs` from staking record
   - Match UTXO owner addresses against staking addresses
   - Only use UTXO that belongs to the delegator → **Fixed amount discrepancy**

3. **Still Missing 5 Rewards**
   - Debug logging revealed: lazy loading ran for wrong txIDs
   - Some txIDs returned 0 UTXOs (low uptime = no rewards distributed)
   - Those NONE records filled pagination slots, pushing valid ones out

4. **Root Cause: Pagination Bug**
   - Query fetched `pageSize+1` rows, but some became NONE after lazy-load
   - NONE rows were filtered post-query, leaving fewer than pageSize results
   - Solution: Fetch 3x rows (`fetchLimit = (pageSize+1)*3`) to ensure enough valid results

5. **Off-by-One in Result Trimming**
   - Changed result loop to break at `pageSize+1`
   - Added explicit trimming: `historicalRewards = historicalRewards[:pageSize]`

6. **Pagination Token Format**
   - Glacier uses UUID format, we use base64-encoded offset
   - Added `SkipFields: ["nextPageToken"]` to selftest

### Files Changed

| File | Change |
|------|--------|
| `historical_rewards/api.go` | Address matching, 3x fetch limit, result trimming |
| `historical_rewards/selftest.go` | Added `SkipFields` for pagination token |

### Result
```
=== SUMMARY ===
Passed: 10, Failed: 0
```

---

## Notes

- **Low Uptime = No Rewards**: Some staking txIDs return 0 reward UTXOs from `GetRewardUTXOs`. This is expected behavior - validators/delegators with <80% uptime don't receive rewards. These are correctly marked as `NONE` in our DB.
- **Pagination Token Format**: We use base64-encoded offset, Glacier uses UUIDs. Functionally equivalent, just different format.

## How to Reproduce

To run the full suite of reward tests (warning: slow on fresh start due to syncing):

```bash
go run cmd/test/main.go historical_rewards --fresh
```
