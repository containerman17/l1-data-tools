# Golden File Testing for Self-Tests

**Goal:** Replace live Glacier API comparisons with hardcoded fixtures for historical/immutable data.

## Problem

Current self-tests always hit Glacier API live, which causes:
- Rate limiting (429 errors)
- Flaky tests when Glacier is slow/down
- Tests can't run offline
- Unnecessary API calls for data that never changes

## Self-Test Classification

### 🔴 Must Be Live (Current State)

| Indexer | Reason |
|---------|--------|
| `pending_rewards` | Rewards accumulate in real-time, expire when staking ends |
| `network_stats` | Live validator counts, staking ratios, total stakes |
| `validators` | Active status, uptime, health — all live node data |

### 🟢 Can Use Golden Files (Historical/Immutable)

| Indexer | Reason |
|---------|--------|
| `historical_rewards` | Finalized rewards never change |
| `utxos` (historical) | Past balances at specific timestamps are immutable |
| `list_chain_ids` | Chain interactions are append-only history |
| `assets` | Asset metadata is immutable once created |
| `blockchains` | Blockchain metadata is mostly immutable |
| `subnets` | Subnet creation data is immutable |

## Proposed Solution

### Step 1: Add Golden File Fields to TestCase

```go
// indexer/api.go
type TestCase struct {
    // ... existing fields ...
    GoldenFile   string  // Path to expected JSON response (relative to testdata/)
    RecordGolden bool    // If true, save response as new golden file
}
```

### Step 2: Add Golden File Logic to Test Runner

```go
// selftest/selftest.go

func runTestCase(tc indexer.TestCase, localBase string) bool {
    // If golden file specified, compare against file instead of Glacier
    if tc.GoldenFile != "" {
        return runGoldenTest(tc, localBase)
    }
    // ... existing Glacier comparison logic ...
}

func runGoldenTest(tc indexer.TestCase, localBase string) bool {
    localURL := buildURL(localBase, tc.Path, tc.Params)
    localResp, localSize, localErr := fetchJSON(localURL)
    
    if tc.RecordGolden {
        // Save response as new golden file
        return saveGoldenFile(tc.GoldenFile, localResp)
    }
    
    // Load and compare against golden file
    expected, err := loadGoldenFile(tc.GoldenFile)
    if err != nil {
        return false
    }
    
    // Use existing comparison logic
    diffs := compareResponses(expected, localResp, skipSet, approxFields)
    return len(diffs) == 0
}
```

### Step 3: Create Golden Files Directory

```
indexers/
├── historical_rewards/
│   ├── selftest.go
│   └── testdata/
│       ├── historical-by-address-fuji1jun9yd.golden.json
│       └── historical-by-address-fuji1rjjy.golden.json
```

### Step 4: Mark Tests as Golden

```go
// indexers/historical_rewards/selftest.go
testCases = append(testCases, indexer.TestCase{
    Name:       "historical-by-address-" + address,
    Path:       "/v1/networks/testnet/rewards",
    Params:     map[string]string{"addresses": address, "pageSize": "10"},
    GoldenFile: "historical-by-address-" + address + ".golden.json",
    // No more live Glacier comparison
})
```

## Implementation Checklist

- [ ] Add `GoldenFile` and `RecordGolden` fields to `TestCase`
- [ ] Add golden file loading/saving functions in `selftest/selftest.go`
- [ ] Add `runGoldenTest()` function
- [ ] Create `testdata/` directories for each indexer
- [ ] Record initial golden files from Glacier
- [ ] Update `historical_rewards/selftest.go`
- [ ] Update `list_chain_ids/selftest.go`
- [ ] Update `assets/selftest.go`
- [ ] Update `blockchains/selftest.go`
- [ ] Update `subnets/selftest.go`
- [ ] Update historical UTXO/balance tests in `utxos/selftest.go`

## Usage

```bash
# Normal run (uses golden files where specified)
go run ./cmd/test historical_rewards

# Record new golden files (run after verifying Glacier parity)
go run ./cmd/test historical_rewards --record-golden

# Force live comparison (ignore golden files)
go run ./cmd/test historical_rewards --live
```

## Benefits

1. **Fast tests** — No network latency, no rate limits
2. **Offline capable** — Tests work without internet
3. **Deterministic** — Same input, same output, always
4. **Easy updates** — Re-record when legitimate changes occur
5. **Backward compatible** — Tests without golden files still hit Glacier
