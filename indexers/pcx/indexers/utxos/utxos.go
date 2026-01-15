package utxos

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/db"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/pchain"
	"github.com/cockroachdb/pebble/v2"
)

// Chain IDs
const (
	pChainID        = "11111111111111111111111111111111LpoYY"
	cChainIDMainnet = "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5"
	cChainIDFuji    = "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp"
	xChainIDMainnet = "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM"
	xChainIDFuji    = "2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm"
)

var keyBlkPrefix = []byte("blk:")

// UTXOs indexes UTXOs across P, X, and C chains.
// Implements PChainIndexer, XChainIndexer, and CChainIndexer.
type UTXOs struct {
	db        *pebble.DB
	blocksDB  *pebble.DB // P-chain blocks for timestamps
	networkID uint32
	hrp       string
	baseDir   string

	avmParser block.Parser

	initOnce sync.Once
	initErr  error
	mu       sync.RWMutex
}

// New creates a new UTXOs indexer.
func New() *UTXOs {
	return &UTXOs{}
}

func (u *UTXOs) Name() string { return "utxos" }

// Init initializes the database. Safe to call multiple times (sync.Once).
func (u *UTXOs) Init(ctx context.Context, baseDir string, networkID uint32) error {
	u.initOnce.Do(func() {
		u.baseDir = baseDir
		u.networkID = networkID
		u.hrp = pchain.GetHRP(networkID)

		var err error
		u.db, err = pebble.Open(baseDir, &pebble.Options{Logger: db.QuietLogger()})
		if err != nil {
			u.initErr = err
			return
		}

		// Initialize AVM parser with all FXs
		u.avmParser, err = block.NewParser([]fxs.Fx{
			&secp256k1fx.Fx{},
			&nftfx.Fx{},
			&propertyfx.Fx{},
		})
		if err != nil {
			u.initErr = err
			return
		}
	})
	return u.initErr
}

// SetBlocksDB sets the P-chain blocks database for timestamp lookups.
func (u *UTXOs) SetBlocksDB(db *pebble.DB) {
	u.blocksDB = db
}

// Close closes the database.
func (u *UTXOs) Close() error {
	if u.db != nil {
		return u.db.Close()
	}
	return nil
}
