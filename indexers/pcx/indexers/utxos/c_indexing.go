package utxos

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
	"github.com/cockroachdb/pebble/v2"
)

// ApricotPhase5 timestamps (batch atomic txs)
const (
	ap5MainnetTimestamp int64 = 1638468000 // Dec 2, 2021 18:00 UTC
	ap5FujiTimestamp    int64 = 1637766000 // Nov 24, 2021 15:00 UTC
)

// ProcessCChainBatch indexes UTXOs from C-chain blocks.
func (u *UTXOs) ProcessCChainBatch(ctx context.Context, blocks []indexer.CBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	// Fast path: if no atomic txs in entire batch, just update watermark
	hasAtomicTxs := false
	for _, blk := range blocks {
		if len(blk.ExtraData) > 0 {
			hasAtomicTxs = true
			break
		}
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	batch := u.db.NewIndexedBatch()
	defer batch.Close()

	if hasAtomicTxs {
		// Determine C-Chain ID and AP5 timestamp based on network
		cChainID := cChainIDFuji
		ap5Timestamp := ap5FujiTimestamp
		if u.networkID == 1 {
			cChainID = cChainIDMainnet
			ap5Timestamp = ap5MainnetTimestamp
		}

		for _, blk := range blocks {
			if len(blk.ExtraData) == 0 {
				continue // No atomic txs in this block
			}

			// Parse atomic transactions
			isAP5 := blk.Timestamp >= ap5Timestamp
			atomicTxs, err := extractAtomicTxs(blk.ExtraData, isAP5)
			if err != nil {
				return fmt.Errorf("parse atomic txs at C-Chain block %d: %w", blk.Height, err)
			}

			for _, tx := range atomicTxs {
				u.processCChainAtomicTx(batch, tx, blk, cChainID)
			}
		}
	}

	// Update watermark
	lastHeight := blocks[len(blocks)-1].Height
	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, lastHeight)
	batch.Set([]byte("c:watermark"), heightBytes, nil)

	return batch.Commit(pebble.NoSync)
}

// processCChainAtomicTx handles a single C-Chain atomic transaction.
func (u *UTXOs) processCChainAtomicTx(batch *pebble.Batch, tx *atomicTx, blk indexer.CBlock, cChainID string) {
	txID := tx.ID()

	switch utx := tx.UnsignedAtomicTx.(type) {
	case *unsignedExportTx:
		// ExportTx: C-Chain creates UTXOs for destination chain (P or X)
		// Only index exports to P-Chain for now
		destChainID := utx.DestinationChain.String()
		if destChainID != pChainID {
			return
		}

		// Extract credentials from the transaction (applies to all outputs)
		creds := extractCChainCredentials(tx)

		for i, out := range utx.ExportedOutputs {
			outputIdx := uint32(i)
			utxoid := avax.UTXOID{TxID: txID, OutputIndex: outputIdx}
			utxoID := utxoid.InputID().String()

			utxo := u.buildUTXO(out.Out, txID, outputIdx, out.AssetID(), blk.Height, blk.Timestamp, false, 0, 0, cChainID, destChainID)
			if utxo == nil {
				continue
			}

			// Generate utxoBytes using the atomic codec
			utxoBytes := u.generateCChainUTXOBytes(&utxoid, out)

			// Creation data for C-Chain storage (includes credentials)
			cChainUpdates := map[string]any{
				"txHash":            utxo.TxHash,
				"outputIndex":       utxo.OutputIndex,
				"amount":            utxo.Amount,
				"assetId":           utxo.AssetID,
				"addresses":         utxo.Addresses,
				"threshold":         utxo.Threshold,
				"utxoType":          utxo.UTXOType,
				"blockNumber":       utxo.BlockNumber,
				"blockTimestamp":    utxo.BlockTimestamp,
				"createdOnChainId":  utxo.CreatedOnChainID,
				"consumedOnChainId": destChainID,
				"utxoBytes":         utxoBytes,
			}
			if len(creds) > 0 {
				cChainUpdates["credentials"] = creds
			}

			// P-Chain storage (no credentials - P-Chain API doesn't include them)
			pChainUpdates := map[string]any{
				"txHash":            utxo.TxHash,
				"outputIndex":       utxo.OutputIndex,
				"amount":            utxo.Amount,
				"assetId":           utxo.AssetID,
				"addresses":         utxo.Addresses,
				"threshold":         utxo.Threshold,
				"utxoType":          utxo.UTXOType,
				"blockNumber":       utxo.BlockNumber,
				"blockTimestamp":    utxo.BlockTimestamp,
				"createdOnChainId":  utxo.CreatedOnChainID,
				"consumedOnChainId": destChainID,
				"utxoBytes":         utxoBytes,
			}

			// Write to C-Chain storage (source chain)
			stored, _ := u.upsertCChainUTXO(batch, utxoID, cChainUpdates)

			// Also write to P-Chain storage (destination chain) for cross-chain visibility
			u.upsertPChainUTXO(batch, utxoID, pChainUpdates)

			// Store address index in BOTH chains
			if stored != nil {
				for _, addr := range stored.Addresses {
					batch.Set([]byte(prefixCChainAddr+addr+":"+utxoID), nil, nil)
					batch.Set([]byte(prefixPChainAddr+addr+":"+utxoID), nil, nil)
				}
			}
		}

	case *unsignedImportTx:
		// ImportTx: C-Chain consumes UTXOs from source chain (P or X)
		sourceChainID := utx.SourceChain.String()

		for _, in := range utx.ImportedInputs {
			utxoID := in.UTXOID.InputID().String()

			// Mark spent on C-Chain spend index
			markSpent(batch, "c:", utxoID, &SpendInfo{
				ConsumingTxHash:   txID.String(),
				ConsumingTime:     blk.Timestamp,
				ConsumedOnChainID: cChainID,
				CreatedOnChainID:  sourceChainID,
			})

			// Also mark spent on source chain
			if sourceChainID == pChainID {
				markSpent(batch, "p:", utxoID, &SpendInfo{
					ConsumingTxHash: txID.String(),
					ConsumingTime:   blk.Timestamp,
				})
			} else if sourceChainID == xChainIDFuji || sourceChainID == xChainIDMainnet {
				markSpent(batch, "x:", utxoID, &SpendInfo{
					ConsumingTxHash: txID.String(),
					ConsumingTime:   blk.Timestamp,
				})
			}
		}
	}
}

// generateCChainUTXOBytes creates the hex-encoded UTXO bytes for C-Chain exports.
func (u *UTXOs) generateCChainUTXOBytes(utxoid *avax.UTXOID, out *avax.TransferableOutput) string {
	utxo := avax.UTXO{
		UTXOID: *utxoid,
		Asset:  avax.Asset{ID: out.AssetID()},
		Out:    out.Out,
	}

	// Use atomic codec (same as platform codec for UTXOs)
	bytes, err := atomicCodec.Marshal(atomicCodecVersion, &utxo)
	if err != nil {
		return ""
	}
	return "0x" + hex.EncodeToString(bytes)
}
