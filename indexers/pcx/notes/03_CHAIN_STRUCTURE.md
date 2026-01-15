# Chain Structure

## Directory Layout

Each chain has its own package with client and fetcher:

```
pchain/
  client.go         # RPC client for P-Chain (/ext/bc/P)
  cached_client.go  # Caching wrapper for immutable RPC data
  fetcher.go        # Block fetcher, stores raw bytes

xchain/
  client.go         # RPC client for X-Chain (/ext/bc/X)
  fetcher.go        # Block fetcher, stores raw bytes

cchain/
  client.go         # RPC client for C-Chain (/ext/bc/C/rpc)
  fetcher.go        # Stores blockExtraData only (atomic txs)
```

## Data Storage

Each chain stores blocks in a separate pebble DB:

```
data/{networkID}/
  blocks/
    p/              # P-Chain raw blocks
    x/              # X-Chain raw blocks
    c/              # C-Chain atomic data (future)
  rpc_cache/        # Cached RPC responses (immutable data)
  {indexer_name}/   # Per-indexer state (SQLite, etc.)
```

## RPC Endpoints

| Chain | Endpoint | Methods |
|-------|----------|---------|
| P-Chain | `/ext/bc/P` | `platform.getHeight`, `platform.getBlockByHeight` |
| X-Chain | `/ext/bc/X` | `avm.getHeight`, `avm.getBlockByHeight` |
| C-Chain | `/ext/bc/C/rpc` | `eth_blockNumber`, `eth_getBlockByNumber` (extracts `blockExtraData`) |

## Naming Conventions

- Package: `pchain`, `xchain`, `cchain`
- Client constructor: `NewClient(url string)`
- Fetcher constructor: `NewFetcher(client, db)`
- Log prefix: `[p-fetcher]`, `[x-fetcher]`, `[c-fetcher]`

## Status

- [x] P-Chain: client + fetcher working
- [x] X-Chain: client + fetcher working
- [x] C-Chain: client + fetcher working (stores blockExtraData only)

## C-Chain Storage Note

C-Chain fetcher stores only `blockExtraData` field (not full blocks) because:
- Atomic transactions (imports/exports) are fully contained in `blockExtraData`
- Standard EVM transactions don't contain UTXO data
- Much smaller storage footprint than full EVM blocks
- Empty blocks still stored (empty bytes) for database consistency
