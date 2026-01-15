package validators

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/db"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/pchain"
	"github.com/cockroachdb/pebble/v2"
)

const primaryNetworkID = "11111111111111111111111111111111LpoYY"

func (v *Validators) ProcessPChainBatch(ctx context.Context, blocks []indexer.PBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	batch := v.db.NewBatch()
	defer batch.Close()

	hrp := pchain.GetHRP(v.networkID)

	// In-memory cache for records modified in this batch
	pending := &pendingValidators{
		validators: make(map[string]*ValidatorRecord),
		delegators: make(map[string]*DelegatorRecord),
	}

	// Helper to get validator from cache or DB
	getValidator := func(txHash string) (*ValidatorRecord, error) {
		if rec, ok := pending.getValidator(txHash); ok {
			return rec, nil
		}
		return v.getValidatorFromBatch(batch, txHash)
	}

	// Helper to get delegator from cache or DB
	getDelegator := func(txHash string) (*DelegatorRecord, error) {
		if rec, ok := pending.getDelegator(txHash); ok {
			return rec, nil
		}
		return v.getDelegatorFromBatch(batch, txHash)
	}

	for _, blk := range blocks {
		for _, tx := range blk.Block.Txs() {
			switch utx := tx.Unsigned.(type) {

			case *txs.AddValidatorTx:
				// Primary network validator (legacy)
				rec := &ValidatorRecord{
					TxHash:         tx.ID().String(),
					NodeID:         utx.NodeID().String(),
					SubnetID:       primaryNetworkID,
					AmountStaked:   fmt.Sprintf("%d", utx.Weight()),
					StartTimestamp: int64(utx.Start),
					EndTimestamp:   int64(utx.End),
					RewardAddrs:    extractAddrs(utx.RewardsOwner, hrp),
					BlockHeight:    blk.Height,
				}

				// Delegation fee: shares/10000 as percentage (e.g., 20000 = 2%)
				if utx.DelegationShares > 0 {
					fee := float64(utx.DelegationShares) / 10000.0
					rec.DelegationFee = fmt.Sprintf("%.16f", fee)
				}

				pending.setValidator(rec)
				if err := v.saveValidator(batch, rec); err != nil {
					return err
				}

			case *txs.AddPermissionlessValidatorTx:
				// Post-Durango permissionless validator
				rec := &ValidatorRecord{
					TxHash:         tx.ID().String(),
					NodeID:         utx.NodeID().String(),
					SubnetID:       utx.Subnet.String(),
					AmountStaked:   fmt.Sprintf("%d", utx.Weight()),
					StartTimestamp: int64(utx.Start),
					EndTimestamp:   int64(utx.End),
					RewardAddrs:    extractAddrs(utx.ValidatorRewardsOwner, hrp),
					BlockHeight:    blk.Height,
				}

				// Delegation fee
				if utx.DelegationShares > 0 {
					fee := float64(utx.DelegationShares) / 10000.0
					rec.DelegationFee = fmt.Sprintf("%.16f", fee)
				}

				// BLS credentials (primary network only)
				if utx.Subnet == ids.Empty {
					rec.SubnetID = primaryNetworkID
					if pop, ok := utx.Signer.(*signer.ProofOfPossession); ok {
						rec.BlsCredentials = &BlsCredentials{
							PublicKey:         fmt.Sprintf("0x%x", pop.PublicKey[:]),
							ProofOfPossession: fmt.Sprintf("0x%x", pop.ProofOfPossession[:]),
						}
					}
				}

				pending.setValidator(rec)
				if err := v.saveValidator(batch, rec); err != nil {
					return err
				}

			case *txs.AddSubnetValidatorTx:
				// Permissioned subnet validator
				rec := &ValidatorRecord{
					TxHash:         tx.ID().String(),
					NodeID:         utx.NodeID().String(),
					SubnetID:       utx.Subnet.String(),
					AmountStaked:   fmt.Sprintf("%d", utx.Weight()),
					StartTimestamp: int64(utx.Start),
					EndTimestamp:   int64(utx.End),
					BlockHeight:    blk.Height,
				}

				pending.setValidator(rec)
				if err := v.saveValidator(batch, rec); err != nil {
					return err
				}

			case *txs.AddDelegatorTx:
				// Primary network delegator (legacy)
				delRec := &DelegatorRecord{
					TxHash:          tx.ID().String(),
					NodeID:          utx.NodeID().String(),
					SubnetID:        primaryNetworkID,
					AmountDelegated: fmt.Sprintf("%d", utx.Weight()),
					StartTimestamp:  int64(utx.Start),
					EndTimestamp:    int64(utx.End),
					RewardAddrs:     extractAddrs(utx.DelegationRewardsOwner, hrp),
					BlockHeight:     blk.Height,
				}

				// Find the validator this delegation is for
				// Look for validator with matching nodeId and time range
				valRec, err := v.findValidatorByNodeAndTime(utx.NodeID().String(), int64(utx.Start), primaryNetworkID)
				if err == nil {
					pending.setDelegator(delRec)
					if err := v.saveDelegator(batch, delRec, valRec.TxHash); err != nil {
						return err
					}
					if err := v.saveStakingToValidatorMapping(batch, tx.ID().String(), valRec.TxHash); err != nil {
						return err
					}
				} else {
					// If validator not found, still save delegator but without validator link
					pending.setDelegator(delRec)
					if err := v.saveDelegator(batch, delRec, ""); err != nil {
						return err
					}
				}

			case *txs.AddPermissionlessDelegatorTx:
				// Post-Durango delegator
				subnetID := utx.Subnet.String()
				if utx.Subnet == ids.Empty {
					subnetID = primaryNetworkID
				}

				delRec := &DelegatorRecord{
					TxHash:          tx.ID().String(),
					NodeID:          utx.NodeID().String(),
					SubnetID:        subnetID,
					AmountDelegated: fmt.Sprintf("%d", utx.Weight()),
					StartTimestamp:  int64(utx.Start),
					EndTimestamp:    int64(utx.End),
					RewardAddrs:     extractAddrs(utx.DelegationRewardsOwner, hrp),
					BlockHeight:     blk.Height,
				}

				valRec, err := v.findValidatorByNodeAndTime(utx.NodeID().String(), int64(utx.Start), subnetID)
				if err == nil {
					pending.setDelegator(delRec)
					if err := v.saveDelegator(batch, delRec, valRec.TxHash); err != nil {
						return err
					}
					if err := v.saveStakingToValidatorMapping(batch, tx.ID().String(), valRec.TxHash); err != nil {
						return err
					}
				} else {
					pending.setDelegator(delRec)
					if err := v.saveDelegator(batch, delRec, ""); err != nil {
						return err
					}
				}

			case *txs.RemoveSubnetValidatorTx:
				// Permissioned subnet validator removal
				// Find the validator by nodeId and subnet
				nodeID := utx.NodeID.String()
				subnetID := utx.Subnet.String()

				// Search for active validator
				rec, err := v.findActiveValidatorForRemoval(nodeID, subnetID)
				if err == nil {
					rec.RemoveTxHash = tx.ID().String()
					rec.RemoveTimestamp = blk.Timestamp
					pending.setValidator(rec)
					if err := v.updateValidator(batch, rec); err != nil {
						return err
					}
				}

			case *txs.RewardValidatorTx:
				// Staking period completed - mark validator as completed
				stakingTxID := utx.TxID.String()

				// First check if it's a validator
				rec, err := getValidator(stakingTxID)
				if err == nil {
					rec.RewardTxHash = tx.ID().String()

					// Extract reward amounts from reward UTXOs if available
					if rewardUTXOs, ok := blk.RewardUTXOs[utx.TxID]; ok {
						var totalReward uint64
						for _, utxo := range rewardUTXOs {
							if out, ok := utxo.Out.(*secp256k1fx.TransferOutput); ok {
								totalReward += out.Amt
							}
						}
						rec.ValidationRewardAmount = fmt.Sprintf("%d", totalReward)
					}

					pending.setValidator(rec)
					if err := v.updateValidator(batch, rec); err != nil {
						return err
					}
					continue
				}

				// Check if it's a delegator
				delRec, err := getDelegator(stakingTxID)
				if err == nil {
					delRec.Completed = true
					delRec.RewardTxHash = tx.ID().String()
					pending.setDelegator(delRec)
					if err := v.updateDelegator(batch, delRec); err != nil {
						return err
					}
				}
			}
		}
	}

	lastHeight := blocks[len(blocks)-1].Height
	db.SaveWatermark(batch, "p-blocks", lastHeight)

	return batch.Commit(nil)
}

