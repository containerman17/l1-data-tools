# Cross-Chain UTXO Bytes Implementation

## Problem

Cross-chain UTXOs (imported from C-Chain or X-Chain to P-Chain) are missing:
- `utxoBytes` - raw serialized UTXO data
- `blockNumber` - creation block (should be source chain block, not P-Chain import block)
- `blockTimestamp` - creation timestamp (source chain time)
- `createdOnChainId` - correctly set to source chain

Currently, when P-Chain ImportTx runs, we only have:
- UTXO ID (from input reference)
- Consumption metadata (P-Chain block where it was imported)

We don't have the original `TransferableOutput` from the source chain.

## Solution Overview

1. **Update C-Chain Fetcher** to store more block data (not just `blockExtraData`)
2. **Update C-Chain Runner** to pass richer block data to indexers
3. **Implement UTXO indexing** from C-Chain atomic transactions

---

## Step 1: C-Chain Fetcher Storage Format

### Current: Only stores `blockExtraData`

```
blk:{height} → []byte (blockExtraData only)
```

### New: Store structured block data

We need these fields for:
- UTXO indexing: `timestamp`, `blockExtraData`
- Future `/blocks` API: `blockHash`, `parentHash`, `txCount`, `blockSize`

**Target API format (from Glacier):**
```json
{
  "blockNumber": "123",
  "blockHash": "0x4d58d76...",
  "parentHash": "0x7585dd9...",
  "blockTimestamp": 1601578829,
  "blockType": "Standard",
  "txCount": 0,
  "blockSizeBytes": 777
}
```

### New Storage Format

Store as JSON for flexibility and future `/blocks` API compatibility:
```go
type BlockData struct {
    // Core fields
    Height     uint64 `json:"h"`
    Timestamp  int64  `json:"ts"`
    Hash       string `json:"hash"`
    ParentHash string `json:"parent"`

    // Size and counts
    Size    int `json:"size"`
    TxCount int `json:"txc"`

    // Gas fields (post-EIP-1559)
    GasLimit      uint64 `json:"gasLimit,omitempty"`
    GasUsed       uint64 `json:"gasUsed,omitempty"`
    BaseFeePerGas uint64 `json:"baseFee,omitempty"`

    // Miner/Coinbase address
    Miner string `json:"miner,omitempty"`

    // Atomic transaction data
    ExtraData   []byte `json:"extra,omitempty"`     // Only if non-empty
    ExtDataHash string `json:"extraHash,omitempty"`
}
```

### Glacier API Field Mapping

| Glacier Field | Our Storage | Notes |
|---------------|-------------|-------|
| `blockNumber` | `Height` | ✅ |
| `blockHash` | `Hash` | ✅ |
| `parentHash` | `ParentHash` | ✅ |
| `blockTimestamp` | `Timestamp` | ✅ |
| `blockSizeBytes` | `Size` | ✅ |
| `txCount` | `TxCount` | ✅ |
| `blockType` | - | Compute from block characteristics |
| `activeL1Validators` | - | Requires separate query |
| `l1ValidatorsAccruedFees` | - | Requires separate query |
| `proposerDetails` | - | Decode from Snowman++ header |

**Note**: `proposerDetails` requires decoding the Snowman++ proposer certificate from the block header, which is not directly available from `eth_getBlockByNumber`. This would need to be fetched separately or decoded from the raw block bytes.

---

## Step 3: UTXO Indexing from C-Chain

### C-Chain Atomic Transaction Location

`blockExtraData` contains serialized atomic transactions:
- `ExportTx` - exports AVAX from C-Chain to P-Chain or X-Chain
- `ImportTx` - imports AVAX to C-Chain from P-Chain or X-Chain

### ExportTx to P-Chain Flow

1. **C-Chain ExportTx** creates UTXOs in shared memory
   - Outputs are `TransferableOutput` structs
   - We can generate `utxoBytes` from these outputs

2. **P-Chain ImportTx** consumes these UTXOs
   - With upsert, we merge into existing record

### Processing Logic

