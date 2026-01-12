# Block Indexer Batching Layer Specification

## Purpose

Add memory-safe buffering to the existing block subscription system. When processing keeps up with incoming blocks, behavior is unchanged from the current implementation. When processing falls behind, the system applies backpressure instead of consuming unbounded memory.

## Parameters

| Parameter        | Default             | Description                                                      |
|------------------|---------------------|------------------------------------------------------------------|
| `max_batch_size` | 30 MB               | Maximum compressed data to slice from queue per processing cycle |
| `buffer_size`    | 5 × max_batch_size  | Maximum compressed data to hold in receive buffer                |

Users may increase `max_batch_size` for higher throughput (expect values up to 250 MB on beefy machines). Out-of-memory errors indicate the parameter is set too high for available RAM—this is expected behavior, not a bug.

## Memory Budget

- **Receive buffer**: `buffer_size` (compressed)
- **Processing working set**: `max_batch_size × ~10` (decompressed, varies with compression ratio)

## Architecture

```
[WebSocket] → [Receive Buffer] → [Batch Slice] → [Decompress] → [Process] → [Indexer]
                    ↑
             backpressure when full
```

## Receive Buffer Behavior

- Incoming blocks (compressed, ZSTD) accumulate in buffer
- Each block is variable size (KB to tens of MB)
- When `buffer_used >= buffer_size`: stop accepting new blocks (backpressure)
- Resume accepting when buffer drains below threshold

## Processing Loop

```
loop:
    if buffer is empty:
        wait 1ms
        continue
    
    slice blocks from buffer where total_compressed_size <= max_batch_size
    (take at least one block even if it exceeds max_batch_size)
    
    decompress slice
    send to batch processor / indexer
```

Key properties:

- No waiting to fill batch—take whatever is available immediately
- Batch size is a maximum, not a target
- Single oversized block is processed alone (never rejected)
- Self-adjusts: fast processing = small batches, slow processing = larger batches

## Backpressure Mechanism

When buffer is full, stop reading from WebSocket. The TCP/WebSocket flow control will propagate backpressure to the origin server. The 5× buffer provides headroom for in-flight data during backpressure propagation (relevant for high-latency connections up to ~200ms RTT).

## Implementation Notes

- This is additive to current implementation—when blocks process faster than they arrive, behavior is identical to current "read block, process, repeat" flow
- Only difference: memory is bounded, and slow processing triggers backpressure instead of OOM
- All size accounting uses compressed size (pre-decompression)
- Decompression happens only when batch is ready to process, not on receive

---

## Current Client Architecture (Reference)

The existing `client.go` has:

```go
type Client struct {
    addr      string
    conn      *websocket.Conn
    zstdDec   *zstd.Decoder
    reconnect bool
}
```

### Current Flow (`Stream` method)

```go
func (c *Client) Stream(ctx context.Context, fromBlock uint64, handler Handler) error {
    for {
        // connect
        for {
            blocks, err := c.readPack()   // ← blocking read, immediate decompress
            // filter, call handler
        }
    }
}
```

### Current `readPack`

```go
func (c *Client) readPack() ([]Block, error) {
    _, data, err := c.conn.ReadMessage()     // ← get compressed data
    decompressed, err := c.zstdDec.DecodeAll(data, nil)  // ← immediate decompress
    // parse JSON newline-delimited blocks
    return blocks, nil
}
```

---

## Proposed Changes

### 1. Add Buffering Configuration

```go
type BufferConfig struct {
    MaxBatchSize int64  // default: 30 * 1024 * 1024 (30 MB)
    BufferSize   int64  // default: 5 * MaxBatchSize
}

func DefaultBufferConfig() BufferConfig {
    return BufferConfig{
        MaxBatchSize: 30 * 1024 * 1024,
        BufferSize:   150 * 1024 * 1024,  // 5 × 30 MB
    }
}
```

### 2. Receive Buffer Structure

```go
type receiveBuffer struct {
    mu          sync.Mutex
    items       []bufferedItem  // queue of compressed payloads
    totalSize   int64           // sum of compressed sizes
    maxSize     int64           // buffer_size limit
    cond        *sync.Cond      // for signaling
}

type bufferedItem struct {
    compressedData []byte
    size           int64
}
```

