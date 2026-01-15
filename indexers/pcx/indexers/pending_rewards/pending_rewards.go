package pending_rewards

import (
	"context"
	"encoding/binary"
	"sync"

	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/db"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/pchain"
	"github.com/cockroachdb/pebble/v2"
)

// Cache key prefixes
const (
	cacheKeyPrefixAddr   = "pending:addr:"
	cacheKeyPrefixNode   = "pending:node:"
	stakingTxMetadataKey = "staking:meta:"
	watermarkKey         = "watermark"
)

// PendingRewards caches pending rewards responses and invalidates on staking changes.
// Implements PChainIndexer.
type PendingRewards struct {
	cacheDB   *pebble.DB
	rpc       *pchain.CachedClient
	networkID uint32
	mu        sync.RWMutex
}

// New creates a new PendingRewards indexer.
// rpc is required for fetching current validators.
func New(rpc *pchain.CachedClient) *PendingRewards {
	return &PendingRewards{rpc: rpc}
}

func (p *PendingRewards) Name() string { return "pending_rewards" }

// Init opens the cache database.
func (p *PendingRewards) Init(ctx context.Context, baseDir string, networkID uint32) error {
	p.networkID = networkID

	opts := &pebble.Options{Logger: db.QuietLogger()}
	pdb, err := pebble.Open(baseDir, opts)
	if err != nil {
		return err
	}
	p.cacheDB = pdb
	return nil
}

// GetPChainWatermark returns the last processed block height.
// If DB is empty (fresh start), returns 0 - caller should skip to currentHeight
// since we don't need historical data (we're just cache busting, nothing to bust yet).
func (p *PendingRewards) GetPChainWatermark() (uint64, error) {
	if p.cacheDB == nil {
		return 0, nil
	}
	val, closer, err := p.cacheDB.Get([]byte(watermarkKey))
	if err == pebble.ErrNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer closer.Close()
	if len(val) < 8 {
		return 0, nil
	}
	return binary.BigEndian.Uint64(val), nil
}

// Close closes the cache database.
func (p *PendingRewards) Close() error {
	if p.cacheDB != nil {
		return p.cacheDB.Close()
	}
	return nil
}
