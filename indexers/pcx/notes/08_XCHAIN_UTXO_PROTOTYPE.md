# X-Chain UTXO Prototype

## Goal
Create an in-memory X-chain UTXO indexer that reproduces what Glacier API returns for X-chain UTXOs.

## Decisions
- **Data Source**: Use already fetched blocks from `xchain/fetcher.go` (stored in pebble DB)
- **Network**: Fuji testnet
- **Structure**: Both pre-Cortina (DAG) and post-Cortina (linear blocks)
- **Test Addresses**: 28 Fuji addresses (see main.go)

## Pre-Cortina vs Post-Cortina Architecture

### The Problem
- Pre-Cortina (before April 6, 2023 on Fuji): X-chain was a DAG, no linear blocks
- Post-Cortina: X-chain has linear blocks
- Need ALL transactions to build complete UTXO history

### Solution: Two Data Sources
1. **Pre-Cortina**: Index API (`index.getContainerRange`) - requires `--index-enabled=true`
2. **Post-Cortina**: Block RPC (`avm.getBlockByHeight`) - standard approach

**Key Finding**: Index API ONLY contains pre-Cortina transactions. Post-Cortina transactions are ONLY in blocks. No overlap! This simplifies the fetcher - just fetch all from Index API, then all blocks.

### Storage Format
- `tx:{index}` → pre-Cortina transaction bytes (from Index API)
- `blk:{height}` → post-Cortina block bytes (from block RPC)

### Cortina Boundary (Fuji)
- **Activation**: April 6, 2023 at 15:00 UTC
- **First block timestamp**: 2023-04-06T15:01:51Z
- **Stop Vertex**: `2D1cmbiG36BqQMRyHt4kFhWarmatA1ighSpND3FeFgz3vFVtCZ`
- **Pre-Cortina tx count**: 508,607 (as of Dec 2025)

### Indexer Interface Design
```go
type XChainIndexer interface {
    OnPreCortinaTx(tx *txs.Tx, index uint64)  // DAG era
    OnBlock(block avmblock.Block)              // Linear era
}
```
Most indexers (UTXO, balance) have internal `processTx()` called from both.
Block-specific indexers implement them separately.

## AVM Transaction Types (from avalanchego)
1. **BaseTx** - Basic transfer with Ins/Outs
2. **CreateAssetTx** - Create new assets (extends BaseTx, adds States with outputs)
3. **OperationTx** - NFT/property operations (extends BaseTx, adds Ops with outputs)
4. **ImportTx** - Import from another chain (extends BaseTx, adds ImportedIns)
5. **ExportTx** - Export to another chain (extends BaseTx, adds ExportedOuts)

## UTXO Creation Logic (from visitor.go:utxoGetter)
- **BaseTx/ImportTx/ExportTx**: UTXOs from `tx.Outs` at indices 0, 1, 2, ...
- **CreateAssetTx**: `tx.Outs` + `tx.States[i].Outs` (asset outputs come after)
- **OperationTx**: `tx.Outs` + `tx.Ops[i].Op.Outs()` (operation outputs come after)

## UTXO Consumption
- All tx types: consume via `tx.Ins` (inputs reference previous UTXOs by TxID:OutputIndex)
- ImportTx: also consumes `tx.ImportedIns` (from other chain - P or C)

## Cross-Chain UTXOs

### The Challenge
X-chain UTXOs can be created or consumed on different chains via atomic transactions:
- **Export X→P/C**: Creates UTXO on X-chain (`ExportTx.ExportedOuts`), consumed on P/C-chain
- **Import P/C→X**: UTXO created on P/C-chain, consumed on X-chain (`ImportTx.ImportedIns`)

### ExportTx.ExportedOuts (X-chain → P/C-chain)
- `ExportTx.ExportedOuts` creates UTXOs on X-chain that are destined for P/C-chain
- These UTXOs have `CreatedOnChainId = X-chain ID`
- They are consumed on P/C-chain (via `ImportTx.ImportedIns` on P/C)
- **We CAN track these** - they are X-chain UTXOs
- Output indices start AFTER `tx.Outs` (e.g., if 2 outputs, exported starts at index 2)

### ImportTx.ImportedIns (P/C-chain → X-chain)
- `ImportTx.ImportedIns` consumes UTXOs that were created on P/C-chain
- These UTXOs have `CreatedOnChainId = P-chain or C-chain ID`
- **We CANNOT track creation** - requires P/C-chain indexing
- We only see the consumption on X-chain

### Prototype Limitation
Single-chain indexer cannot match Glacier for cross-chain UTXOs:
- **Exported UTXOs (X→P/C)**: Can track creation, cannot track consumption (happens on P/C)
- **Imported UTXOs (P/C→X)**: Cannot track creation, can track consumption

### Solution in Prototype
Filter comparison to only X-chain native UTXOs:
```go
xChainID := "2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm" // Fuji
if remote.CreatedOnChainId != xChainID {
    continue // Skip UTXOs created on P/C chain
}
```

For full multi-chain support, need to index P-chain and C-chain atomic transactions too.

## Tasks
- [x] Understand X-chain data structure
- [x] Understand AVM tx types
- [x] Create glacier.go for X-chain
- [x] Create main.go with UTXO tracking
- [x] Compare with Glacier API results - PASS (post-Cortina only)
- [x] Identify pre-Cortina gap (423 UTXOs missing)
- [x] Research pre-Cortina indexing solution (Index API)
- [x] Update xchain/fetcher.go to support both eras (RunPreCortina method)
- [x] Discover: Index API only has pre-Cortina txs (no timestamp filtering needed)
- [x] Update prototype to use combined data source
- [x] Add ExportTx.ExportedOuts processing (was missing exported UTXOs)
- [x] Document cross-chain UTXO limitations
- [x] Filter cross-chain UTXOs in comparison (skip P/C-chain created UTXOs)
- [x] Full validation against Glacier (all 28 addresses, X-chain native UTXOs) - PASSED

## Final Validation Results (Dec 2025)
- **Total Transactions**: 544,837 (508,608 pre-Cortina + 36,229 post-Cortina)
- **Addresses Tested**: 28 (all passed)
- **Total UTXOs**: 22,960 across all test addresses
- **Total Balance**: ~746k AVAX
- **Cross-chain filtering**: Skipped UTXOs created on P/C-chain
- **API rate limiting**: Required 500ms delay + exponential backoff retry

## Key Implementation Details
- Must use all 3 FXs: secp256k1fx, nftfx, propertyfx (type ID 20 error without nftfx)
- Storage location: `./data/{networkID}/blocks/x/` in pebble DB
- Key formats:
  - Pre-Cortina: `tx:{index_uint64_bigendian}` → raw tx bytes
  - Post-Cortina: `blk:{height_uint64_bigendian}` → block bytes
- Prototype is fail-fast by design

## Files
```
experiments/x_chain_utxos_prototype/
├── main.go      # Entry point + UTXO comparison
├── glacier.go   # Fetch UTXOs from Glacier API
└── data/        # Symlink or path to xchain fetcher data
```