### 3. Modified Stream Method

Split into two goroutines:

**Receiver goroutine** (reads from WebSocket → buffer):
```go
func (c *Client) receiveLoop(ctx context.Context, buf *receiveBuffer) {
    for {
        // Wait if buffer is full (backpressure)
        buf.waitForSpace()
        
        _, data, err := c.conn.ReadMessage()
        if err != nil { return }
        
        buf.push(data)  // add compressed payload
    }
}
```

**Processor goroutine** (buffer → decompress → handler):
```go
func (c *Client) processLoop(ctx context.Context, buf *receiveBuffer, handler Handler) {
    for {
        batch := buf.sliceBatch(c.config.MaxBatchSize)
        if len(batch) == 0 {
            time.Sleep(1 * time.Millisecond)
            continue
        }
        
        var blocks []Block
        for _, item := range batch {
            decompressed, _ := c.zstdDec.DecodeAll(item.compressedData, nil)
            // parse blocks...
            blocks = append(blocks, ...)
        }
        
        handler(blocks)
    }
}
```

### 4. Buffer Slicing Logic

```go
func (buf *receiveBuffer) sliceBatch(maxSize int64) []bufferedItem {
    buf.mu.Lock()
    defer buf.mu.Unlock()
    
    if len(buf.items) == 0 {
        return nil
    }
    
    var batch []bufferedItem
    var batchSize int64
    
    for i, item := range buf.items {
        if i > 0 && batchSize + item.size > maxSize {
            break  // would exceed max, stop here
        }
        batch = append(batch, item)
        batchSize += item.size
        // First item always included, even if oversized
    }
    
    // Remove from buffer
    buf.items = buf.items[len(batch):]
    buf.totalSize -= batchSize
    buf.cond.Signal()  // wake receiver if waiting
    
    return batch
}
```

### 5. Backpressure Wait

```go
func (buf *receiveBuffer) waitForSpace() {
    buf.mu.Lock()
    defer buf.mu.Unlock()
    
    for buf.totalSize >= buf.maxSize {
        buf.cond.Wait()  // blocks until space available
    }
}

func (buf *receiveBuffer) push(data []byte) {
    buf.mu.Lock()
    defer buf.mu.Unlock()
    
    buf.items = append(buf.items, bufferedItem{
        compressedData: data,
        size:           int64(len(data)),
    })
    buf.totalSize += int64(len(data))
    buf.cond.Signal()  // wake processor if waiting
}
```

---

## API Changes

### New Option

```go
func WithBufferConfig(cfg BufferConfig) Option {
    return func(c *Client) {
        c.bufferConfig = cfg
    }
}
```

### Backward Compatibility

- Default behavior unchanged when processing keeps up
- Existing `Stream(ctx, from, handler)` signature preserved
- New `BufferConfig` options are additive

---

## Metrics to Add

| Metric                         | Type    | Description                           |
|--------------------------------|---------|---------------------------------------|
| `buffer_used_bytes`            | Gauge   | Current compressed bytes in buffer    |
| `buffer_capacity_bytes`        | Gauge   | Buffer size limit                     |
| `batches_processed_total`      | Counter | Number of batches processed           |
| `batch_size_bytes`             | Histogram | Compressed size per batch           |
| `backpressure_wait_seconds`    | Histogram | Time spent waiting for buffer space |

---

## Edge Cases

1. **Single oversized block** (> `max_batch_size`): Process alone, no rejection
2. **Empty handler call**: Never happens—always at least one block in batch
3. **Context cancellation**: Clean shutdown of both goroutines
4. **WebSocket disconnect**: Existing reconnect logic unchanged
5. **Handler error**: Propagated up, stops processing

---

## Testing Strategy

1. **Unit tests**: Buffer slicing logic, backpressure triggering
2. **Integration tests**: Slow handler simulating backpressure
3. **Stress tests**: High-throughput scenarios with varying block sizes
4. **Memory profiling**: Verify bounded memory under load
