# PCX Indexer Architecture

Multi-chain indexer for Avalanche P-Chain, X-Chain, and C-Chain (non-EVM data).

## Overview

```
┌─────────────────────────────────────────────────────────────┐
│                         HTTP API                             │
│  /v1/networks/{net}/blockchains/p-chain/utxos               │
│  /v1/networks/{net}/blockchains/x-chain/utxos               │
│  /v1/networks/{net}/blockchains/c-chain/balances            │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│                      Chain Indexers                          │
│  ┌─────────┐    ┌─────────┐    ┌─────────┐                  │
│  │ P-Chain │    │ X-Chain │    │ C-Chain │                  │
│  │ - UTXOs │    │ - UTXOs │    │ - Atomic│                  │
│  │ - Valid.│    │ - Assets│    │   UTXOs │                  │
│  └─────────┘    └─────────┘    └─────────┘                  │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│                    Shared Services                           │
│  ┌─────────────────────────────────────────────────────┐    │
│  │ tx_metadata: {chainID}:{txID} → {height, timestamp} │    │
│  └─────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│                       Block Storage                          │
│  data/{networkID}/                                          │
│    blocks/p/     - Full P-Chain blocks                      │
│    blocks/x/     - Full X-Chain blocks                      │
│    blocks/c/     - C-Chain blockExtraData only              │
│    indexes/p/    - P-Chain indexes (utxos, validators)      │
│    indexes/x/    - X-Chain indexes (utxos, assets)          │
│    indexes/c/    - C-Chain indexes (atomic utxos)           │
│    shared/       - Cross-chain metadata                     │
└─────────────────────────────────────────────────────────────┘
```

## Chains

### P-Chain
- **Block source**: `platform.getBlockByHeight`
- **Data indexed**: UTXOs, validators, delegators, subnets
- **Cross-chain**: Imports from X/C need source timestamps

### X-Chain  
- **Block source**: `avm.getBlockByHeight`
- **Data indexed**: UTXOs, assets, balances
- **Cross-chain**: Imports from P/C need source timestamps

### C-Chain
- **Block source**: `eth_getBlockByNumber` (only `blockExtraData` field)
- **Data indexed**: Atomic UTXOs (P↔C, X↔C transfers)
- **NOT indexed**: EVM state, contracts, ERC20s (use standard EVM indexer)
- **Cross-chain**: Imports from P/X need source timestamps

## Cross-Chain Resolution

### The Problem
When P-Chain sees `ImportTx` from X-Chain:
- P-Chain knows the source txID
- P-Chain needs: when was that UTXO created? (block height, timestamp)
- This data lives on X-Chain

### Solution: Lazy Resolution

**During indexing:**
1. Store cross-chain UTXOs with `blockHeight=0, timestamp=0` (placeholder)
2. Store `createdOnChainId` to know where to look

**On API query:**
1. If `blockTimestamp=0` and `createdOnChainId != P-Chain`:
2. Lookup `shared/tx_metadata/{sourceChain}:{sourceTxId}`
3. Return resolved timestamp (cache in stored UTXO for next query)

**Why lazy?**
- Indexing stays fast (no cross-chain RPC during batch processing)
- Cross-chain data might not exist yet (X-Chain behind P-Chain)
- Query volume is low enough that 2-3ms extra is fine

### tx_metadata Table

Each chain indexer writes metadata for every transaction:

```
Key:   tx:{chainID}:{txID}
Value: {blockHeight uint64, timestamp int64}
```

This allows any chain to lookup when a tx was included, regardless of source.

## Folder Structure

```
/cmd/
  main.go              # Entry point
/chains/
  interface.go         # Common interfaces (Indexer, Fetcher)
  pchain/
    fetcher.go         # Block fetching from RPC
    indexer.go         # Block processing, UTXO tracking
    handlers.go        # HTTP API handlers
  xchain/
    fetcher.go
    indexer.go  
    handlers.go
  cchain/
    fetcher.go         # Fetch blockExtraData only
    indexer.go         # Atomic UTXO processing
    handlers.go
/shared/
  tx_metadata.go       # Cross-chain tx → timestamp service
  resolver.go          # Lazy timestamp resolution
/db/
  pebble.go            # Database utilities
/indexer/
  runner.go            # Orchestrates all chain indexers
```

## Data Models

### StoredUTXO (all chains)
```go
type StoredUTXO struct {
    TxID             ids.ID
    OutputIndex      uint32
    Amount           uint64
    AssetID          ids.ID
    Addresses        []ids.ShortID
    Locktime         uint64
    Threshold        uint32
    BlockHeight      uint64    // 0 if cross-chain, unresolved
    BlockTimestamp   int64     // 0 if cross-chain, unresolved
    CreatedOnChainID string    // Source chain (for cross-chain resolution)
    ConsumingTxID    ids.ID    // Empty if unspent
    // ... chain-specific fields
}
```

### TxMetadata (shared)
```go
type TxMetadata struct {
    BlockHeight uint64
    Timestamp   int64
}
```

## Sync Strategy

### Initial Sync
1. Start all chain fetchers in parallel
2. Each chain indexes independently at max speed
3. Cross-chain UTXOs stored with placeholders
4. Resolution happens on first query

### Live Sync  
1. Each chain polls for new blocks
2. Process immediately
3. Cross-chain resolution same as above

### Reindex
1. Delete `indexes/*` (keep `blocks/*` and `shared/*`)
2. Re-run indexers over stored blocks
3. Much faster than re-fetching

## API Compatibility

Target: 100% Glacier API compatibility for supported endpoints.

### P-Chain
- `GET /v1/networks/{network}/blockchains/p-chain/utxos`
- `GET /v1/networks/{network}/blockchains/p-chain/balances`

### X-Chain
- `GET /v1/networks/{network}/blockchains/x-chain/utxos`
- `GET /v1/networks/{network}/blockchains/x-chain/balances`

### C-Chain (atomic only)
- `GET /v1/networks/{network}/blockchains/c-chain/balances` (atomic UTXOs)

EVM balances require a separate EVM indexer or direct RPC.

