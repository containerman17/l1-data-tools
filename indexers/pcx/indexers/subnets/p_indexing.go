package subnets

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/db"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/pchain"
)

func (s *Subnets) ProcessPChainBatch(ctx context.Context, blocks []indexer.PBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	hrp := pchain.GetHRP(s.networkID)

	// In-memory cache for subnets modified in this batch
	pendingSubnets := make(map[string]*SubnetMetadata)

	// Helper to get subnet from cache or DB
	getSubnetForUpdate := func(subnetID string) (*SubnetMetadata, error) {
		if meta, ok := pendingSubnets[subnetID]; ok {
			return meta, nil
		}
		return s.getSubnet(subnetID, false)
	}

	for _, blk := range blocks {
		for _, tx := range blk.Block.Txs() {
			switch unsignedTx := tx.Unsigned.(type) {
			case *txs.CreateSubnetTx:
				meta := &SubnetMetadata{
					SubnetID:             tx.ID().String(),
					CreateBlockTimestamp: blk.Timestamp,
					CreateBlockIndex:     fmt.Sprintf("%d", blk.Height),
					IsL1:                 false,
					Blockchains:          []*SubnetBlockchain{},
				}

				if owners, ok := unsignedTx.Owner.(*secp256k1fx.OutputOwners); ok {
					meta.Threshold = owners.Threshold
					meta.Locktime = owners.Locktime
					meta.OwnerAddresses = make([]string, len(owners.Addrs))
					for i, addr := range owners.Addrs {
						fAddr, err := address.Format("P", hrp, addr.Bytes())
						if err != nil {
							fAddr = addr.String()
						}
						meta.OwnerAddresses[i] = fAddr
					}
					meta.SubnetOwnershipInfo = &OwnershipInfo{
						Threshold: owners.Threshold,
						Addresses: meta.OwnerAddresses,
						Locktime:  owners.Locktime,
					}
				}

				pendingSubnets[meta.SubnetID] = meta
				if err := s.saveSubnet(batch, meta, blk.Height); err != nil {
					return err
				}

			case *txs.TransferSubnetOwnershipTx:
				meta, err := getSubnetForUpdate(unsignedTx.Subnet.String())
				if err != nil {
					continue
				}

				// Only update SubnetOwnershipInfo, keep original ownerAddresses/threshold from creation
				if owners, ok := unsignedTx.Owner.(*secp256k1fx.OutputOwners); ok {
					newAddrs := make([]string, len(owners.Addrs))
					for i, addr := range owners.Addrs {
						fAddr, err := address.Format("P", hrp, addr.Bytes())
						if err != nil {
							fAddr = addr.String()
						}
						newAddrs[i] = fAddr
					}
					meta.SubnetOwnershipInfo = &OwnershipInfo{
						Threshold: owners.Threshold,
						Addresses: newAddrs,
						Locktime:  owners.Locktime,
					}
				}

				pendingSubnets[meta.SubnetID] = meta
				if err := s.saveSubnet(batch, meta, 0); err != nil {
					return err
				}

			case *txs.ConvertSubnetToL1Tx:
				meta, err := getSubnetForUpdate(unsignedTx.Subnet.String())
				if err != nil {
					log.Printf("[subnets] WARN: ConvertSubnetToL1Tx for unknown subnet %s at block %d", unsignedTx.Subnet.String(), blk.Height)
					continue
				}

				meta.IsL1 = true
				meta.L1ConversionTransactionHash = tx.ID().String()
				meta.L1ValidatorManagerDetails = &L1ValidatorManagerDetails{
					BlockchainID:    unsignedTx.ChainID.String(),
					ContractAddress: fmt.Sprintf("0x%x", unsignedTx.Address),
				}

				pendingSubnets[meta.SubnetID] = meta
				if err := s.saveSubnet(batch, meta, 0); err != nil {
					return err
				}

			case *txs.CreateChainTx:
				sbc := &SubnetBlockchain{
					BlockchainID:         tx.ID().String(),
					BlockchainName:       unsignedTx.ChainName,
					CreateBlockNumber:    fmt.Sprintf("%d", blk.Height),
					CreateBlockTimestamp: blk.Timestamp,
					SubnetID:             unsignedTx.SubnetID.String(),
					VMID:                 unsignedTx.VMID.String(),
				}

				var genesis struct {
					Config struct {
						ChainID int `json:"chainId"`
					} `json:"config"`
				}
				if err := json.Unmarshal(unsignedTx.GenesisData, &genesis); err == nil && genesis.Config.ChainID != 0 {
					sbc.EVMChainID = &genesis.Config.ChainID
				}

				if err := s.saveSubnetBlockchain(batch, sbc); err != nil {
					return err
				}
			}
		}
	}

	lastHeight := blocks[len(blocks)-1].Height
	db.SaveWatermark(batch, "p-blocks", lastHeight)

	return batch.Commit(nil)
}
