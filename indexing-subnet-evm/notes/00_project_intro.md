# AI Context: Indexing Subnet-EVM Plugin

## What This Is

VM wrapper for subnet-evm that intercepts block acceptance and indexes blocks with traces/receipts. Runs as an AvalancheGo plugin.

## Critical Knowledge

### State History Is In-Memory

`state-history: 128` in subnet-evm config is an **IN-MEMORY buffer** that is **LOST ON RESTART**.

- Tracing requires block state (accounts, storage)
- State only commits to disk every ~4096 blocks (CommitInterval)
- Between commits, state lives in memory only
- On restart, only tip state is regenerated
- `reexec=0` in subnet-evm means NO re-execution for tracing

**Consequence:** If indexer falls behind and node restarts, those blocks' traces are gone forever.

### Backpressure Mechanism

`block.go` Accept() blocks if indexer falls too far behind:
```go
threshold := stateHistory - 2
if height - lastIndexed >= threshold {
    // wait for indexer to catch up
}
```
This prevents the chain from outrunning the indexer.

### On Restart

`vm.go` Initialize():
1. `lastAcceptedHeight` = chain tip from `vm.chain.CurrentBlock()`
2. `lastIndexedHeight` = max(GetMeta(), LatestBlock()) - checks both compacted and individual blocks
3. If gap > stateHistory - 10: FATAL with rm command
4. If gap ≤ threshold: indexer catches up

### Why Accept() Isn't Called On Restart

Accept() only fires for NEW blocks during runtime. On restart, existing blocks are loaded from DB, not re-accepted. This is why we must initialize lastAcceptedHeight from chain state.

## File Responsibilities

- `main.go` - Entry point, calls `rpcchainvm.Serve()`
- `vm.go` - IndexingVM wrapper, Initialize/Shutdown, state management
- `block.go` - IndexingBlock wrapper, Accept() with backpressure
- `indexer.go` - Background loop, parallel fetch, batch indexing
- `validate.go` - Post-bootstrap validation (compares DB vs fresh RPC)
- `marshal.go` - RPC-compatible marshaling (copied from subnet-evm internals)

## Storage Structure

Uses shared `evm-ingestion/storage` package:
- Individual blocks: `block:00000000000000001234` → JSON
- Compressed batches: `batch:00000000000000001234-00000000000000001333` → zstd(JSONL)
- Meta: last compacted block number (NOT last indexed!)

Path: `~/.avalanchego/chainData/{chainID}/indexer/`

## Common Issues

### "lastAccepted: 0" in logs
Missing initialization of lastAcceptedHeight from chain. Fixed by reading `vm.chain.CurrentBlock()` in Initialize().

### Gap between meta and actual indexed blocks
Meta stores last COMPACTED block, not last indexed. Individual blocks above meta may exist. Fixed by checking max(meta, latestIndividualBlock).

### "required historical state unavailable (reexec=0)"
Gap exceeded state history window. State is gone. Must wipe indexer DB and resync.

## Dependencies

- `evm-ingestion/storage` - PebbleDB storage, compactor
- `evm-ingestion/api` - Firehose WebSocket server
- `github.com/ava-labs/subnet-evm` - Base VM
- `github.com/ava-labs/avalanchego` - Plugin infrastructure

## Environment

- `GRPC_INDEXER_CHAIN_ID` - Required, chain ID to index (others rejected)
- Plugin built to `~/.avalanchego/plugins/{vmID}`
- vmID computed from "indexing-subnet-evm" string

