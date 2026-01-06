# EVM Sink

High-throughput EVM blockchain data ingestion service. Pulls blocks, receipts, and traces from RPC nodes, stores locally with compaction, and streams to consumers via WebSocket with zstd-compressed frames.

## Quick Start

```bash
# Build
go build -o sink ./cmd/sink

# Run (single chain, env config)
RPC_URL=http://avalanche-node:9650/ext/bc/C/rpc ./sink
```

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `RPC_URL` | Yes | - | Chain RPC endpoint |
| `PEBBLE_PATH` | No | `./data/pebble` | Database path |
| `SERVER_ADDR` | No | `:9090` | WebSocket server address |
| `MAX_PARALLELISM` | No | `200` | Max concurrent RPC requests |
| `LOOKAHEAD` | No | `100` | Sliding window size for fetching |

## How It Works

```
RPC Node → [Ingestion] → PebbleDB → [Compaction] → PebbleDB (batches)
     ↑                                    ↓
  WebSocket                          WebSocket → Consumers
 (newHeads)                         (zstd frames)
```

1. **Head Tracking**: WebSocket subscription to `newHeads` for instant block notifications
2. **Ingestion**: Sliding window fetcher pulls blocks with receipts and traces in parallel
3. **Compaction**: Background process compacts old blocks (100 at a time) to compressed batches
4. **Serving**: WebSocket streaming of zstd-compressed blocks

## Storage

All data stored in PebbleDB:

- **Individual blocks**: `block:{blockNum:020d}` → JSON
- **Compressed batches**: `batch:{start:020d}-{end:020d}` → zstd JSONL (100 blocks)
- **Metadata**: `meta` → last compacted block

Compaction runs automatically, keeping ~1000 individual blocks as buffer while converting older blocks to compressed batches (~3-5x compression).

## Consumer Client

```go
import "github.com/ava-labs/devrel-experiments/03_data_api/ingestion/client"

// Stream block packs (one websocket frame = one pack)
// Historical: up to 100 blocks per pack
// Real-time: 1 block per pack
c := client.NewClient("localhost:9090")
err := c.Stream(ctx, 1, func(blocks []client.Block) error {
    for _, b := range blocks {
        fmt.Printf("Block %d: %d txs\n", b.Number, len(b.Data.Block.Transactions))
    }
    return nil
})
```

## Example Client

```bash
# Build example client
go build -o example-client ./cmd/example-client

# Stream blocks (WebSocket)
./example-client -addr localhost:9090 -from 1
```

## Protocol

**Stream blocks (WebSocket):**
```
GET /ws?from=12345  (upgrade to WebSocket)
← [BINARY] zstd(NormalizedBlock\nNormalizedBlock\n...)  // batch, ~100 blocks
← [BINARY] zstd(NormalizedBlock\n)                      // live block
```

All WebSocket frames are binary with zstd-compressed JSONL. Historical data sends compressed batches as-is (~100 blocks per frame). Live blocks are compressed individually.

Client decompresses each frame, splits on `\n`, parses each line as `NormalizedBlock` JSON. First frame may contain blocks before `from` (due to batch alignment) - client filters them out.

## Adaptive Rate Limiting

The `MAX_PARALLELISM` env var is the only knob. The system automatically:
- Starts at min parallelism (10% of max) and climbs up
- Increases parallelism when P95 < 1.2s
- Reduces parallelism if P95 > 2s
- Halves parallelism on errors
- Adjusts every 2s based on 60s sliding window

Target: maximize throughput without overloading RPC node.

## Block Data Format

Each block is stored as a `NormalizedBlock`:

```go
type NormalizedBlock struct {
    Block    Block                 `json:"block"`
    Receipts []Receipt             `json:"receipts"`
    Traces   []TraceResultOptional `json:"traces"`
}
```

- **Traces**: `debug_traceBlockByNumber` with `callTracer`

## Ingestion Progress

The sink logs progress every 5 seconds:
```
block 50234567 | 142.3 blk/s avg | 1234 behind, eta 8s | p=50 p95=450ms
```

- `blk/s avg`: average since start
- `behind`: blocks remaining to sync
- `eta`: estimated time to catch up
- `p=50`: current parallelism level
- `p95=450ms`: P95 request latency
