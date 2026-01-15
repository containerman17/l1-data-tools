package blockchains

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/db"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
)

func (b *Blockchains) ProcessPChainBatch(ctx context.Context, blocks []indexer.PBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	batch := b.db.NewBatch()
	defer batch.Close()

	for _, blk := range blocks {
		for _, tx := range blk.Block.Txs() {
			if createChainTx, ok := tx.Unsigned.(*txs.CreateChainTx); ok {
				meta := &BlockchainMetadata{
					CreateBlockTimestamp: blk.Timestamp,
					CreateBlockNumber:    fmt.Sprintf("%d", blk.Height),
					BlockchainID:         tx.ID().String(),
					VMID:                 createChainTx.VMID.String(),
					SubnetID:             createChainTx.SubnetID.String(),
					BlockchainName:       createChainTx.ChainName,
					GenesisData:          parseGenesisData(createChainTx.GenesisData),
				}

				// Try to extract EVM Chain ID from genesis data
				// Subnet-EVM genesis format: {"config": {"chainId": ...}}
				var genesis struct {
					Config struct {
						ChainID int `json:"chainId"`
					} `json:"config"`
				}
				if err := json.Unmarshal(createChainTx.GenesisData, &genesis); err == nil && genesis.Config.ChainID != 0 {
					meta.EVMChainID = &genesis.Config.ChainID
				}

				if err := b.saveBlockchain(batch, meta, blk.Height); err != nil {
					return err
				}
			}
		}
	}

	lastHeight := blocks[len(blocks)-1].Height
	db.SaveWatermark(batch, "p-blocks", lastHeight)

	return batch.Commit(nil)
}
