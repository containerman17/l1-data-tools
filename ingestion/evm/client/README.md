# EVM Ingestion Client

Go client library for streaming blocks from ingestion/evm/rpc server.

## Usage

```go
import "github.com/containerman17/l1-data-tools/ingestion/evm/client"

c := client.NewClient("localhost:9090")
err := c.Stream(ctx, 1, func(blocks []client.Block) error {
    for _, b := range blocks {
        fmt.Printf("Block %d: %d txs\n", b.Number, len(b.Data.Block.Transactions))
    }
    return nil
})
```

## Backpressure Buffering

The client includes memory-safe buffering that prevents OOM when processing falls behind incoming blocks. When processing keeps up, behavior is unchanged from simple read-process loops.

### Configuration

| Parameter      | Default | Description                                          |
|----------------|---------|------------------------------------------------------|
| `MaxBatchSize` | 30 MB   | Maximum compressed data per processing cycle         |
| `BufferSize`   | 150 MB  | Maximum compressed data to hold in receive buffer    |

```go
cfg := client.BufferConfig{
    MaxBatchSize: 100 * 1024 * 1024,  // 100 MB batches
    BufferSize:   500 * 1024 * 1024,  // 500 MB buffer
}
c := client.NewClient("localhost:9090", client.WithBufferConfig(cfg))
```

### Memory Budget

- **Receive buffer**: `BufferSize` (compressed data)
- **Processing working set**: `MaxBatchSize × ~10` (decompressed, varies with compression ratio)

Increase `MaxBatchSize` for higher throughput on machines with more RAM. OOM errors indicate the parameter is set too high for available memory.

### How It Works

1. Receiver goroutine reads compressed blocks from WebSocket into a bounded buffer
2. When buffer is full, backpressure propagates via TCP/WebSocket flow control
3. Processor goroutine slices batches from buffer, decompresses, and calls handler
4. Single oversized blocks (> `MaxBatchSize`) are still processed (never rejected)

## Example

```bash
go build -o example-client ./ingestion/evm/client/example
./example-client -addr localhost:9090 -from 1
```
