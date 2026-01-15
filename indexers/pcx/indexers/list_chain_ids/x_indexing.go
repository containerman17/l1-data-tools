package list_chain_ids

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/pchain"
	"github.com/cockroachdb/pebble/v2"
)

var (
	xParser     block.Parser
	xParserOnce sync.Once
)

func getXParser() block.Parser {
	xParserOnce.Do(func() {
		xParser, _ = block.NewParser([]fxs.Fx{
			&secp256k1fx.Fx{},
			&nftfx.Fx{},
			&propertyfx.Fx{},
		})
	})
	return xParser
}

func (c *Chains) ProcessXChainPreCortinaTxs(ctx context.Context, inputTxs []indexer.XTx) error {
	if len(inputTxs) == 0 {
		return nil
	}

	batch := c.db.NewIndexedBatch()
	defer batch.Close()

	hrp := pchain.GetHRP(c.networkID)
	xChainID := c.getXChainID()
	parser := getXParser()

	for _, xtx := range inputTxs {
		tx, err := parser.ParseTx(xtx.Bytes)
		if err != nil {
			return fmt.Errorf("parse pre-Cortina tx %d: %w", xtx.Index, err)
		}
		c.processXTx(batch, tx, xChainID, hrp)
	}

	lastIndex := inputTxs[len(inputTxs)-1].Index
	indexBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(indexBytes, lastIndex)
	batch.Set([]byte("x:preCortina:watermark"), indexBytes, nil)

	return batch.Commit(pebble.NoSync)
}

func (c *Chains) ProcessXChainBlocks(ctx context.Context, blocks []indexer.XBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	batch := c.db.NewIndexedBatch()
	defer batch.Close()

	hrp := pchain.GetHRP(c.networkID)
	xChainID := c.getXChainID()
	parser := getXParser()

	for _, blk := range blocks {
		avmBlk, err := parser.ParseBlock(blk.Bytes)
		if err != nil {
			return fmt.Errorf("parse X-Chain block %d: %w", blk.Height, err)
		}

		for _, tx := range avmBlk.Txs() {
			c.processXTx(batch, tx, xChainID, hrp)
		}
	}

	lastHeight := blocks[len(blocks)-1].Height
	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, lastHeight)
	batch.Set([]byte("x:block:watermark"), heightBytes, nil)

	return batch.Commit(pebble.NoSync)
}

func (c *Chains) processXTx(batch *pebble.Batch, tx *txs.Tx, xChainID string, hrp string) {
	unsigned := tx.Unsigned

	// Specialized handling for each tx type
	switch t := unsigned.(type) {
	case *txs.BaseTx:
		for _, out := range t.Outs {
			c.markAddressesTouched(batch, out.Out, xChainID, hrp)
		}
	case *txs.ExportTx:
		for _, out := range t.Outs {
			c.markAddressesTouched(batch, out.Out, xChainID, hrp)
		}
		destChain := t.DestinationChain.String()
		for _, out := range t.ExportedOuts {
			c.markAddressesTouched(batch, out.Out, xChainID, hrp)
			c.markAddressesTouched(batch, out.Out, destChain, hrp)
		}
	case *txs.ImportTx:
		for _, out := range t.Outs {
			c.markAddressesTouched(batch, out.Out, xChainID, hrp)
		}
		sourceChain := t.SourceChain.String()
		for _, out := range t.Outs {
			c.markAddressesTouched(batch, out.Out, sourceChain, hrp)
		}
	case *txs.CreateAssetTx:
		for _, out := range t.Outs {
			c.markAddressesTouched(batch, out.Out, xChainID, hrp)
		}
		for _, state := range t.States {
			for _, out := range state.Outs {
				c.markAddressesTouched(batch, out, xChainID, hrp)
			}
		}
	case *txs.OperationTx:
		for _, out := range t.Outs {
			c.markAddressesTouched(batch, out.Out, xChainID, hrp)
		}
		for _, op := range t.Ops {
			for _, out := range op.Op.Outs() {
				c.markAddressesTouched(batch, out, xChainID, hrp)
			}
		}
	}
}
