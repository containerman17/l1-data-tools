# Pre-Cortina Timestamps

Compact archive for pre-Cortina X-Chain timestamps. Provides O(1) lookup for ~4.5M entries in ~25 MB.

## Why

AvalancheGo's Index API timestamps reflect when *your node* accepted a container, not when the network did. For pre-Cortina X-Chain data, we need accurate network timestamps from Glacier.

## Implementation

Built on top of the generic `xchain/prefixarchive` package, which provides:
- Prefix truncation (3-8 bytes instead of 32)
- Delta encoding for timestamps
- ~6.5× compression ratio

## Files

- `archive.go` - Thin wrapper around `prefixarchive` with domain-specific API
- `fuji.bin` - Fuji (testnet) timestamps (~450k entries, 2.6 MB)
- `mainnet.bin` - Mainnet timestamps (~4.1M entries, ~25 MB)
- `timestamps-*.json` - Source JSON data (not embedded, kept for rebuilding)

## Size

| Dataset  | Entries | Full Hashes | This Format |
|----------|---------|-------------|-------------|
| Fuji     | ~1M     | 40 MB       | 5.7 MB      |
| Mainnet  | ~8.2M   | 328 MB      | ~50 MB      |

## Usage

```go
import ts "github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/xchain/pre_cortina_timestamps"

// Simple lookup (loads archive automatically on first call)
timestamp, found, err := ts.Lookup("fuji", vertexID)
if err != nil {
    // Handle error
}
if found {
    // Use timestamp
}

// Or get the archive directly for multiple lookups
archive, err := ts.GetFujiArchive()  // Cached after first call
if err != nil {
    // Handle error
}
timestamp, found := archive.LookupVertex(vertexID)
timestamp, found = archive.LookupTransaction(txID)

// Mainnet (not yet implemented)
_, _, err = ts.Lookup("mainnet", txID)
// Returns: "mainnet archive not implemented yet"
```

## Rebuilding

To rebuild the binary archives (e.g., if the JSON source changes):

```bash
cd /home/ubuntu/devrel-experiments/03_data_api/p-chain-indexer
go run ./cmd/scrape_timestamps testnet   # Rebuilds fuji.bin
go run ./cmd/scrape_timestamps mainnet   # Rebuilds mainnet.bin
```

## Context

See `notes/20_timestamp_problem.md` for why these archives exist.
See `xchain/prefixarchive/README.md` for format details.

