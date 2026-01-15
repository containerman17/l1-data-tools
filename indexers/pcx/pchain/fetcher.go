package pchain

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/cockroachdb/pebble/v2"
)

func getBatchSize() int {
	if s := os.Getenv("BATCH_SIZE"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 10 // default
}

func getPollInterval() time.Duration {
	if s := os.Getenv("POLL_INTERVAL_MS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return time.Second // default
}

var (
	keyLatest = []byte("meta:latest")
	keyPrefix = []byte("blk:")
)

// Fetcher polls the P-Chain for new blocks and stores raw bytes.
type Fetcher struct {
	rpc         *Client
	db          *pebble.DB
	chainHeight uint64 // cached chain height

	// Stats
	startTime   time.Time
	startHeight uint64
}

// NewFetcher creates a new P-Chain block fetcher.
func NewFetcher(rpc *Client, db *pebble.DB) *Fetcher {
	return &Fetcher{rpc: rpc, db: db}
}

// Run fetches blocks in batches until context is cancelled.
func (f *Fetcher) Run(ctx context.Context) error {
	f.startTime = time.Now()
	f.startHeight = f.LatestFetched()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fetched, err := f.fetchBatch(ctx)
		if err != nil {
			log.Printf("[p-fetcher] error: %v", err)
			time.Sleep(time.Second)
			continue
		}

		// If we fetched nothing, we're caught up - poll slower
		if fetched == 0 {
			time.Sleep(getPollInterval())
		}
	}
}

// fetchBatch fetches one batch of blocks.
func (f *Fetcher) fetchBatch(ctx context.Context) (int, error) {
	localHeight := f.LatestFetched()
	batchSize := uint64(getBatchSize())

	// Only refresh chain height if we're close to it (within batchSize)
	if f.chainHeight == 0 || localHeight+batchSize >= f.chainHeight {
		height, err := f.rpc.GetHeight(ctx)
		if err != nil {
			return 0, err
		}
		f.chainHeight = height
	}

	if localHeight >= f.chainHeight {
		return 0, nil // up to date
	}

	// Calculate batch range
	batchStart := localHeight + 1
	batchEnd := batchStart + batchSize - 1
	if batchEnd > f.chainHeight {
		batchEnd = f.chainHeight
	}

	// Fetch batch in parallel
	count := int(batchEnd - batchStart + 1)
	blocks := make([][]byte, count)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var fetchErr error

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			height := batchStart + uint64(idx)
			blockBytes, err := f.rpc.GetBlockByHeight(ctx, height)
			if err != nil {
				mu.Lock()
				if fetchErr == nil {
					fetchErr = err
				}
				mu.Unlock()
				return
			}

			// Parse block to check for RewardValidatorTx
			var rewardUTXOs [][]byte
			blk, parseErr := block.Parse(block.Codec, blockBytes)
			if parseErr != nil {
				mu.Lock()
				if fetchErr == nil {
					fetchErr = fmt.Errorf("parse block %d: %w", height, parseErr)
				}
				mu.Unlock()
				return
			}

			// Check if block contains RewardValidatorTx
			if len(blk.Txs()) > 0 {
				if rewardTx, ok := blk.Txs()[0].Unsigned.(*txs.RewardValidatorTx); ok {
					// Fetch reward UTXOs with retries
					const maxRetries = 3
					const retryDelay = 2 * time.Second
					var fetchRewardsErr error

					for attempt := 0; attempt < maxRetries; attempt++ {
						if attempt > 0 {
							log.Printf("[p-fetcher] Retry %d/%d GetRewardUTXOs for height %d", attempt, maxRetries, height)
							time.Sleep(retryDelay)
						}
						rewardUTXOs, fetchRewardsErr = f.rpc.GetRewardUTXOs(ctx, rewardTx.TxID.String())
						if fetchRewardsErr == nil {
							break
						}
					}
					if fetchRewardsErr != nil {
						mu.Lock()
						if fetchErr == nil {
							fetchErr = fmt.Errorf("GetRewardUTXOs for height %d (staking tx %s): %w", height, rewardTx.TxID, fetchRewardsErr)
						}
						mu.Unlock()
						return
					}
				}
			}

			// Encode block with rewards
			encoded := EncodeBlockWithRewards(blockBytes, rewardUTXOs)

			mu.Lock()
			blocks[idx] = encoded
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	if fetchErr != nil {
		return 0, fetchErr
	}

	// Store all blocks in one batch
	batch := f.db.NewBatch()
	defer batch.Close()

	for i, encodedBlock := range blocks {
		height := batchStart + uint64(i)
		if err := batch.Set(blockKey(height), encodedBlock, pebble.NoSync); err != nil {
			return 0, err
		}
	}

	// Update latest
	if err := batch.Set(keyLatest, encodeHeight(batchEnd), pebble.NoSync); err != nil {
		return 0, err
	}

	if err := batch.Commit(pebble.NoSync); err != nil {
		return 0, err
	}

	// Calculate stats
	elapsed := time.Since(f.startTime).Seconds()
	totalFetched := batchEnd - f.startHeight
	blocksPerSec := float64(totalFetched) / elapsed
	remaining := f.chainHeight - batchEnd
	etaSec := float64(remaining) / blocksPerSec

	log.Printf("[p-fetcher] %d -> %d | %.0f blk/s | remaining %d | ETA %s",
		batchStart, batchEnd, blocksPerSec, remaining, formatDuration(etaSec))

	return count, nil
}

func formatDuration(seconds float64) string {
	if seconds < 60 {
		return fmt.Sprintf("%.0fs", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%.0fm", seconds/60)
	}
	hours := seconds / 3600
	if hours < 24 {
		return fmt.Sprintf("%.1fh", hours)
	}
	return fmt.Sprintf("%.1fd", hours/24)
}

// LatestFetched returns the latest fetched block height, or 0 if none.
func (f *Fetcher) LatestFetched() uint64 {
	val, closer, err := f.db.Get(keyLatest)
	if err != nil {
		return 0
	}
	defer closer.Close()
	return decodeHeight(val)
}

// GetBlock returns raw block bytes for a given height.
func (f *Fetcher) GetBlock(height uint64) ([]byte, error) {
	val, closer, err := f.db.Get(blockKey(height))
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	result := make([]byte, len(val))
	copy(result, val)
	return result, nil
}

func blockKey(height uint64) []byte {
	key := make([]byte, len(keyPrefix)+8)
	copy(key, keyPrefix)
	binary.BigEndian.PutUint64(key[len(keyPrefix):], height)
	return key
}

func encodeHeight(height uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, height)
	return buf
}

func decodeHeight(b []byte) uint64 {
	if len(b) < 8 {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

// EncodeBlockWithRewards encodes block bytes and reward UTXOs in a single value.
// Format: [4 bytes: block length][block bytes][2 bytes: reward count][for each: 2 bytes length + utxo bytes]
func EncodeBlockWithRewards(blockBytes []byte, rewardUTXOs [][]byte) []byte {
	// Calculate total size
	size := 4 + len(blockBytes) + 2
	for _, utxo := range rewardUTXOs {
		size += 2 + len(utxo)
	}

	buf := make([]byte, size)
	offset := 0

	// Block length + bytes
	binary.BigEndian.PutUint32(buf[offset:], uint32(len(blockBytes)))
	offset += 4
	copy(buf[offset:], blockBytes)
	offset += len(blockBytes)

	// Reward count
	binary.BigEndian.PutUint16(buf[offset:], uint16(len(rewardUTXOs)))
	offset += 2

	// Each reward UTXO
	for _, utxo := range rewardUTXOs {
		binary.BigEndian.PutUint16(buf[offset:], uint16(len(utxo)))
		offset += 2
		copy(buf[offset:], utxo)
		offset += len(utxo)
	}

	return buf
}

// DecodeBlockWithRewards decodes block bytes and reward UTXOs from a stored value.
func DecodeBlockWithRewards(data []byte) (blockBytes []byte, rewardUTXOs [][]byte) {
	if len(data) < 6 {
		return data, nil // Fallback for old format (shouldn't happen with no backwards compat)
	}

	blockLen := binary.BigEndian.Uint32(data[0:4])
	if int(blockLen) > len(data)-6 {
		return data, nil // Invalid, return as-is
	}

	blockBytes = data[4 : 4+blockLen]
	pos := 4 + int(blockLen)

	count := binary.BigEndian.Uint16(data[pos:])
	pos += 2

	rewardUTXOs = make([][]byte, 0, count)
	for i := 0; i < int(count); i++ {
		if pos+2 > len(data) {
			break
		}
		utxoLen := binary.BigEndian.Uint16(data[pos:])
		pos += 2
		if pos+int(utxoLen) > len(data) {
			break
		}
		rewardUTXOs = append(rewardUTXOs, data[pos:pos+int(utxoLen)])
		pos += int(utxoLen)
	}

	return blockBytes, rewardUTXOs
}
