# Subnet-EVM Indexing Plugin

## The Idea

**Goal:** A prunable subnet-evm node that indexes itself - capturing all block data, receipts, and traces in real-time, stored locally.

**Why:**
- Regular archive nodes are expensive (full state history)
- RPC-based ingestion is slow and rate-limited
- In-process indexing captures data the moment blocks are accepted - no RPC overhead
- Pruning keeps disk usage low while still capturing everything we need

## Architecture

```
ingestion/cmd/subnet-evm-plugin/
├── main.go        # Entry point
├── vm.go          # IndexingVM wrapper
├── block.go       # IndexingBlock with Accept hook
├── indexer.go     # Fetch/marshal/store logic
├── marshal.go     # RPC-compatible marshaling
├── compactor.go   # In-place batch compression
└── ARCHITECTURE.md
```

### How It Works

1. **IndexingVM** wraps `evm.VM` and intercepts block methods
2. **IndexingBlock** wraps `snowman.Block` and intercepts `Accept()`
3. On `Accept()`:
   - Check lag against `stateHistory` threshold
   - If lag >= threshold, call `indexBatch()`
   - Uses **direct blockchain access** (via reflection):
     - `vm.chain.GetBlockByNumber()` - block data
     - `vm.chain.GetReceiptsByHash()` - receipts
     - `vm.tracerAPI.TraceBlockByNumber()` - traces
   - Marshal to RPC format using copied subnet-evm code
   - Atomic batch write to indexDB

4. **Compactor** runs every 3s:
   - When 1100+ blocks buffered, compress oldest 100
   - Store as `batch:{chainID}:{start}-{end}` key
   - Delete individual block keys

5. **Firehose Server** on `:9090`:
   - `GET /chains` - list registered chains
   - `GET /ws?chain=X&from=Y` - WebSocket block stream

### Config

| Variable | Description |
|----------|-------------|
| `GRPC_INDEXER_CHAIN_ID` | Required. Chain ID to run (other chains fail to initialize) |

### Output Format

Same as RPC ingestion:

```go
{
    "block":    {...},  // RPC-formatted block
    "traces":   [...],  // callTracer output
    "receipts": [...]   // RPC-formatted receipts
}
```

## Building & Running

```bash
cd ingestion/scripts
./plugin-dev.sh
```

This builds the plugin and starts avalanchego with it.

## Notes

1. **Go version:** Requires go 1.24.9+ (toolchain go1.24.11)
2. **Debug API:** `debug_traceBlockByNumber` requires debug APIs enabled in VM config
3. **Blocking indexing:** `indexBatch()` blocks `Accept()` - intentional to ensure no data loss
4. **Fatal on error:** Indexing failure = `os.Exit(1)` - don't continue with data gaps
