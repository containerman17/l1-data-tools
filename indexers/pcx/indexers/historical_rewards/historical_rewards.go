package historical_rewards

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/pchain"
	_ "github.com/mattn/go-sqlite3"
)

// HistoricalRewards tracks completed staking rewards.
// Implements PChainIndexer.
type HistoricalRewards struct {
	db        *sql.DB
	rpc       *pchain.CachedClient
	networkID uint32
	mu        sync.RWMutex
}

// New creates a new HistoricalRewards indexer.
// rpc is required for fetching reward UTXOs.
func New(rpc *pchain.CachedClient) *HistoricalRewards {
	return &HistoricalRewards{rpc: rpc}
}

func (h *HistoricalRewards) Name() string { return "historical_rewards" }

// Init opens the SQLite database and creates schema.
func (h *HistoricalRewards) Init(ctx context.Context, baseDir string, networkID uint32) error {
	h.networkID = networkID

	// Ensure directory exists
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	dbPath := filepath.Join(baseDir, "rewards.db")
	// SQLite optimizations:
	// journal_mode=WAL: Allows concurrent reads during writes
	// synchronous=NORMAL: Good balance with WAL
	// cache_size=-200000: 200MB cache
	// busy_timeout=5000: Wait up to 5s for locks
	db, err := sql.Open("sqlite3", dbPath+"?mode=rwc&_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000&_cache_size=-200000")
	if err != nil {
		return err
	}
	h.db = db

	// Schema
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS staking_records (
			tx_id TEXT PRIMARY KEY,
			reward_addrs TEXT NOT NULL,
			node_id TEXT NOT NULL,
			stake_amount INTEGER NOT NULL,
			start_time INTEGER NOT NULL,
			end_time INTEGER NOT NULL,
			reward_type TEXT NOT NULL,
			completed INTEGER DEFAULT 0,
			reward_tx_id TEXT,
			reward_amount INTEGER DEFAULT 0,
			reward_utxo_id TEXT,
			block_height INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_completed ON staking_records(completed);
		CREATE INDEX IF NOT EXISTS idx_end_time ON staking_records(end_time);
		CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value INTEGER);
	`)
	return err
}

// GetPChainWatermark returns the last processed block height.
func (h *HistoricalRewards) GetPChainWatermark() (uint64, error) {
	if h.db == nil {
		return 0, nil
	}
	var val uint64
	err := h.db.QueryRow(`SELECT value FROM meta WHERE key='watermark'`).Scan(&val)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		log.Printf("[historical_rewards] GetPChainWatermark error: %v", err)
	}
	return val, err
}

// Close closes the database.
func (h *HistoricalRewards) Close() error {
	if h.db != nil {
		return h.db.Close()
	}
	return nil
}

