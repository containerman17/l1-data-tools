# C-Chain UTXO Indexing

## Goal
Index C-Chain atomic UTXOs (imports/exports only) to provide `/balances` and `/utxos` endpoints.

**Note**: C-Chain is EVM-based. We only index atomic transactions (cross-chain), NOT EVM state.

## Target API

```
GET /v1/networks/{network}/blockchains/c-chain/balances?addresses=...
GET /v1/networks/{network}/blockchains/c-chain/utxos?addresses=...&includeSpent=true
```

### Balance Response Structure
```json
{
  "balances": {
    "atomicMemoryUnlocked": [{ "assetId": "...", "amount": "..." }],
    "atomicMemoryLocked": []
  },
  "chainInfo": { "chainName": "c-chain", "network": "fuji" }
}
```

**Note**: C-Chain balances only show atomic memory (pending imports). On-chain EVM balance requires `eth_getBalance` which is out of scope.

### UTXO Response Structure
```json
{
  "utxos": [{
    "addresses": ["fuji1..."],
    "utxoId": "...",
    "asset": { "assetId": "...", "name": "AVAX", "symbol": "AVAX", "amount": "..." },
    "utxoType": "transfer",
    "creationTxHash": "...",
    "consumingTxHash": "...",
    "createdOnChainId": "...",
    "consumedOnChainId": "..."
  }],
  "nextPageToken": "...",
  "chainInfo": { "chainName": "c-chain", "network": "fuji" }
}
```

## C-Chain Atomic Characteristics

### What We Have
We're already storing `blockExtraData` for each C-Chain block. This contains:
- Pre-ApricotPhase5: Single atomic tx
- Post-ApricotPhase5: Batch of atomic txs

### Transaction Types (Atomic Only)
- **ImportTx** - Import from P/X-Chain
  - ImportedInputs: UTXOs from shared memory (consumed)
  - Outs: EVMOutputs (addresses credited on EVM side)
- **ExportTx** - Export to P/X-Chain
  - Ins: EVMInputs (addresses debited on EVM side)
  - ExportedOutputs: UTXOs created for destination chain

### Key Structures
```go
// ImportTx
type UnsignedImportTx struct {
    SourceChain    ids.ID                    // P or X-Chain
    ImportedInputs []*avax.TransferableInput // UTXOs consumed
    Outs           []EVMOutput               // EVM addresses credited
}

// ExportTx
type UnsignedExportTx struct {
    DestinationChain ids.ID                     // P or X-Chain
    Ins              []EVMInput                 // EVM addresses debited
    ExportedOutputs  []*avax.TransferableOutput // UTXOs created
}

// EVMOutput (import destination)
type EVMOutput struct {
    Address common.Address  // 0x...
    Amount  uint64          // nAVAX (10^-9)
    AssetID ids.ID
}

// EVMInput (export source)
type EVMInput struct {
    Address common.Address
    Amount  uint64
    AssetID ids.ID
    Nonce   uint64
}
```

### Balance Categories
- **atomicMemoryUnlocked** - Exported UTXOs waiting to be imported on P/X
- **atomicMemoryLocked** - Exported but time-locked
- No `unlocked`/`locked` - those are EVM balance (out of scope)

### ApricotPhase5 Detection
- Mainnet AP5: Block timestamp >= 1639162800 (Dec 10, 2021)
- Fuji AP5: Different timestamp (need to check)

## Plan

### 1. Parse blockExtraData

```go
import "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"

// Determine format
isAP5 := blockTimestamp >= AP5Timestamp

// Parse
atomicTxs, err := atomic.ExtractAtomicTxs(blockExtraData, isAP5, atomic.Codec)

for _, tx := range atomicTxs {
    switch utx := tx.UnsignedAtomicTx.(type) {
    case *atomic.UnsignedImportTx:
        // Import from P/X → C
    case *atomic.UnsignedExportTx:
        // Export from C → P/X
    }
}
```

### 2. UTXO Tracking Logic

**For ImportTx (P/X → C)**:
- UTXOs in `ImportedInputs` were created on source chain
- Mark as consumed (consuming chain = C-Chain)
- Track: created on P/X, consumed on C

**For ExportTx (C → P/X)**:
- UTXOs in `ExportedOutputs` are created for destination chain
- Track: created on C, will be consumed on P/X
- These show up as `atomicMemoryUnlocked` on C-Chain until imported

### 3. Address Format

C-Chain atomic uses Bech32 addresses (same as P/X), NOT 0x addresses:
- Query param: `fuji1...` or `avax1...`
- EVMOutput.Address is 0x but we need to map to Bech32 for UTXO tracking

**Challenge**: How to link Bech32 address to 0x address?
- User provides Bech32 address
- ImportTx credits 0x address
- Need mapping or accept both formats

### 4. Storage Schema

```
utxo:{utxoID}           → StoredUTXO
addr:{address}:{utxoID} → empty (index by Bech32 address)
watermark               → height
```

**StoredUTXO for C-Chain**:
- TxID, OutputIndex, Amount, AssetID
- Addresses (Bech32 for source chain, or mapped from 0x)
- BlockHeight, BlockTimestamp
- ConsumingTxID, ConsumingBlockHeight
- CreatedOnChainID, ConsumedOnChainID
- Direction: "IMPORT" or "EXPORT"

### 5. Implementation Steps

**Phase 1: Parse and Index**
- [ ] Add coreth dependency (or copy codec)
- [ ] Parse blockExtraData from stored blocks
- [ ] Extract ImportTx/ExportTx
- [ ] Index UTXOs with chain IDs

**Phase 2: API Endpoints**
- [ ] /utxos endpoint (list atomic UTXOs)
- [ ] /balances endpoint (aggregate atomic memory)
- [ ] Handle address format (Bech32)

## Progress
- [x] C-chain fetcher stores blockExtraData
- [ ] Parse blockExtraData into atomic txs
- [ ] Implement UTXO indexer
- [ ] Implement /utxos endpoint
- [ ] Implement /balances endpoint
- [ ] Test against Glacier API

## Open Questions

1. **Address mapping**: How to link Bech32 (fuji1...) to 0x addresses for imports?
   - Option A: Store both formats in index
   - Option B: Derive Bech32 from secp256k1 pubkey in tx signatures
   - Option C: Only track ExportTx outputs (have Bech32 addresses)

2. **Coreth dependency**:
   - Full coreth import is heavy
   - Alternative: Copy just the codec/tx parsing code

3. **AP5 timestamp per network**:
   - Need to look up exact activation block/timestamp for Fuji vs Mainnet

## Decisions
- Focus on ExportTx outputs first (have Bech32 addresses natively)
- ImportTx consumes UTXOs from P/X (those are already tracked there)
- C-Chain atomic indexer is simpler than X-Chain (only 2 tx types)