// findActiveValidatorForRemoval finds a validator that can be removed
func (v *Validators) findActiveValidatorForRemoval(nodeID, subnetID string) (*ValidatorRecord, error) {
	prefix := []byte(fmt.Sprintf("vn:%s:", nodeID))
	upper := []byte(fmt.Sprintf("vn:%s;", nodeID))

	iter, _ := v.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: upper,
	})
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		parts := splitKey(string(iter.Key()), 4)
		if len(parts) < 4 {
			continue
		}
		txHash := parts[3]

		rec, err := v.getValidator(txHash)
		if err != nil {
			continue
		}

		// Match subnet and ensure not already removed/completed
		if rec.SubnetID == subnetID && rec.RemoveTxHash == "" && rec.RewardTxHash == "" {
			return rec, nil
		}
	}

	return nil, fmt.Errorf("active validator not found")
}

func extractAddrs(owner any, hrp string) []string {
	if o, ok := owner.(*secp256k1fx.OutputOwners); ok {
		addrs := make([]string, len(o.Addrs))
		for i, a := range o.Addrs {
			s, err := address.FormatBech32(hrp, a.Bytes())
			if err != nil {
				addrs[i] = a.String()
			} else {
				addrs[i] = s
			}
		}
		return addrs
	}
	return nil
}
