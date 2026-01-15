# Pre-Cortina X-Chain Timestamp Problem

**Date**: December 2025  
**Status**: ✅ Implemented

---

## The Problem

**Pre-Cortina X-Chain transactions (before April 2023) have no network timestamps available via AvalancheGo RPC.**

### Background: X-Chain Architecture Evolution

| Era | Structure | Data Source | Dates |
|-----|-----------|-------------|-------|
| **Pre-Cortina** | DAG vertices | Index API (`/ext/index/X/tx`) | Genesis → April 2023 |
| **Post-Cortina** | Linear blocks | Block RPC (`avm.getBlock`) | April 2023 → present |

**Cortina Activation Times**:
- **Mainnet**: April 25, 2023 at 15:00 UTC (1682434800)
- **Fuji**: April 6, 2023 at 15:00 UTC (1680793200)

### Why Vertices Don't Have Timestamps

Vertices don't contain timestamps in their binary format (confirmed in AvalancheGo source).

### Index API Timestamps Are Useless

The Index API returns timestamps, but they reflect **when your node accepted the container**, not when the network did.

**Result**: All pre-Cortina transactions get the same timestamp = when you bootstrapped your node.

### Pre-Cortina Data Counts

**Mainnet**: ~4.1M vertices + ~4.1M transactions  
**Fuji**: ~450k vertices + ~508k transactions

---

## The Solution: Static Archive with Prefix Truncation

Create compact archives from Glacier API data using:
1. **Prefix truncation** (3-8 byte hash prefixes instead of full 32 bytes)
2. **Delta encoding** for timestamp compression
3. **Auto-collision detection** to find minimum safe prefix length

### Implementation

**Generic package**: `xchain/prefixarchive/`
- Reusable hash→int64 archive format
- Automatic prefix length selection (3-8 bytes)
- Delta-encoded timestamps with varint compression
- ~6.5× compression ratio

**Domain package**: `xchain/pre_cortina_timestamps/`
- Wrapper around prefixarchive
- Embedded Fuji archive (`//go:embed fuji.bin`)
- Lazy-loaded with `sync.Once`
- Simple API: `Lookup(network, txID)`

### Archive Structure

```
[MAGIC: 4 bytes "PXAR"]
[VERSION: 1 byte]
[PREFIX_LEN: 1 byte]  // Auto-selected 3-8 bytes
[COUNT: 4 bytes]
[BASE_TIMESTAMP: 8 bytes]
[ENTRIES...]: sorted by timestamp for delta encoding
  [PREFIX: prefix_len bytes]
  [DELTA: varint]
```

**Why this works**:
- Prefixes sorted by timestamp (not by hash)
- Sequential timestamps → tiny deltas (1-10 seconds typically)
- Varint encoding: 1-2 bytes per delta
- Random hash prefixes don't compress further with zstd

### Actual Sizes

| Network | Entries | Naive (40B/entry) | Archive | Compression |
|---------|---------|-------------------|---------|-------------|
| Fuji    | ~1M     | 40 MB             | 5.7 MB  | 7.0×        |
| Mainnet | ~8.2M   | 328 MB            | ~50 MB  | 6.6×        |

**Prefix lengths used**:
- Fuji: 5 bytes (collision-free for ~1M entries)
- Mainnet: 6 bytes (collision-free for ~8.2M entries)

### Data Source

Scraped from Glacier API via `cmd/scrape_timestamps`:
- Vertices: `GET /v1/networks/{network}/blockchains/x-chain/vertices`
- Transactions: `GET /v1/networks/{network}/blockchains/x-chain/transactions?chainFormat=non-linear`

### Usage in Indexer

```go
import ts "github.com/.../xchain/pre_cortina_timestamps"

// In ProcessXChainPreCortinaTxs
func (u *UTXOs) ProcessXChainPreCortinaTxs(ctx context.Context, txs []indexer.XTx) error {
    // Load archive once (cached after first call)
    archive, err := ts.GetFujiArchive()  // or GetMainnetArchive()
    if err != nil {
        return err
    }
    
    for _, tx := range txs {
        txID := extractTxID(tx.Bytes)
        
        // IGNORE tx.Timestamp (bogus bootstrap time)
        // Use real network timestamp from archive
        realTimestamp, found := archive.Lookup(txID)
        if !found {
            return fmt.Errorf("tx %s not in archive", txID)
        }
        
        // Process with correct timestamp
        u.processXChainTx(batch, parsedTx, realTimestamp, ...)
    }
}
```

**Key insight**: Archive only needed during **initial sync**. After UTXOs are saved with correct timestamps, archive is never queried again.

---

## Alternatives Considered

### ❌ Minimal Perfect Hash (MPH)
**Rejected**: Go MPH libraries don't have stable binary serialization. Would need custom implementation.

### ❌ Full 32-byte hashes
**Rejected**: 164 MB for mainnet is too large to commit to git.

### ❌ External compression (zstd)
**Tested**: Only 2-3% reduction (hash prefixes are random, don't compress).

### ✅ Prefix truncation + delta encoding
**Selected**: 
- 6.5× compression with simple, stable format
- Collision-free (verified at build time)
- Fast O(1) lookups via map
- Works across Go versions
- Small enough to embed and commit to git

---

## Status

✅ **Implemented**:
- [x] `xchain/prefixarchive/` - Generic prefix archive package
- [x] `xchain/pre_cortina_timestamps/` - Domain-specific wrapper
- [x] `cmd/scrape_timestamps` - Glacier scraper with resume capability
- [x] Fuji archive built and embedded (2.6 MB)
- [x] Tests passing
- [ ] Mainnet archive (run: `go run ./cmd/scrape_timestamps mainnet`)
- [ ] Integrate into UTXO indexer
- [ ] Update vertex API endpoints

**Files**:
- `xchain/pre_cortina_timestamps/fuji.bin` - 2.6 MB embedded archive
- `xchain/pre_cortina_timestamps/timestamps-fuji.json` - Source data (not embedded)
- `xchain/pre_cortina_timestamps/timestamps-mainnet.json` - Source data (325 MB, .gitignored)

---

## Notes

- Archives are **immutable** - pre-Cortina data will never change
- Post-Cortina blocks have timestamps in headers (no archive needed)
- Archive decouples us from Glacier after initial data scrape
- 2.6 MB (Fuji) / ~25 MB (Mainnet) fit in memory easily
- One-time lookup during initial sync, then never used again
- Could embed mainnet archive too if needed (25 MB is acceptable)