package list_chain_ids

import (
	"context"
	"encoding/binary"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	ptxs "github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/pchain"
	"github.com/cockroachdb/pebble/v2"
)

func (c *Chains) ProcessPChainBatch(ctx context.Context, blocks []indexer.PBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	batch := c.db.NewIndexedBatch()
	defer batch.Close()

	hrp := pchain.GetHRP(c.networkID)

	for _, blk := range blocks {
		for _, tx := range blk.Block.Txs() {
			unsigned := tx.Unsigned

			// 1. All addresses in regular outputs touched P-Chain
			for _, out := range unsigned.Outputs() {
				c.markAddressesTouched(batch, out.Out, pChainID, hrp)
			}

			// 2. Specialized handling for stake, import, export
			switch t := unsigned.(type) {
			case *ptxs.AddValidatorTx:
				for _, out := range t.StakeOuts {
					c.markAddressesTouched(batch, out.Out, pChainID, hrp)
				}
			case *ptxs.AddDelegatorTx:
				for _, out := range t.StakeOuts {
					c.markAddressesTouched(batch, out.Out, pChainID, hrp)
				}
			case *ptxs.AddPermissionlessValidatorTx:
				for _, out := range t.StakeOuts {
					c.markAddressesTouched(batch, out.Out, pChainID, hrp)
				}
			case *ptxs.AddPermissionlessDelegatorTx:
				for _, out := range t.StakeOuts {
					c.markAddressesTouched(batch, out.Out, pChainID, hrp)
				}
			case *ptxs.ExportTx:
				destChain := t.DestinationChain.String()
				for _, out := range t.ExportedOutputs {
					c.markAddressesTouched(batch, out.Out, pChainID, hrp)
					c.markAddressesTouched(batch, out.Out, destChain, hrp)
				}
			case *ptxs.ImportTx:
				sourceChain := t.SourceChain.String()
				// Outputs of ImportTx on P-Chain touched both P-Chain and source
				for _, out := range t.Outputs() {
					c.markAddressesTouched(batch, out.Out, pChainID, hrp)
					c.markAddressesTouched(batch, out.Out, sourceChain, hrp)
				}
			case *ptxs.RewardValidatorTx:
				// Reward UTXOs for completed staking
				rewards := blk.RewardUTXOs[t.TxID]
				for _, reward := range rewards {
					c.markAddressesTouched(batch, reward.Out, pChainID, hrp)
				}
			}
		}
	}

	// Update watermark
	lastHeight := blocks[len(blocks)-1].Height
	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, lastHeight)
	batch.Set([]byte("p:watermark"), heightBytes, nil)

	return batch.Commit(pebble.NoSync)
}

func (c *Chains) markAddressesTouched(batch *pebble.Batch, out any, blockchainID string, hrp string) {
	var addrs []ids.ShortID
	switch o := out.(type) {
	case *secp256k1fx.TransferOutput:
		addrs = o.Addrs
	case *nftfx.TransferOutput:
		addrs = o.Addrs
	case *nftfx.MintOutput:
		addrs = o.Addrs
	case *propertyfx.OwnedOutput:
		addrs = o.Addrs
	case *propertyfx.MintOutput:
		addrs = o.Addrs
	case *avax.TransferableOutput:
		c.markAddressesTouched(batch, o.Out, blockchainID, hrp)
		return
	default:
		return
	}

	for _, addr := range addrs {
		if s, err := address.FormatBech32(hrp, addr.Bytes()); err == nil && s != "" {
			markTouched(batch, s, blockchainID)
		}
	}
}
