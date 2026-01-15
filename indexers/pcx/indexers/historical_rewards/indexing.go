package historical_rewards

import (
	"context"
	"encoding/json"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
)

// ProcessPChainBatch tracks staking transactions and their rewards.
func (h *HistoricalRewards) ProcessPChainBatch(ctx context.Context, blocks []indexer.PBlock) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	insertStaking, err := tx.Prepare(`
		INSERT OR REPLACE INTO staking_records 
		(tx_id, reward_addrs, node_id, stake_amount, start_time, end_time, reward_type, block_height)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer insertStaking.Close()

	completeStaking, err := tx.Prepare(`
		UPDATE staking_records SET completed=1, reward_tx_id=?, reward_amount=?, reward_utxo_id=?
		WHERE tx_id=?
	`)
	if err != nil {
		return err
	}
	defer completeStaking.Close()

	var lastHeight uint64
	for _, block := range blocks {
		lastHeight = block.Height
		for _, ptx := range block.Block.Txs() {
			switch utx := ptx.Unsigned.(type) {
			case *txs.AddValidatorTx:
				addrs := encodeAddrsJSON(getRewardOwnerAddrs(utx.RewardsOwner), h.networkID)
				insertStaking.Exec(ptx.ID().String(), addrs, utx.NodeID().String(),
					utx.Weight(), utx.StartTime().Unix(), utx.EndTime().Unix(), "VALIDATOR", block.Height)

			case *txs.AddDelegatorTx:
				addrs := encodeAddrsJSON(getRewardOwnerAddrs(utx.DelegationRewardsOwner), h.networkID)
				insertStaking.Exec(ptx.ID().String(), addrs, utx.NodeID().String(),
					utx.Weight(), utx.StartTime().Unix(), utx.EndTime().Unix(), "DELEGATOR", block.Height)

			case *txs.AddPermissionlessValidatorTx:
				if utx.Subnet != ids.Empty {
					continue
				}
				addrs := encodeAddrsJSON(getRewardOwnerAddrs(utx.ValidatorRewardsOwner), h.networkID)
				insertStaking.Exec(ptx.ID().String(), addrs, utx.NodeID().String(),
					utx.Weight(), utx.StartTime().Unix(), utx.EndTime().Unix(), "VALIDATOR", block.Height)

			case *txs.AddPermissionlessDelegatorTx:
				if utx.Subnet != ids.Empty {
					continue
				}
				addrs := encodeAddrsJSON(getRewardOwnerAddrs(utx.DelegationRewardsOwner), h.networkID)
				insertStaking.Exec(ptx.ID().String(), addrs, utx.NodeID().String(),
					utx.Weight(), utx.StartTime().Unix(), utx.EndTime().Unix(), "DELEGATOR", block.Height)

			case *txs.RewardValidatorTx:
				stakingTxID := utx.TxID
				// Don't fetch rewards now (slow RPC).
				// Mark as completed with empty reward_utxo_id.
				// API handler will lazy-load on first request.
				completeStaking.Exec(ptx.ID().String(), 0, "", stakingTxID.String())
			}
		}
	}

	// Update watermark
	if _, err := tx.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('watermark', ?)`, lastHeight); err != nil {
		return err
	}

	return tx.Commit()
}

func getRewardOwnerAddrs(owner any) []ids.ShortID {
	if o, ok := owner.(*secp256k1fx.OutputOwners); ok {
		return o.Addrs
	}
	return nil
}

func encodeAddrsJSON(addrs []ids.ShortID, networkID uint32) string {
	strs := make([]string, len(addrs))
	hrp := getHRP(networkID)
	for i, a := range addrs {
		s, err := address.FormatBech32(hrp, a.Bytes())
		if err != nil {
			strs[i] = a.String()
		} else {
			strs[i] = s
		}
	}
	b, _ := json.Marshal(strs)
	return string(b)
}

func getHRP(networkID uint32) string {
	if networkID == 5 {
		return "fuji"
	}
	return "avax"
}

