# Self-Test Rewrite

## Problem

The current self-test system is overcomplicated:
- Requires managing background processes (AI is bad at this)
- HTTP polling for sync status
- Two terminals needed (server + test runner)
- Restarts forgotten when code changes

## Requirements

1. **Single command** - one terminal, run and done
2. **Single indexer focus** - when debugging utxos, don't run pending_rewards/historical_rewards
3. **Fresh resync** - drop one indexer's data, resync, test
4. **Compare against Glacier** - reuse existing TestCases() definitions
5. **Clean exit** - no zombie processes, no manual cleanup

## Solution: Two Binaries

```
cmd/
  server/main.go   # production: all indexers, runs forever
  test/main.go     # dev: ONE indexer, resync, test, exit
```

### Production Server

```bash
go run ./cmd/server
```

- Starts all indexers (utxos, pending_rewards, historical_rewards, etc.)
- Starts all fetchers (P, X, C chains)
- Runs HTTP API on :8080
- Runs forever until Ctrl+C

### Dev Test Tool

```bash
go run ./cmd/test utxos --fresh
```

1. `--fresh` flag drops `./data/{networkID}/utxos` directory
2. Starts ONLY the utxos indexer (less log noise)
3. Starts ONLY needed chain fetcher/runner (P-chain for utxos)
4. Blocks are already fetched (in `./data/{networkID}/blocks/`)
5. Waits for utxos indexer to catch up (direct DB check, not HTTP)
6. Runs utxos TestCases() against localhost vs Glacier
7. Prints pass/fail with diffs
8. Exits cleanly

## Workflow for AI-Assisted Development

```bash
# AI runs this single command repeatedly:
go run ./cmd/test utxos --fresh

# Output:
# 2024/12/17 ... Dropped utxos data
# 2024/12/17 ... Starting utxos indexer (P-chain only)
# 2024/12/17 ... Processing 253074 blocks...
# 2024/12/17 ... Synced in 4.2s
# 2024/12/17 ... Running 5 test cases...
# ✅ MATCH: list utxos for fuji1abc...
# ✅ MATCH: balances for fuji1xyz...
# ❌ DIFF: utxos with pagination
#    ... diff output ...
# 
# Passed: 4, Failed: 1
```

AI sees output → fixes code → runs again → repeat until all green.

## Implementation Notes

- `cmd/test/main.go` imports server components, doesn't spawn subprocess
- Indexer selection: only init/run the requested indexer
- Chain selection: indexer declares which chains it needs (P, X, C, or combo)
- Sync detection: check indexer watermark vs fetcher watermark directly
- Reuse existing: TestCases(), comparison logic, diff output from selftest.go
- **No Ctrl+C handling** - OS kills immediately, no graceful shutdown
  - All operations are atomic (Pebble writes, SQLite writes)
  - No risk of corruption on hard kill
  - Dev iteration speed > cleanup time

