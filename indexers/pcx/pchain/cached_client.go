package pchain

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"strconv"

	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/db"
	"github.com/cockroachdb/pebble/v2"
)

// CachedClient wraps Client and caches immutable RPC responses forever.
// Use this for data that never changes once written (e.g., reward UTXOs for completed staking).
// The cache DB can be safely deleted to force re-fetch - it will repopulate on demand.
type CachedClient struct {
	*Client
	cache *pebble.DB
}

// NewCachedClient creates a cached client with its own persistent cache.
// cacheDir should be a dedicated directory (e.g., "data/5/rpc_cache").
func NewCachedClient(client *Client, cacheDir string) (*CachedClient, error) {
	cache, err := pebble.Open(cacheDir, &pebble.Options{Logger: db.QuietLogger()})
	if err != nil {
		return nil, err
	}
	return &CachedClient{Client: client, cache: cache}, nil
}

// Close closes the cache database.
func (c *CachedClient) Close() error {
	if c.cache != nil {
		return c.cache.Close()
	}
	return nil
}

// GetRewardUTXOs returns reward UTXOs for a completed staking tx.
// Results are cached forever since rewards are immutable once staking completes.
func (c *CachedClient) GetRewardUTXOs(ctx context.Context, stakingTxID string) ([][]byte, error) {
	key := []byte("reward:" + stakingTxID)

	// Check cache first
	if val, closer, err := c.cache.Get(key); err == nil {
		defer closer.Close()
		return decodeUTXOList(val), nil
	}

	// Cache miss - fetch from RPC
	utxos, err := c.fetchRewardUTXOs(ctx, stakingTxID)
	if err != nil {
		return nil, err
	}

	// Cache forever (rewards are immutable)
	if encoded := encodeUTXOList(utxos); encoded != nil {
		c.cache.Set(key, encoded, pebble.NoSync)
	}

	return utxos, nil
}

// fetchRewardUTXOs fetches reward UTXOs directly from RPC (no cache).
func (c *CachedClient) fetchRewardUTXOs(ctx context.Context, stakingTxID string) ([][]byte, error) {
	var result struct {
		UTXOs    []string            `json:"utxos"`
		Encoding formatting.Encoding `json:"encoding"`
	}

	params := map[string]any{
		"txID":     stakingTxID,
		"encoding": "hexnc",
	}

	if err := c.rpcCall(ctx, "platform.getRewardUTXOs", params, &result); err != nil {
		return nil, err
	}

	utxos := make([][]byte, 0, len(result.UTXOs))
	for _, utxoHex := range result.UTXOs {
		utxoBytes, err := formatting.Decode(result.Encoding, utxoHex)
		if err != nil {
			continue
		}
		utxos = append(utxos, utxoBytes)
	}

	return utxos, nil
}

// encodeUTXOList encodes a slice of byte slices for storage.
func encodeUTXOList(utxos [][]byte) []byte {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(utxos); err != nil {
		return nil
	}
	return buf.Bytes()
}

// decodeUTXOList decodes a slice of byte slices from storage.
func decodeUTXOList(data []byte) [][]byte {
	var utxos [][]byte
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&utxos); err != nil {
		return nil
	}
	return utxos
}

// AtomicTxInfo contains block info for a cross-chain atomic transaction
type AtomicTxInfo struct {
	BlockHeight uint64
	Timestamp   int64
}

// GetAtomicTxInfo returns block info for an atomic tx on a source chain (C-Chain).
// Results are cached forever since this is immutable historical data.
func (c *CachedClient) GetAtomicTxInfo(ctx context.Context, sourceChainID, txID string) (*AtomicTxInfo, error) {
	key := []byte("atomic:" + sourceChainID + ":" + txID)

	// Check cache first
	if val, closer, err := c.cache.Get(key); err == nil {
		defer closer.Close()
		var info AtomicTxInfo
		if err := gob.NewDecoder(bytes.NewReader(val)).Decode(&info); err == nil {
			return &info, nil
		}
	}

	// Cache miss - fetch from RPC
	info, err := c.fetchAtomicTxInfo(ctx, sourceChainID, txID)
	if err != nil {
		return nil, err
	}

	// Cache forever
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(info); err == nil {
		c.cache.Set(key, buf.Bytes(), pebble.NoSync)
	}

	return info, nil
}

// fetchAtomicTxInfo fetches atomic tx block info from the source chain.
// Only C-Chain is supported. X-Chain will return an error.
func (c *CachedClient) fetchAtomicTxInfo(ctx context.Context, sourceChainID, txID string) (*AtomicTxInfo, error) {
	// C-Chain IDs (mainnet and fuji)
	cChainMainnet := "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5"
	cChainFuji := "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp"

	if sourceChainID != cChainMainnet && sourceChainID != cChainFuji {
		// X-Chain or unknown chain - log a notice
		return nil, fmt.Errorf("NOTICE: unsupported source chain %s for atomic tx %s (only C-Chain supported)", sourceChainID, txID)
	}

	// Step 1: Get block height from avax.getAtomicTxStatus
	var statusResult struct {
		Status      string `json:"status"`
		BlockHeight string `json:"blockHeight"`
	}

	statusParams := map[string]any{"txID": txID}
	if err := c.rpcCallChain(ctx, sourceChainID, "avax.getAtomicTxStatus", statusParams, &statusResult); err != nil {
		return nil, fmt.Errorf("avax.getAtomicTxStatus failed: %w", err)
	}

	blockHeight, err := strconv.ParseUint(statusResult.BlockHeight, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid blockHeight %q: %w", statusResult.BlockHeight, err)
	}

	// Step 2: Get timestamp from eth_getBlockByNumber
	blockHex := fmt.Sprintf("0x%x", blockHeight)
	var blockResult struct {
		Timestamp string `json:"timestamp"`
	}

	blockParams := []any{blockHex, false}
	if err := c.rpcCallChainEVM(ctx, sourceChainID, "eth_getBlockByNumber", blockParams, &blockResult); err != nil {
		return nil, fmt.Errorf("eth_getBlockByNumber failed: %w", err)
	}

	timestamp, err := strconv.ParseInt(blockResult.Timestamp, 0, 64) // handles 0x prefix
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp %q: %w", blockResult.Timestamp, err)
	}

	return &AtomicTxInfo{
		BlockHeight: blockHeight,
		Timestamp:   timestamp,
	}, nil
}
