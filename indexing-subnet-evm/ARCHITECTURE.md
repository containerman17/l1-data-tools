# Indexing Plugin Architecture

## Overview

The plugin stores indexed blocks **locally in the same database as the node** (AvalancheGo's built-in database via `prefixdb`). No external dependencies.

## Data Flow

```
Accept(block)
    │
    ├─→ Check lag against stateHistory threshold
    │
    └─→ If lag >= threshold: indexBatch()
            │
            ├─→ Parallel fetch (direct blockchain access)
            │   - block data (vm.chain.GetBlockByNumber)
            │   - receipts (vm.chain.GetReceiptsByHash)
            │   - traces (vm.tracerAPI.TraceBlockByNumber)
            │
            ├─→ Marshal to RPC format
            │
            └─→ Atomic batch write to indexDB
                    key: block:{chainID}:{height:020d}
                    value: JSON {block, receipts, traces}
```

## Compaction (In-Place)

Every 3 seconds, the compactor checks if there are enough blocks buffered:

```
Compactor Loop
    │
    ├─→ Check: (latestBlock - firstBlock) > MinBuffer + BatchSize?
    │
    └─→ If yes:
            ├─→ Read 100 blocks from indexDB
            ├─→ Compress to JSONL + zstd
            ├─→ Save as batch:{chainID}:{start:020d}-{end:020d}
            └─→ Delete individual block keys
```

**Benefits:**
- Reduces key count by 100x
- ~3-5x compression ratio
- Faster range reads (sequential I/O)

## Firehose Server

WebSocket server on `:9090`:

```
GET /chains         → JSON list of registered chains
GET /ws?chain=X&from=Y → WebSocket block stream

Stream Priority:
1. Individual blocks from indexDB (recent)
2. Compressed batches from indexDB (historical)
```

All data sent as zstd-compressed JSONL (1-100 blocks per frame).

## Key Format

```
Individual blocks:  block:{chainID}:{height:020d}
Compressed batches: batch:{chainID}:{start:020d}-{end:020d}
Metadata:           last_indexed_height
```

## Configuration

Environment variables:
- `GRPC_INDEXER_CHAIN_ID` - Required. Chain ID to run (other chains fail to initialize)

## Constants

```go
BatchSize = 100                     // blocks per compressed batch
MinBlocksBeforeCompaction = 1000    // keep this many individual blocks
CompactionCheckInterval = 3s        // how often to check for compaction
```

## Design Rationale

The plugin is **stateful by design** - it lives with the node. If the node dies, you re-bootstrap anyway. Benefits of local storage:

1. **No external dependencies** - no credentials, no network issues
2. **Low latency** - microseconds for local reads
3. **Simple operations** - one thing to manage
4. **Co-hosted cost** - 600GB node + 600GB index ≈ 2x storage, acceptable for most use cases
