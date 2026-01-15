package utxos

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	ptxs "github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
	"github.com/cockroachdb/pebble/v2"
)

// ProcessPChainBatch indexes UTXOs from P-chain blocks.
func (u *UTXOs) ProcessPChainBatch(ctx context.Context, blocks []indexer.PBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	batch := u.db.NewIndexedBatch()
	defer batch.Close()

	for _, blk := range blocks {
		height := blk.Height
		timestamp := blk.Timestamp

		for _, ptx := range blk.Block.Txs() {
			txID := ptx.ID()
			unsigned := ptx.Unsigned
			outs := unsigned.Outputs()

			var ins []*avax.TransferableInput
			var stakeOuts []*avax.TransferableOutput
			var exportedOuts []*avax.TransferableOutput
			var stakeEnd int64
			var destChain ids.ID

			// Snowflake export fields
			var txType string
			var nodeID string
			var rewardAddresses []string

			// Extract inputs/outputs based on tx type
			switch t := unsigned.(type) {
			case *ptxs.AddValidatorTx:
				txType = "AddValidatorTx"
				ins = t.Ins
				stakeOuts = t.StakeOuts
				stakeEnd = t.EndTime().Unix()
				nodeID = t.NodeID().String()
				rewardAddresses = u.extractOwnerAddresses(t.RewardsOwner)

			case *ptxs.AddDelegatorTx:
				txType = "AddDelegatorTx"
				ins = t.Ins
				stakeOuts = t.StakeOuts
				stakeEnd = t.EndTime().Unix()
				nodeID = t.NodeID().String()
				rewardAddresses = u.extractOwnerAddresses(t.RewardsOwner)

			case *ptxs.AddPermissionlessValidatorTx:
				txType = "AddPermissionlessValidatorTx"
				ins = t.Ins
				stakeOuts = t.StakeOuts
				stakeEnd = t.EndTime().Unix()
				nodeID = t.NodeID().String()
				rewardAddresses = u.extractOwnerAddresses(t.DelegationRewardsOwner)

			case *ptxs.AddPermissionlessDelegatorTx:
				txType = "AddPermissionlessDelegatorTx"
				ins = t.Ins
				stakeOuts = t.StakeOuts
				stakeEnd = t.EndTime().Unix()
				nodeID = t.NodeID().String()
				rewardAddresses = u.extractOwnerAddresses(t.DelegationRewardsOwner)

			case *ptxs.ImportTx:
				txType = "ImportTx"
				ins = t.Ins
				u.processImportTx(batch, t, txID, height, timestamp)

			case *ptxs.ExportTx:
				txType = "ExportTx"
				ins = t.Ins
				exportedOuts = t.ExportedOutputs
				destChain = t.DestinationChain

			case *ptxs.CreateSubnetTx:
				txType = "CreateSubnetTx"
				ins = t.Ins
			case *ptxs.TransferSubnetOwnershipTx:
				txType = "TransferSubnetOwnershipTx"
				ins = t.Ins
			case *ptxs.AddSubnetValidatorTx:
				txType = "AddSubnetValidatorTx"
				ins = t.Ins
			case *ptxs.CreateChainTx:
				txType = "CreateChainTx"
				ins = t.Ins
			case *ptxs.BaseTx:
				txType = "BaseTx"
				ins = t.Ins
			case *ptxs.TransformSubnetTx:
				txType = "TransformSubnetTx"
				ins = t.Ins
			case *ptxs.RemoveSubnetValidatorTx:
				txType = "RemoveSubnetValidatorTx"
				ins = t.Ins
			case *ptxs.ConvertSubnetToL1Tx:
				txType = "ConvertSubnetToL1Tx"
				ins = t.Ins
			case *ptxs.IncreaseL1ValidatorBalanceTx:
				txType = "IncreaseL1ValidatorBalanceTx"
				ins = t.Ins
			case *ptxs.DisableL1ValidatorTx:
				txType = "DisableL1ValidatorTx"
				ins = t.Ins
			case *ptxs.SetL1ValidatorWeightTx:
				txType = "SetL1ValidatorWeightTx"
				ins = t.Ins
			case *ptxs.RegisterL1ValidatorTx:
				txType = "RegisterL1ValidatorTx"
				ins = t.Ins

			case *ptxs.RewardValidatorTx:
				txType = "RewardValidatorTx"
				rewards := blk.RewardUTXOs[t.TxID]
				u.processRewardTx(batch, t.TxID, height, timestamp, rewards)

			case *ptxs.AdvanceTimeTx:
				txType = "AdvanceTimeTx"
				// No UTXOs
			}

			// Set txType on indexed UTXOs (will be applied via context)
			// For all outputs, upsert txType
			if txType != "" {
				allOutputs := append(append([]*avax.TransferableOutput{}, outs...), stakeOuts...)
				allOutputs = append(allOutputs, exportedOuts...)
				for i := range allOutputs {
					utxoid := avax.UTXOID{TxID: txID, OutputIndex: uint32(i)}
					utxoID := utxoid.InputID().String()
					u.upsertPChainUTXO(batch, utxoID, map[string]any{
						"txType": txType,
					})
				}
			}

			// 1. Mark consumed UTXOs
			for _, in := range ins {
				utxoID := in.UTXOID.InputID().String()
				u.markConsumed(batch, utxoID, txID.String(), height, timestamp)
			}

			// 2. Index regular outputs
			u.indexOutputs(batch, txID, 0, outs, height, timestamp, false, 0, 0, pChainID, pChainID)

			// 3. Index stake outputs (after regular outputs) and record their UTXO IDs
			if len(stakeOuts) > 0 {
				stakeOutputIDs := u.indexOutputs(batch, txID, len(outs), stakeOuts, height, timestamp, true, 0, stakeEnd, pChainID, pChainID)
				u.saveStakingOutputs(batch, txID.String(), stakeOutputIDs)

				// Set nodeID and rewardAddresses on staking outputs
				if nodeID != "" || len(rewardAddresses) > 0 {
					for _, utxoID := range stakeOutputIDs {
						updates := map[string]any{}
						if nodeID != "" {
							updates["nodeId"] = nodeID
						}
						if len(rewardAddresses) > 0 {
							updates["rewardAddresses"] = rewardAddresses
						}
						u.upsertPChainUTXO(batch, utxoID, updates)
					}
				}
			}

			// 4. Index exported outputs (created on P-Chain, consumed on destination chain)
			if len(exportedOuts) > 0 {
				destChainStr := destChain.String()
				u.indexOutputs(batch, txID, len(outs)+len(stakeOuts), exportedOuts, height, timestamp, false, 0, 0, pChainID, destChainStr)

				// Also write to C-Chain storage if destination is C-Chain
				if destChainStr == cChainIDFuji || destChainStr == cChainIDMainnet {
					u.indexOutputsToChain(batch, txID, len(outs)+len(stakeOuts), exportedOuts, height, timestamp, pChainID, destChainStr, prefixCChainUTXO, prefixCChainAddr)
				}
				// Also write to X-Chain storage if destination is X-Chain
				if destChainStr == xChainIDFuji || destChainStr == xChainIDMainnet {
					u.indexOutputsToChain(batch, txID, len(outs)+len(stakeOuts), exportedOuts, height, timestamp, pChainID, destChainStr, prefixXChainUTXO, prefixXChainAddr)
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

// indexOutputs indexes regular or staked outputs to P-Chain storage.
// Returns the list of UTXO IDs that were indexed (for tracking stake outputs).
func (u *UTXOs) indexOutputs(batch *pebble.Batch, txID ids.ID, startIdx int, outputs []*avax.TransferableOutput, height uint64, timestamp int64, staked bool, stakeStart, stakeEnd int64, createdOnChain, consumedOnChain string) []string {
	var utxoIDs []string
	for i := range outputs {
		outputIdx := uint32(startIdx + i)
		utxoid := avax.UTXOID{TxID: txID, OutputIndex: outputIdx}
		utxoIDs = append(utxoIDs, utxoid.InputID().String())
	}

	u.indexOutputsToChain(batch, txID, startIdx, outputs, height, timestamp, createdOnChain, consumedOnChain, prefixPChainUTXO, prefixPChainAddr)
	if staked {
		// Handle staking-specific fields
		for i := range outputs {
			outputIdx := uint32(startIdx + i)
			utxoid := avax.UTXOID{TxID: txID, OutputIndex: outputIdx}
			utxoID := utxoid.InputID().String()

			start := stakeStart
			if start == 0 {
				start = timestamp
			}
			u.upsertPChainUTXO(batch, utxoID, map[string]any{
				"staked":             true,
				"utxoStartTimestamp": start,
				"utxoEndTimestamp":   stakeEnd,
			})
		}
	}
	return utxoIDs
}

// indexOutputsToChain indexes outputs to a specific chain storage.
func (u *UTXOs) indexOutputsToChain(batch *pebble.Batch, txID ids.ID, startIdx int, outputs []*avax.TransferableOutput, height uint64, timestamp int64, createdOnChain, consumedOnChain, utxoPrefix, addrPrefix string) {
	for i, out := range outputs {
		u.indexAnyOutputToChain(batch, txID, uint32(startIdx+i), out.Out, out.AssetID(), height, timestamp, createdOnChain, consumedOnChain, utxoPrefix, addrPrefix)
	}
}

// indexAnyOutputToChain indexes any output type to a specific chain storage.
func (u *UTXOs) indexAnyOutputToChain(batch *pebble.Batch, txID ids.ID, outputIdx uint32, out any, assetID ids.ID, height uint64, timestamp int64, createdOnChain, consumedOnChain, utxoPrefix, addrPrefix string) {
	utxoid := avax.UTXOID{TxID: txID, OutputIndex: outputIdx}
	utxoID := utxoid.InputID()

	utxo := u.buildUTXO(out, txID, outputIdx, assetID, height, timestamp, false, 0, 0, createdOnChain, consumedOnChain)
	if utxo == nil || len(utxo.Addresses) == 0 {
		return
	}
	utxo.UTXOId = utxoID.String()

	// X-Chain specific: lowercase utxoType
	if utxoPrefix == prefixXChainUTXO {
		utxo.UTXOType = strings.ToLower(utxo.UTXOType)
	}

	// Generate utxoBytes
	utxo.UTXOBytes = u.generateAnyUTXOBytes(&utxoid, out, assetID)

	// Store UTXO
	saveUTXO(batch, utxoPrefix, utxo)

	// Store address index for each owner
	for _, addr := range utxo.Addresses {
		batch.Set([]byte(addrPrefix+addr+":"+utxo.UTXOId), nil, nil)
	}
}

// generateAnyUTXOBytes creates the hex-encoded UTXO bytes for any output type.
func (u *UTXOs) generateAnyUTXOBytes(utxoid *avax.UTXOID, out any, assetID ids.ID) string {
	utxo := avax.UTXO{
		UTXOID: *utxoid,
		Asset:  avax.Asset{ID: assetID},
		Out:    out.(verify.State),
	}

	// Use AVM codec if it's an X-chain output, otherwise PlatformVM codec
	// Actually, both codecs handle UTXOs similarly if FXs are registered.
	// We'll use AVM codec as it has all 3 FXs registered in our indexer.
	bytes, err := u.avmParser.Codec().Marshal(0, &utxo)
	if err != nil {
		return ""
	}
	return "0x" + hex.EncodeToString(bytes)
}

// buildUTXO extracts UTXO data from an output.
func (u *UTXOs) buildUTXO(out any, txID ids.ID, outputIdx uint32, assetID ids.ID, height uint64, timestamp int64, staked bool, stakeStart, stakeEnd int64, createdOnChain, consumedOnChain string) *StoredUTXO {
	utxo := &StoredUTXO{
		TxHash:            txID.String(),
		OutputIndex:       outputIdx,
		AssetID:           assetID.String(),
		BlockNumber:       fmt.Sprintf("%d", height),
		BlockTimestamp:    timestamp,
		Staked:            staked,
		CreatedOnChainID:  createdOnChain,
		ConsumedOnChainID: consumedOnChain,
	}

	if staked {
		// For staked UTXOs, always set start timestamp (use block timestamp if not provided)
		start := stakeStart
		if start == 0 {
			start = timestamp
		}
		utxo.UTXOStartTimestamp = &start
		if stakeEnd > 0 {
			utxo.UTXOEndTimestamp = &stakeEnd
		}
	}

	switch o := out.(type) {
	case *secp256k1fx.TransferOutput:
		utxo.FxID = fxIDSecp256k1
		utxo.Amount = fmt.Sprintf("%d", o.Amt)
		utxo.Addresses = u.formatAddresses(o.Addrs)
		// Only set platformLocktime for P-Chain UTXOs (Glacier omits it for cross-chain)
		if createdOnChain == pChainID {
			utxo.PlatformLocktime = &o.Locktime
		}
		utxo.Threshold = o.Threshold
		utxo.UTXOType = "TRANSFER"

	case *stakeable.LockOut:
		utxo.FxID = fxIDSecp256k1
		// Only set platformLocktime for P-Chain UTXOs (Glacier omits it for cross-chain)
		if createdOnChain == pChainID {
			utxo.PlatformLocktime = &o.Locktime
		}
		// StakeableLocktime is the locktime for StakeableLockOut
		utxo.StakeableLocktime = &o.Locktime
		utxo.UTXOType = "STAKEABLE_LOCK"
		if inner, ok := o.TransferableOut.(*secp256k1fx.TransferOutput); ok {
			utxo.Amount = fmt.Sprintf("%d", inner.Amt)
			utxo.Addresses = u.formatAddresses(inner.Addrs)
			utxo.Threshold = inner.Threshold
		}

	case *nftfx.TransferOutput:
		utxo.FxID = fxIDNFT
		utxo.Amount = "0"
		utxo.Addresses = u.formatAddresses(o.Addrs)
		utxo.Threshold = o.Threshold
		if createdOnChain == pChainID {
			utxo.PlatformLocktime = &o.Locktime
		}
		utxo.UTXOType = "TRANSFER"
		gid := o.GroupID
		utxo.GroupID = &gid
		if len(o.Payload) > 0 {
			utxo.Payload = "0x" + hex.EncodeToString(o.Payload)
		}

	case *nftfx.MintOutput:
		utxo.FxID = fxIDNFT
		utxo.Amount = "0"
		utxo.Addresses = u.formatAddresses(o.Addrs)
		utxo.Threshold = o.Threshold
		if createdOnChain == pChainID {
			utxo.PlatformLocktime = &o.Locktime
		}
		utxo.UTXOType = "MINT"
		gid := o.GroupID
		utxo.GroupID = &gid

	case *propertyfx.OwnedOutput:
		utxo.FxID = fxIDProperty
		utxo.Amount = "0"
		utxo.Addresses = u.formatAddresses(o.Addrs)
		utxo.Threshold = o.Threshold
		if createdOnChain == pChainID {
			utxo.PlatformLocktime = &o.Locktime
		}
		utxo.UTXOType = "TRANSFER"

	case *propertyfx.MintOutput:
		utxo.FxID = fxIDProperty
		utxo.Amount = "0"
		utxo.Addresses = u.formatAddresses(o.Addrs)
		utxo.Threshold = o.Threshold
		if createdOnChain == pChainID {
			utxo.PlatformLocktime = &o.Locktime
		}
		utxo.UTXOType = "MINT"

	default:
		return nil
	}

	return utxo
}

// formatAddresses converts ids.ShortID addresses to bech32 strings.
func (u *UTXOs) formatAddresses(addrs []ids.ShortID) []string {
	result := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if s, _ := address.FormatBech32(u.hrp, addr.Bytes()); s != "" {
			result = append(result, s)
		}
	}
	return result
}

// extractOwnerAddresses extracts addresses from a reward owner for Snowflake export.
func (u *UTXOs) extractOwnerAddresses(owner any) []string {
	if owner == nil {
		return nil
	}
	if out, ok := owner.(*secp256k1fx.OutputOwners); ok {
		return u.formatAddresses(out.Addrs)
	}
	return nil
}

// markConsumed marks a P-Chain UTXO as spent using upsert.
func (u *UTXOs) markConsumed(batch *pebble.Batch, utxoID string, consumingTxHash string, consumingHeight uint64, consumingTimestamp int64) {
	heightStr := fmt.Sprintf("%d", consumingHeight)
	u.upsertPChainUTXO(batch, utxoID, map[string]any{
		"consumingTxHash":         consumingTxHash,
		"consumingBlockNumber":    heightStr,
		"consumingBlockTimestamp": consumingTimestamp,
	})
}

// processImportTx handles UTXOs imported from other chains.
// We only mark them as spent in the source chain storage.
func (u *UTXOs) processImportTx(batch *pebble.Batch, t *ptxs.ImportTx, consumingTxID ids.ID, consumingHeight uint64, consumingTimestamp int64) {
	sourceChainID := t.SourceChain.String()
	heightStr := fmt.Sprintf("%d", consumingHeight)

	for _, importedIn := range t.ImportedInputs {
		utxoID := importedIn.UTXOID.InputID().String()

		// Mark spent on P-Chain spend index
		markSpent(batch, "p:", utxoID, &SpendInfo{
			ConsumingTxHash:      consumingTxID.String(),
			ConsumingTime:        consumingTimestamp,
			ConsumingBlockNumber: heightStr,
			ConsumedOnChainID:    pChainID,
			CreatedOnChainID:     sourceChainID,
		})

		// Also mark spent on source chain
		if sourceChainID == xChainIDFuji || sourceChainID == xChainIDMainnet {
			markSpent(batch, "x:", utxoID, &SpendInfo{
				ConsumingTxHash:      consumingTxID.String(),
				ConsumingTime:        consumingTimestamp,
				ConsumingBlockNumber: heightStr,
			})
		} else if sourceChainID == cChainIDFuji || sourceChainID == cChainIDMainnet {
			markSpent(batch, "c:", utxoID, &SpendInfo{
				ConsumingTxHash:      consumingTxID.String(),
				ConsumingTime:        consumingTimestamp,
				ConsumingBlockNumber: heightStr,
			})
		}
	}
}

// processRewardTx handles reward UTXOs for completed staking.
// NOTE: Stake outputs are NOT consumed when staking ends - they become
// regular spendable UTXOs (the principal is returned to the owner).
// This function only indexes the NEW reward UTXOs.
func (u *UTXOs) processRewardTx(batch *pebble.Batch, stakingTxID ids.ID, height uint64, timestamp int64, rewards []avax.UTXO) {
	// Clean up the staking outputs mapping (no longer needed for tracking)
	u.deleteStakingOutputs(batch, stakingTxID.String())

	// Index each reward UTXO (these are NEW UTXOs, not replacements)
	for _, reward := range rewards {
		utxoID := reward.InputID().String()
		utxo := u.buildUTXO(reward.Out, reward.TxID, reward.OutputIndex, reward.AssetID(), height, timestamp, false, 0, 0, pChainID, pChainID)
		if utxo == nil {
			continue
		}
		utxo.UTXOId = utxoID

		// Snowflake export fields for reward UTXOs
		utxo.IsReward = true
		utxo.TxType = "RewardValidatorTx"

		// Generate utxoBytes
		utxoid := avax.UTXOID{TxID: reward.TxID, OutputIndex: reward.OutputIndex}
		utxo.UTXOBytes = u.generateAnyUTXOBytes(&utxoid, reward.Out, reward.AssetID())

		// Store UTXO
		savePChainUTXO(batch, utxo)

		// Store address index
		for _, addr := range utxo.Addresses {
			batch.Set([]byte(prefixPChainAddr+addr+":"+utxoID), nil, nil)
		}
	}
}

// saveStakingOutputs stores the mapping of staking tx ID to its stake output UTXO IDs.
func (u *UTXOs) saveStakingOutputs(batch *pebble.Batch, stakingTxID string, utxoIDs []string) {
	data, _ := json.Marshal(utxoIDs)
	batch.Set([]byte(prefixStakingOutputs+stakingTxID), data, nil)
}

// getStakingOutputs retrieves the stake output UTXO IDs for a staking tx.
func (u *UTXOs) getStakingOutputs(stakingTxID string) []string {
	val, closer, err := u.db.Get([]byte(prefixStakingOutputs + stakingTxID))
	if err != nil {
		return nil
	}
	defer closer.Close()

	var utxoIDs []string
	if err := json.Unmarshal(val, &utxoIDs); err != nil {
		return nil
	}
	return utxoIDs
}

// deleteStakingOutputs removes the staking outputs mapping.
func (u *UTXOs) deleteStakingOutputs(batch *pebble.Batch, stakingTxID string) {
	batch.Delete([]byte(prefixStakingOutputs+stakingTxID), nil)
}
