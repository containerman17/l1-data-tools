package cchain

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/cockroachdb/pebble/v2"
)

func getBatchSize() int {
	if s := os.Getenv("C_BATCH_SIZE"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 1000 // default: 1000 blocks per batch request
}

func getParallelBatches() int {
	if s := os.Getenv("C_PARALLEL"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 10 // default: 10 parallel batch requests
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

// Fetcher polls the C-Chain for new blocks and stores blockExtraData.
type Fetcher struct {
	rpc         *Client
	db          *pebble.DB
	chainHeight uint64 // cached chain height

	// Stats
	startTime   time.Time
	startHeight uint64
	nonEmpty    uint64 // count of blocks with atomic txs
}

// NewFetcher creates a new C-Chain block fetcher.
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
			log.Printf("[c-fetcher] error: %v", err)
			time.Sleep(time.Second)
			continue
		}

		// If we fetched nothing, we're caught up - poll slower
		if fetched == 0 {
			time.Sleep(getPollInterval())
		}
	}
}

// fetchBatch fetches multiple batches in parallel using batch JSON-RPC.
func (f *Fetcher) fetchBatch(ctx context.Context) (int, error) {
	localHeight := f.LatestFetched()
	batchSize := uint64(getBatchSize())
	parallelBatches := uint64(getParallelBatches())
	totalSize := batchSize * parallelBatches

	// Only refresh chain height if we're close to it
	if f.chainHeight == 0 || localHeight+totalSize >= f.chainHeight {
		height, err := f.rpc.GetHeight(ctx)
		if err != nil {
			return 0, err
		}
		f.chainHeight = height
	}

	if localHeight >= f.chainHeight {
		return 0, nil // up to date
	}

	// Calculate total range
	rangeStart := localHeight + 1
	rangeEnd := rangeStart + totalSize - 1
	if rangeEnd > f.chainHeight {
		rangeEnd = f.chainHeight
	}

	// Split into parallel batches
	totalBlocks := rangeEnd - rangeStart + 1
	numBatches := int((totalBlocks + batchSize - 1) / batchSize)

	// Fetch batches in parallel
	results := make([][]BlockData, numBatches)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var fetchErr error

	for i := 0; i < numBatches; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			start := rangeStart + uint64(idx)*batchSize
			end := start + batchSize - 1
			if end > rangeEnd {
				end = rangeEnd
			}

			data, err := f.rpc.GetBlocksBatch(ctx, start, end)
			mu.Lock()
			if err != nil && fetchErr == nil {
				fetchErr = err
			} else {
				results[idx] = data
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	if fetchErr != nil {
		return 0, fetchErr
	}

	// Store all blocks in one pebble batch
	batch := f.db.NewBatch()
	defer batch.Close()

	count := 0
	nonEmptyInBatch := 0
	for _, batchData := range results {
		for _, blk := range batchData {
			// Encode block data as binary (fast)
			data := blk.Encode()
			if err := batch.Set(blockKey(blk.Height), data, pebble.NoSync); err != nil {
				return 0, err
			}
			if len(blk.ExtraData) > 0 {
				nonEmptyInBatch++
			}
			count++
		}
	}

	// Update latest
	if err := batch.Set(keyLatest, encodeHeight(rangeEnd), pebble.NoSync); err != nil {
		return 0, err
	}

	if err := batch.Commit(pebble.NoSync); err != nil {
		return 0, err
	}

	f.nonEmpty += uint64(nonEmptyInBatch)

	// Calculate stats
	elapsed := time.Since(f.startTime).Seconds()
	totalFetched := rangeEnd - f.startHeight
	blocksPerSec := float64(totalFetched) / elapsed
	remaining := f.chainHeight - rangeEnd
	etaSec := float64(remaining) / blocksPerSec

	// Only log if there's something meaningful to report
	if remaining > 0 || nonEmptyInBatch > 0 {
		log.Printf("[c-fetcher] %d -> %d | %.0f blk/s | remaining %d | atomic: %d | ETA %s",
			rangeStart, rangeEnd, blocksPerSec, remaining, f.nonEmpty, formatDuration(etaSec))
	}

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

// GetBlock returns block data for a given height.
func (f *Fetcher) GetBlock(height uint64) (*BlockData, error) {
	val, closer, err := f.db.Get(blockKey(height))
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	var blk BlockData
	if err := blk.Decode(val); err != nil {
		return nil, fmt.Errorf("decode block %d: %w", height, err)
	}
	return &blk, nil
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
