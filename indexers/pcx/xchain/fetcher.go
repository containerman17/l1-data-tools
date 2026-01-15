package xchain

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
	if s := os.Getenv("BATCH_SIZE"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 10 // default
}

func getPreCortinaBatchSize() int {
	if s := os.Getenv("X_PRECORTINA_BATCH_SIZE"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 100000 // default: 100k transactions per batch
}

func getPollInterval() time.Duration {
	if s := os.Getenv("POLL_INTERVAL_MS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return time.Second // default
}

// Cortina activation timestamps (Unix seconds)
const (
	cortinaTimeFuji    = 1680793200 // April 6, 2023 15:00 UTC
	cortinaTimeMainnet = 1682434800 // April 25, 2023 15:00 UTC
)

var (
	// Post-Cortina blocks
	keyLatest = []byte("meta:latest")
	keyPrefix = []byte("blk:")

	// Pre-Cortina transactions (from Index API)
	keyPreCortinaDone  = []byte("meta:preCortinaDone")
	keyPreCortinaIndex = []byte("meta:preCortinaTxIndex")
	keyTxPrefix        = []byte("tx:")
)

// Fetcher polls the X-Chain for new blocks and stores raw bytes.
type Fetcher struct {
	rpc         *Client
	db          *pebble.DB
	chainHeight uint64 // cached chain height
	cortinaTime int64  // Cortina activation timestamp for this network
	isMainnet   bool

	// Stats
	startTime   time.Time
	startHeight uint64
}

// NewFetcher creates a new X-Chain block fetcher.
// Set isMainnet=true for mainnet, false for Fuji.
func NewFetcher(rpc *Client, db *pebble.DB, isMainnet bool) *Fetcher {
	cortinaTime := int64(cortinaTimeFuji)
	if isMainnet {
		cortinaTime = cortinaTimeMainnet
	}
	return &Fetcher{
		rpc:         rpc,
		db:          db,
		cortinaTime: cortinaTime,
		isMainnet:   isMainnet,
	}
}

// RunPreCortina fetches all pre-Cortina transactions via Index API.
// This should be called before Run() to ensure complete history.
// Requires the node to have --index-enabled=true.
//
// Note: The Index API only indexes pre-Cortina (vertex-based) transactions.
// Post-Cortina transactions are only in blocks, not in the Index API.
func (f *Fetcher) RunPreCortina(ctx context.Context) error {
	// Check if already done
	if f.isPreCortinaDone() {
		log.Printf("[x-fetcher] pre-Cortina transactions already fetched")
		return nil
	}

	// Get total count first
	totalCount, err := f.rpc.GetLastAcceptedIndex(ctx)
	if err != nil {
		return fmt.Errorf("GetLastAcceptedIndex: %w", err)
	}

	log.Printf("[x-fetcher] fetching %d pre-Cortina transactions via Index API...", totalCount+1)
	startTime := time.Now()
	startIndex := f.latestPreCortinaIndex()
	totalFetched := uint64(0)
	lastLogTime := startTime
	batchSize := getPreCortinaBatchSize()

	for startIndex <= totalCount {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		txs, err := f.rpc.GetContainerRange(ctx, startIndex, batchSize)
		if err != nil {
			return fmt.Errorf("GetContainerRange at %d: %w", startIndex, err)
		}

		if len(txs) == 0 {
			break // no more transactions
		}

		batch := f.db.NewBatch()

		for _, tx := range txs {
			// Store as [8 bytes timestamp][tx bytes]
			val := make([]byte, 8+len(tx.Bytes))
			binary.BigEndian.PutUint64(val[:8], uint64(tx.Timestamp))
			copy(val[8:], tx.Bytes)

			if err := batch.Set(txKey(tx.Index), val, pebble.NoSync); err != nil {
				batch.Close()
				return fmt.Errorf("set tx %d: %w", tx.Index, err)
			}
			startIndex = tx.Index + 1
			totalFetched++
		}

		// Update progress
		if err := batch.Set(keyPreCortinaIndex, encodeHeight(startIndex), pebble.NoSync); err != nil {
			batch.Close()
			return err
		}

		if err := batch.Commit(pebble.NoSync); err != nil {
			batch.Close()
			return err
		}
		batch.Close()

		// Log progress every 10k transactions or every 5 seconds
		now := time.Now()
		if totalFetched%10000 == 0 || now.Sub(lastLogTime) >= 5*time.Second {
			elapsed := time.Since(startTime).Seconds()
			txPerSec := float64(totalFetched) / elapsed
			remaining := totalCount + 1 - startIndex
			log.Printf("[x-fetcher] pre-Cortina: %d/%d txs | %.0f tx/s | remaining %d",
				totalFetched, totalCount+1, txPerSec, remaining)
			lastLogTime = now
		}
	}

	// Mark as done
	if err := f.db.Set(keyPreCortinaDone, []byte("1"), pebble.Sync); err != nil {
		return err
	}

	log.Printf("[x-fetcher] pre-Cortina complete: %d transactions in %s", totalFetched, time.Since(startTime).Round(time.Second))
	return nil
}

func (f *Fetcher) isPreCortinaDone() bool {
	_, closer, err := f.db.Get(keyPreCortinaDone)
	if err != nil {
		return false
	}
	closer.Close()
	return true
}

func (f *Fetcher) latestPreCortinaIndex() uint64 {
	val, closer, err := f.db.Get(keyPreCortinaIndex)
	if err != nil {
		return 0
	}
	defer closer.Close()
	return decodeHeight(val)
}

func txKey(index uint64) []byte {
	key := make([]byte, len(keyTxPrefix)+8)
	copy(key, keyTxPrefix)
	binary.BigEndian.PutUint64(key[len(keyTxPrefix):], index)
	return key
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
			log.Printf("[x-fetcher] error: %v", err)
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

			mu.Lock()
			blocks[idx] = blockBytes
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

	for i, blockBytes := range blocks {
		height := batchStart + uint64(i)
		if err := batch.Set(blockKey(height), blockBytes, pebble.NoSync); err != nil {
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

	log.Printf("[x-fetcher] %d -> %d | %.0f blk/s | remaining %d | ETA %s",
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

// GetTx returns raw pre-Cortina transaction bytes for a given index.
func (f *Fetcher) GetTx(index uint64) ([]byte, error) {
	val, closer, err := f.db.Get(txKey(index))
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	result := make([]byte, len(val))
	copy(result, val)
	return result, nil
}

// PreCortinaCount returns the number of pre-Cortina transactions stored.
func (f *Fetcher) PreCortinaCount() uint64 {
	return f.latestPreCortinaIndex()
}

// IsPreCortinaDone returns true if pre-Cortina transactions have been fetched.
func (f *Fetcher) IsPreCortinaDone() bool {
	return f.isPreCortinaDone()
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
