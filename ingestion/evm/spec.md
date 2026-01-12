# EVM Ingestion: Technical Spec

## 1. WebSocket Streaming
- **Endpoint**: `/ws?from={uint64}`
- **Format**: Binary frames, `zstd` compressed JSONL.
- **Batching**:
  - `StorageBatchSize = 100` blocks per frame (historical).
  - 1 block per frame (tip/real-time).
- **Alignment**:
  - Request starts at `from`.
  - If `from` is mid-batch, server sends full 100-block batch.
  - Client MUST filter `block.number < from`.

## 2. Data Schema

```go
type NormalizedBlock struct {
    Block    Block                 `json:"block"`    // standard EVM block w/ txs
    Traces   []TraceResultOptional `json:"traces"`   // one per tx
    Receipts []Receipt             `json:"receipts"` // one per tx
}

type TraceResultOptional struct {
    TxHash string     `json:"txHash"`
    Result *CallTrace `json:"result"` // nil for precompile failures
}

type CallTrace struct {
    From, To, Value, Gas, GasUsed string
    Input, Output, Error, Type    string
    Calls                         []CallTrace `json:"calls,omitempty"` // recursive
}
```

## 3. Wire Format
- **Compression**: `zstd` on all frames.
- **Frame Content**: JSONL (one `NormalizedBlock` per line).

## 4. Catalogue Endpoint
**`GET /chains`** returns JSON object keyed by `blockchainId`:

```json
{
  "2q9e...": { "name": "DFK", "evmChainId": 53935, "subnetId": "Vn3...", "indexer": "/indexer/2q9e.../ws" },
  "2Z36...": { "name": "Swimmer", "evmChainId": 73772, "subnetId": "2Do...", "indexer": "/indexer/2Z36.../ws" }
}
```

Indexer path pattern: `/indexer/{blockchainId}/ws?from={block}`

Additional fields allowed; listed fields required.
