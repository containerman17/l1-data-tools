# X-Chain UTXO Indexing

## Goal
Index X-Chain UTXOs to provide `/balances` and `/utxos` endpoints matching Glacier API.

## Target API

```
GET /v1/networks/{network}/blockchains/x-chain/balances?addresses=...
GET /v1/networks/{network}/blockchains/x-chain/utxos?addresses=...&includeSpent=true&sortBy=amount
```

### Balance Response Structure
```json
{
  "balances": {
    "unlocked": [{ "assetId": "...", "amount": "...", "utxoCount": N }],
    "locked": [],
    "atomicMemoryUnlocked": [],
    "atomicMemoryLocked": []
  },
  "chainInfo": { "chainName": "x-chain", "network": "fuji" }
}
```

### UTXO Response Structure
```json
{
  "utxos": [{
    "addresses": ["fuji1..."],
    "utxoId": "...",
    "asset": { "assetId": "...", "name": "...", "symbol": "...", "amount": "..." },
    "utxoType": "transfer",
    "creationTxHash": "...",
    "consumingTxHash": "...",
    "createdOnChainId": "...",
    "consumedOnChainId": "..."
  }],
  "nextPageToken": "...",
  "chainInfo": { "chainName": "x-chain", "network": "fuji" }
}
```

## X-Chain UTXO Characteristics

### Transaction Types
- **BaseTx** - Simple transfers (Ins → Outs)
- **ImportTx** - Import from P/C-Chain (ImportedIns from shared memory → Outs)
- **ExportTx** - Export to P/C-Chain (Ins → Outs + ExportedOuts to shared memory)
- **CreateAssetTx** - Create ANT (TxID becomes AssetID, initial supply in States)
- **OperationTx** - NFT ops (mint, transfer)

### Key Differences from P-Chain
- No `stakeable.LockOut` - only simple `secp256k1fx.TransferOutput`
- Multi-asset support (AVAX + ANTs)
- No staking-related outputs
- Codec version 0 (vs P-Chain uses platformvm codec)

### Balance Categories
- **unlocked** - `TransferOutput.Locktime <= now`
- **locked** - `TransferOutput.Locktime > now`
- **atomicMemoryUnlocked** - Exported to P/C (waiting for import)
- **atomicMemoryLocked** - Exported but time-locked

## Plan

### 1. Directory Restructure
```
apis/
  shared.go           # Common types (Asset, pagination, encoding)
  pchain/
    utxos.go          # Current utxos.go (rename chainInfo)
    pending_rewards.go
    historical_rewards.go
  xchain/
    utxos.go          # New X-Chain indexer
  cchain/
    utxos.go          # New C-Chain indexer
```

### 2. X-Chain Indexer Implementation

**Storage Schema** (same pattern as P-Chain):
```
utxo:{utxoID}           → StoredUTXO (serialized)
addr:{address}:{utxoID} → empty (index)
watermark               → height
```

**StoredUTXO Fields**:
- TxID, OutputIndex, Amount, AssetID
- Addresses, Locktime, Threshold
- BlockHeight, BlockTimestamp
- ConsumingTxID, ConsumingBlockHeight
- CreatedOnChainID (for imports)
- UTXOType ("TRANSFER" only, no STAKEABLE_LOCK)

**ProcessBatch Logic**:
```
for each block:
  for each tx:
    - Mark consumed: tx.Ins (local UTXOs)
    - Index outputs: tx.Outs

    if ImportTx:
      - Mark consumed: tx.ImportedIns (from P/C shared memory)
      - Create synthetic UTXOs with CreatedOnChainID = sourceChain

    if ExportTx:
      - Index: tx.ExportedOuts (mark as exported, will be consumed on dest chain)

    if CreateAssetTx:
      - Index initial outputs in tx.States
      - Store asset metadata (name, symbol, denomination)
```

### 3. Data Sources (Two Eras)

**Important**: X-Chain has two distinct eras with different data sources:

1. **Pre-Cortina (before April 2023)**: DAG-based vertices
   - Use Index API: `index.getContainerRange` on `/ext/index/X/tx`
   - Requires node with `--index-enabled=true`
   - Fuji: ~508,607 transactions
   - Stored as `tx:{index}` in pebble

2. **Post-Cortina (April 2023+)**: Linear blocks
   - Use block RPC: `avm.getBlockByHeight`
   - Stored as `blk:{height}` in pebble

**Key insight**: Index API only contains pre-Cortina txs. Post-Cortina txs are only in blocks. No overlap.

### 4. Transaction Parsing

Use `avalanchego/vms/avm/block` and `avalanchego/vms/avm/txs`:
```go
import (
    "github.com/ava-labs/avalanchego/vms/avm/block"
    "github.com/ava-labs/avalanchego/vms/avm/txs"
)

// Post-Cortina: parse from block
blk, _ := block.Parse(block.Codec, blockBytes)
for _, tx := range blk.Txs() {
    processTx(tx)
}

// Pre-Cortina: parse raw tx bytes directly
tx, _ := parser.ParseTx(txBytes)
processTx(tx)

func processTx(tx *txs.Tx) {
    switch utx := tx.Unsigned.(type) {
    case *txs.BaseTx:
    case *txs.ImportTx:
    case *txs.ExportTx:
    case *txs.CreateAssetTx:
    case *txs.OperationTx:
    }
}
```

### 5. Asset Metadata
X-Chain has multiple assets (ANTs). Need to track:
- AssetID → Name, Symbol, Denomination
- Fetch via RPC `avm.getAssetDescription` on first encounter
- Cache in DB: `asset:{assetID}` → metadata

## Progress
- [ ] Restructure apis/ directory
- [ ] Create shared types in apis/shared.go
- [ ] Move P-Chain code to apis/pchain/
- [ ] Implement X-Chain block parsing
- [ ] Implement X-Chain UTXO indexer
- [ ] Implement /balances endpoint
- [ ] Implement /utxos endpoint
- [ ] Handle multi-asset balances
- [ ] Test against Glacier API

## Questions Resolved
- X-Chain uses codec version 0 (different from P-Chain)
- No stakeable locks on X-Chain
- ImportTx.SourceChain identifies where UTXOs came from
- ExportTx.DestinationChain identifies where UTXOs are going
