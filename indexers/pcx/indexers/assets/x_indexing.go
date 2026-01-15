package assets

import (
	"context"
	"fmt"

	avmblock "github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/db"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
	ts "github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/xchain/pre_cortina_timestamps"
	"github.com/cockroachdb/pebble/v2"
)

func (a *Assets) ProcessXChainPreCortinaTxs(ctx context.Context, inputTxs []indexer.XTx) error {
	if len(inputTxs) == 0 {
		return nil
	}

	// Fix timestamps: Replace Index API bogus timestamps with real network timestamps
	archive, err := ts.GetFujiArchive()
	if err != nil {
		return fmt.Errorf("loading timestamp archive: %w", err)
	}

	parser, err := avmblock.NewParser([]fxs.Fx{
		&secp256k1fx.Fx{},
		&nftfx.Fx{},
		&propertyfx.Fx{},
	})
	if err != nil {
		return fmt.Errorf("failed to create AVM parser: %w", err)
	}

	batch := a.db.NewBatch()
	defer batch.Close()

	for _, xtx := range inputTxs {
		tx, err := parser.ParseTx(xtx.Bytes)
		if err != nil {
			continue // Skip unparseable txs
		}

		if t, ok := tx.Unsigned.(*txs.CreateAssetTx); ok {
			txID := tx.ID()
			timestamp := xtx.Timestamp
			if realTs, found := archive.Lookup(txID); found {
				timestamp = realTs
			}

			meta := &AssetMetadata{
				AssetID:            txID.String(),
				Name:               t.Name,
				Symbol:             t.Symbol,
				Denomination:       int(t.Denomination),
				CreatedAtTimestamp: timestamp,
				Type:               "secp256k1", // Glacier-compatible
				Cap:                "fixed",     // Glacier-compatible
			}
			if err := a.saveAsset(batch, meta); err != nil {
				return err
			}
		}
	}

	lastIndex := inputTxs[len(inputTxs)-1].Index
	db.SaveWatermark(batch, "x-precortina", uint64(lastIndex))

	return batch.Commit(pebble.NoSync)
}

func (a *Assets) ProcessXChainBlocks(ctx context.Context, blocks []indexer.XBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	parser, err := avmblock.NewParser([]fxs.Fx{
		&secp256k1fx.Fx{},
		&nftfx.Fx{},
		&propertyfx.Fx{},
	})
	if err != nil {
		return fmt.Errorf("failed to create AVM parser: %w", err)
	}

	batch := a.db.NewBatch()
	defer batch.Close()

	for _, b := range blocks {
		blk, err := parser.ParseBlock(b.Bytes)
		if err != nil {
			continue
		}

		timestamp := blk.Timestamp().Unix()

		for _, tx := range blk.Txs() {
			if t, ok := tx.Unsigned.(*txs.CreateAssetTx); ok {
				meta := &AssetMetadata{
					AssetID:            tx.ID().String(),
					Name:               t.Name,
					Symbol:             t.Symbol,
					Denomination:       int(t.Denomination),
					CreatedAtTimestamp: timestamp,
					Type:               "secp256k1",
					Cap:                "fixed",
				}
				if err := a.saveAsset(batch, meta); err != nil {
					return err
				}
			}
		}
	}

	lastHeight := blocks[len(blocks)-1].Height
	db.SaveWatermark(batch, "x-blocks", lastHeight)

	return batch.Commit(pebble.NoSync)
}
