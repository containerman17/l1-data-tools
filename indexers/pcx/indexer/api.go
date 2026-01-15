package indexer

import (
	"context"
	"net/http"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
)

// ============ Block Types ============

// PBlock is a parsed P-chain block with height and pre-fetched reward UTXOs.
// RewardUTXOs maps staking transaction IDs to their reward UTXOs (fetched via GetRewardUTXOs).
// This avoids RPC calls during indexing - all data is fetched upfront by the runner.
type PBlock struct {
	Height uint64

	Timestamp   int64
	Block       block.Block
	RewardUTXOs map[ids.ID][]avax.UTXO // stakingTxID -> parsed reward UTXOs
}

// XTx is a pre-Cortina X-chain transaction (from Index API).
type XTx struct {
	Index     uint64 // Sequential index (0, 1, 2, ...)
	Timestamp int64  // Transaction timestamp (Unix seconds)
	Bytes     []byte // Raw transaction bytes (without timestamp prefix)
}

// XBlock is a post-Cortina X-chain block.
type XBlock struct {
	Height uint64
	Bytes  []byte // Raw block bytes
}

// CBlock is a C-chain block with atomic tx data and metadata.
// Designed for both UTXO indexing and future /blocks API.
type CBlock struct {
	// Core fields
	Height     uint64
	Timestamp  int64
	Hash       string
	ParentHash string

	// Size and counts
	Size    int
	TxCount int

	// Gas fields
	GasLimit      uint64
	GasUsed       uint64
	BaseFeePerGas uint64

	// Miner/Coinbase
	Miner string

	// Atomic tx data
	ExtraData   []byte // blockExtraData (atomic txs only, empty if none)
	ExtDataHash string
}

// ============ Chain Interfaces ============

// PChainIndexer processes P-chain blocks.
type PChainIndexer interface {
	Name() string
	Init(ctx context.Context, baseDir string, networkID uint32) error
	ProcessPChainBatch(ctx context.Context, blocks []PBlock) error
	GetPChainWatermark() (uint64, error)
	RegisterRoutes(mux *http.ServeMux)
}

// XChainIndexer processes X-chain data.
// Pre-Cortina: transactions via Index API (no blocks).
// Post-Cortina: blocks.
type XChainIndexer interface {
	Name() string
	Init(ctx context.Context, baseDir string, networkID uint32) error
	// Pre-Cortina transactions (sequential index, no blocks)
	ProcessXChainPreCortinaTxs(ctx context.Context, txs []XTx) error
	GetXChainPreCortinaWatermark() (uint64, error)
	// Post-Cortina blocks
	ProcessXChainBlocks(ctx context.Context, blocks []XBlock) error
	GetXChainBlockWatermark() (uint64, error)
	RegisterRoutes(mux *http.ServeMux)
}

// CChainIndexer processes C-chain blocks.
type CChainIndexer interface {
	Name() string
	Init(ctx context.Context, baseDir string, networkID uint32) error
	ProcessCChainBatch(ctx context.Context, blocks []CBlock) error
	GetCChainWatermark() (uint64, error)
	RegisterRoutes(mux *http.ServeMux)
}

// ============ Test Support ============

// TestCase defines a single test against Glacier API.
type TestCase struct {
	Name          string                                   // Test name for display
	Path          string                                   // URL path (e.g., "/v1/networks/mainnet/rewards:listPending")
	Params        map[string]string                        // Query parameters
	SkipFields    []string                                 // Fields to completely ignore
	ApproxFields  map[string]float64                       // Fields to compare with tolerance
	FilterGlacier func(resp map[string]any) map[string]any // Optional: filter Glacier response
	LocalOnly     bool                                     // If true, skip Glacier comparison
	MaxTimeMs     int                                      // If > 0, fail if local response takes longer
	Skip          bool                                     // If true, skip the test
	Only          bool                                     // If true, run only this test
}

// Testable is an optional interface for indexers with self-tests.
type Testable interface {
	TestCases() []TestCase
}
