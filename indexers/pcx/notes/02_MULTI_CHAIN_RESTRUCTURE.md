# Multi-Chain Restructuring (P/X/C)

## Goal
Restructure the indexer to support P-Chain, X-Chain, and C-Chain (atomic data only) to provide a complete view of cross-chain imports/exports and shared memory UTXOs.

## User Requests
- Restructure project to support P, C, and X chains
- Need 3 fetchers (P, C, X)
- Need 3 runners/indexers
- Separate `apis` folders for each chain
- C-Chain and X-Chain indexers will be simpler than P-Chain
- Do NOT index EVM state for C-Chain (only atomic transactions/utxos)
- Support tracking imports/exports across chains
- **Current focus**: Just start ingesting X-chain blocks alongside P-chain

## Plan
- [x] Implement X-chain block fetcher
- [x] Integrate X-chain fetcher into main.go
- [x] Update /status endpoint for multi-chain
- [ ] Implement C-chain fetcher (atomic txs only)
- [ ] Create chain-specific indexers
- [ ] Implement cross-chain UTXO resolution logic

## Progress
- [x] Created `xchain/` directory with client.go and fetcher.go
- [x] X-chain client uses `/ext/bc/X` endpoint with `avm.getHeight` and `avm.getBlockByHeight`
- [x] X-chain fetcher stores raw blocks in separate pebble DB (`data/{networkID}/blocks/x/`)
- [x] Both P and X fetchers run concurrently in main.go
- [x] /status endpoint now shows both chains' fetch progress
- [x] Tested on Fuji: X-chain syncs to ~36k blocks at ~10k blk/s

## Decisions
- Using shared `tx_metadata` store for cross-chain timestamp resolution
- Keeping EVM data out of scope (atomic only for C-Chain)
- Each chain has its own blocks DB under `data/{networkID}/blocks/{p,x,c}/`
- X-chain fetcher reuses same batch-parallel pattern as P-chain