```go
func (u *UTXOs) ProcessCChainBatch(ctx context.Context, blocks []indexer.CBlock) error {
    for _, blk := range blocks {
        if len(blk.ExtraData) == 0 {
            continue // No atomic txs
        }
        
        // Parse atomic transactions
        isAP5 := blk.Timestamp >= ap5Timestamp
        atomicTxs, err := atomic.ExtractAtomicTxs(blk.ExtraData, isAP5, atomic.Codec)
        
        for _, tx := range atomicTxs {
            switch utx := tx.UnsignedAtomicTx.(type) {
            case *atomic.UnsignedExportTx:
                // Only care about exports TO P-Chain
                if utx.DestinationChain == constants.PlatformChainID {
                    u.processCChainExportTx(utx, blk)
                }
            case *atomic.UnsignedImportTx:
                // Mark UTXOs as consumed on C-Chain
                u.processCChainImportTx(utx, blk)
            }
        }
    }
}
```

### Order Independence (Upsert)

The upsert design means indexing order doesn't matter:

**Scenario A: P-Chain first**
1. P-Chain ImportTx → creates partial record (consumption data only)
2. C-Chain ExportTx → upsert fills in creation data

**Scenario B: C-Chain first**
1. C-Chain ExportTx → creates record with creation data (unspent)
2. P-Chain ImportTx → upsert adds consumption data

Both scenarios result in identical complete records.

---

## Implementation Checklist

### Phase 1: C-Chain Fetcher Update
- [x] Update `cchain/client.go` - extract more fields from RPC response
- [x] Update `cchain/fetcher.go` - store structured block data as JSON
- [x] Update `indexer/api.go` - add fields to `CBlock` struct
- [x] Update `runner/c_runner.go` - decode JSON and pass to indexers

### Phase 2: UTXO Indexing
- [x] Add atomic tx parsing in `indexers/utxos/atomic.go`
- [x] Implement `processCChainAtomicTx` - handles both ExportTx and ImportTx
- [ ] Test against Glacier API (requires re-indexing C-Chain with new format)

### Phase 3: Future `/blocks` API
- [ ] Implement C-Chain blocks endpoint using stored data

---

## Important: Re-indexing Required

The C-Chain fetcher now stores a **new JSON format** with block metadata.
Old blocks stored with just `blockExtraData` bytes won't have timestamps.

To fully test cross-chain UTXOs:
1. Delete C-Chain blocks DB: `rm -rf data/{networkID}/blocks/c`
2. Delete UTXO index: `rm -rf data/{networkID}/utxos`
3. Restart server to re-fetch and re-index

---

## ApricotPhase5 Timestamps

Pre-AP5: Single atomic tx per block (unmarshal as `*Tx`)
Post-AP5: Batch of atomic txs (unmarshal as `[]*Tx`)

| Network | AP5 Timestamp |
|---------|---------------|
| Mainnet | 1638468000 (Dec 2, 2021 18:00 UTC) |
| Fuji    | 1637766000 (Nov 24, 2021 15:00 UTC) |

---

## Testing

### Current Status

After implementation, native P-Chain UTXOs all pass. The single remaining difference is the cross-chain UTXO:

```diff
-      blockNumber: "48746327"         # C-Chain export block (Glacier)
-      blockTimestamp: 1.765267096e+09 # C-Chain timestamp
+      blockNumber: "250286"           # P-Chain import block (local - fallback)
+      blockTimestamp: 1.765267108e+09 # P-Chain timestamp
-      utxoBytes: 0x0000937de3...      # Generated from C-Chain export
+      # utxoBytes missing - no C-Chain data yet
```

### To Fully Test

1. Delete old C-Chain blocks (stored without metadata):
   ```bash
   rm -rf data/5/blocks/c
   ```

2. Delete UTXO index to re-process:
   ```bash
   rm -rf data/5/utxos
   ```

3. Run server and wait for C-Chain to sync to block 48746327+ (the export block)

4. Re-run tests - cross-chain UTXO should now match:
   - `blockNumber`: from C-Chain export block
   - `blockTimestamp`: from C-Chain export block  
   - `utxoBytes`: generated from C-Chain ExportTx output

### Notes

- C-Chain on Fuji has ~50M+ blocks, so full sync takes time
- The test only needs to reach block 48746327 for this specific UTXO
- Order independence means P-Chain can index first, C-Chain fills in creation data via upsert

