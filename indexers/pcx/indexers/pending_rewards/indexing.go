package pending_rewards

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
	"github.com/cockroachdb/pebble/v2"
)

// stakingTxMeta stores metadata needed to bust cache when RewardValidatorTx arrives.
type stakingTxMeta struct {
	NodeID    string   `json:"n"`
	Addresses []string `json:"a"`
}

// ProcessPChainBatch tracks staking txs and busts cache when stakers are added/removed.
func (p *PendingRewards) ProcessPChainBatch(ctx context.Context, blocks []indexer.PBlock) error {
	if p.cacheDB == nil || len(blocks) == 0 {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	batch := p.cacheDB.NewBatch()
	defer batch.Close()

	var lastHeight uint64
	for _, block := range blocks {
		lastHeight = block.Height
		for _, ptx := range block.Block.Txs() {
			switch utx := ptx.Unsigned.(type) {
			case *txs.AddValidatorTx:
				nodeID := utx.NodeID().String()
				addrs := p.extractAddrs(utx.RewardsOwner)
				p.recordStakingTx(batch, ptx.ID(), nodeID, addrs)
				p.bustCache(batch, nodeID, addrs)

			case *txs.AddDelegatorTx:
				nodeID := utx.NodeID().String()
				addrs := p.extractAddrs(utx.DelegationRewardsOwner)
				p.recordStakingTx(batch, ptx.ID(), nodeID, addrs)
				p.bustCache(batch, nodeID, addrs)

			case *txs.AddPermissionlessValidatorTx:
				// Only Primary Network
				if utx.Subnet != ids.Empty {
					continue
				}
				nodeID := utx.NodeID().String()
				addrs := p.extractAddrs(utx.ValidatorRewardsOwner)
				addrs = append(addrs, p.extractAddrs(utx.DelegatorRewardsOwner)...)
				p.recordStakingTx(batch, ptx.ID(), nodeID, addrs)
				p.bustCache(batch, nodeID, addrs)

			case *txs.AddPermissionlessDelegatorTx:
				// Only Primary Network
				if utx.Subnet != ids.Empty {
					continue
				}
				nodeID := utx.NodeID().String()
				addrs := p.extractAddrs(utx.DelegationRewardsOwner)
				p.recordStakingTx(batch, ptx.ID(), nodeID, addrs)
				p.bustCache(batch, nodeID, addrs)

			case *txs.RewardValidatorTx:
				// Staker finished - lookup metadata and bust cache
				meta := p.getStakingTxMeta(utx.TxID)
				if meta != nil {
					p.bustCache(batch, meta.NodeID, meta.Addresses)
					batch.Delete([]byte(stakingTxMetadataKey+utx.TxID.String()), nil)
				}
			}
		}
	}

	// Update watermark to last block in batch
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, lastHeight)
	batch.Set([]byte(watermarkKey), buf, nil)

	// NoSync - we don't care about crash safety, will reindex on restart
	return batch.Commit(pebble.NoSync)
}

func (p *PendingRewards) recordStakingTx(batch *pebble.Batch, txID ids.ID, nodeID string, addrs []string) {
	meta := stakingTxMeta{NodeID: nodeID, Addresses: addrs}
	data, _ := json.Marshal(meta)
	batch.Set([]byte(stakingTxMetadataKey+txID.String()), data, nil)
}

func (p *PendingRewards) getStakingTxMeta(txID ids.ID) *stakingTxMeta {
	data, closer, err := p.cacheDB.Get([]byte(stakingTxMetadataKey + txID.String()))
	if err != nil {
		return nil
	}
	defer closer.Close()
	var meta stakingTxMeta
	if json.Unmarshal(data, &meta) != nil {
		return nil
	}
	return &meta
}

func (p *PendingRewards) bustCache(batch *pebble.Batch, nodeID string, addrs []string) {
	// Bust by nodeId
	if nodeID != "" {
		batch.Delete([]byte(cacheKeyPrefixNode+nodeID), nil)
	}
	// Bust by each address (normalize to avax1 format)
	for _, addr := range addrs {
		normalized := normalizeAddr(addr)
		batch.Delete([]byte(cacheKeyPrefixAddr+normalized), nil)
	}
}

func (p *PendingRewards) extractAddrs(owner any) []string {
	if o, ok := owner.(*secp256k1fx.OutputOwners); ok {
		hrp := getHRP(p.networkID)
		addrs := make([]string, len(o.Addrs))
		for i, a := range o.Addrs {
			s, err := address.FormatBech32(hrp, a.Bytes())
			if err != nil {
				addrs[i] = a.String() // fallback to hex
			} else {
				addrs[i] = s
			}
		}
		return addrs
	}
	return nil
}

func getHRP(networkID uint32) string {
	if networkID == 5 {
		return "fuji"
	}
	return "avax"
}

func normalizeAddr(addr string) string {
	if strings.HasPrefix(addr, "P-") {
		return addr[2:]
	}
	return addr
}

