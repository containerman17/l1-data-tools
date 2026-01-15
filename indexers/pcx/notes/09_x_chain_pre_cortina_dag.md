# X-Chain Pre-Cortina Transaction Download Guide

**Date**: December 2025  
**Purpose**: How to download ALL X-Chain transactions including pre-Cortina (pre-April 2023)

---

## The Solution: Index API

Run AvalancheGo with indexing enabled:

```bash
avalanchego --index-enabled=true
```

**Critical**: Start with a fresh database to index complete history. The node will index all transactions during bootstrapping.

---

## Downloading All Transactions

### Step 1: Get Total Count

```bash
curl -X POST 'localhost:9650/ext/index/X/tx' \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc": "2.0",
    "method": "index.getLastAccepted",
    "params": {
      "encoding": "hex"
    },
    "id": 1
  }'
```

Response: `"index": "12345"` ← total transaction count (0-indexed)

### Step 2: Download in Batches (1024 at a time)

```bash
curl -X POST 'localhost:9650/ext/index/X/tx' \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc": "2.0",
    "method": "index.getContainerRange",
    "params": {
      "startIndex": 0,
      "numToFetch": 1024,
      "encoding": "hex"
    },
    "id": 1
  }'
```

Increment `startIndex` by 1024 each iteration until you reach the total count.

### Response Format

```json
{
  "id": "28nhjXPwt5QnwW5XjoLn6eqktMJbRyp3v9PUqrT5GAxoVRqZVZ",
  "bytes": "0x00000000...",
  "timestamp": "2024-07-21T09:11:21.81069928Z",
  "index": "0"
}
```

- **`id`**: Transaction ID
- **`bytes`**: Raw transaction bytes (hex)
- **`timestamp`**: When node accepted it
- **`index`**: Sequential position

---

## Verified Test

```bash
curl -X POST 'localhost:9650/ext/index/X/tx' \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc": "2.0",
    "method": "index.getContainerRange",
    "params": {
      "startIndex": 0,
      "numToFetch": 3,
      "encoding": "hex"
    },
    "id": 1
  }'
```

✅ Returns transactions with IDs, bytes, timestamps

---

## Important Notes

- **Index API only contains pre-Cortina transactions** (from vertices)
- Post-Cortina transactions are NOT in Index API - they're only in blocks
- Sequential index (0, 1, 2, ...) is consistent
- Timestamps reflect when **this node** accepted the transaction (not original tx time!)
- Max 1024 transactions per request
- Pre-Cortina transactions don't have block heights
- Fuji: 508,607 pre-Cortina transactions (as of Dec 2025)

---

## Cortina Transition

- **Mainnet**: April 25, 2023 at 15:00 UTC
- **Fuji**: April 6, 2023 at 15:00 UTC
- **Stop Vertex (Mainnet)**: `jrGWDh5Po9FMj54depyunNixpia5PN4aAYxfmNzU8n752Rjga`
- **Stop Vertex (Fuji)**: `2D1cmbiG36BqQMRyHt4kFhWarmatA1ighSpND3FeFgz3vFVtCZ`

---

## Summary

Two API calls:
1. **`index.getLastAccepted`** → get total count
2. **`index.getContainerRange`** → batch download (1024 at a time)

---

## Implementation in xchain/

### Constants
```go
const cortinaTimeFuji = 1680793200      // April 6, 2023 15:00 UTC
const cortinaTimeMainnet = 1682434800   // April 25, 2023 15:00 UTC
```

### Storage Format
- Pre-Cortina: `tx:{index_uint64_bigendian}` → raw tx bytes
- Post-Cortina: `blk:{height_uint64_bigendian}` → block bytes
- Metadata: `meta:preCortinaDone` → marks pre-Cortina fetch complete

### Tasks
- [x] Add Index API methods to client.go (GetLastAcceptedIndex, GetContainerRange)
- [x] Add pre-Cortina fetcher to fetcher.go (RunPreCortina method)
- [x] Update cmd/main.go to call RunPreCortina before Run
- [x] Add GetTx, PreCortinaCount, IsPreCortinaDone methods
- [ ] Test with actual indexed node
- [ ] Update prototype to use combined data source
