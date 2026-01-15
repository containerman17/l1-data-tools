package runner

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/pchain"
	"github.com/cockroachdb/pebble/v2"
)

const (
	pPollInterval = 100 * time.Millisecond
	pBatchSize    = 100000
)

var numParseWorkers = runtime.NumCPU()

var (
	keyBlkPrefix = []byte("blk:")
	keyLatest    = []byte("meta:latest")
)

// Stats for logging
var (
	statBlockRead  atomic.Int64
	statBlockParse atomic.Int64
	statIndexWrite atomic.Int64
)

// PRunner reads P-chain blocks and feeds them to PChainIndexer instances.
// Blocks are stored with pre-fetched reward UTXOs by the fetcher.
type PRunner struct {
	blocksDB  *pebble.DB
	indexers  []indexer.PChainIndexer
	networkID uint32
}

// NewPRunner creates a new P-chain runner.
func NewPRunner(blocksDB *pebble.DB, indexers []indexer.PChainIndexer, networkID uint32) *PRunner {
	return &PRunner{
		blocksDB:  blocksDB,
		indexers:  indexers,
		networkID: networkID,
	}
}

// BlocksDBSetter is an optional interface for indexers that need access to the shared blocks database.
type BlocksDBSetter interface {
	SetBlocksDB(db *pebble.DB)
}

// Init initializes all indexers with dedicated directories.
func (r *PRunner) Init(ctx context.Context, baseDir string) error {
	for _, idx := range r.indexers {
		if err := idx.Init(ctx, baseDir+"/"+idx.Name(), r.networkID); err != nil {
			return fmt.Errorf("init %s: %w", idx.Name(), err)
		}
		// Pass blocksDB to indexers that need it
		if setter, ok := idx.(BlocksDBSetter); ok {
			setter.SetBlocksDB(r.blocksDB)
		}
	}
	return nil
}

// Run processes blocks continuously.
func (r *PRunner) Run(ctx context.Context) error {
	ticker := time.NewTicker(pPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.processNewBlocks(ctx); err != nil {
				return fmt.Errorf("[p-runner] fatal: %w", err)
			}
		}
	}
}

func (r *PRunner) processNewBlocks(ctx context.Context) error {
	latestFetched := r.getLatestFetched()
	if latestFetched == 0 {
		return nil
	}

	// Determine the global minimum processed height across all indexers
	minProcessed := latestFetched
	indexerHeights := make(map[string]uint64)

	for _, idx := range r.indexers {
		h, err := idx.GetPChainWatermark()
		if err != nil {
			return fmt.Errorf("%s watermark: %w", idx.Name(), err)
		}
		indexerHeights[idx.Name()] = h
		if h < minProcessed {
			minProcessed = h
		}
	}

	if minProcessed >= latestFetched {
		return nil // All caught up
	}

	startHeight := minProcessed + 1
	runStart := time.Now()
	totalProcessed := uint64(0)
	lastLogBlocks := uint64(0)
	lastLogTime := time.Now()

	// Reset stats
	statBlockRead.Store(0)
	statBlockParse.Store(0)
	statIndexWrite.Store(0)

	// Process in batches
	for batchStart := startHeight; batchStart <= latestFetched; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		batchEnd := batchStart + pBatchSize - 1
		if batchEnd > latestFetched {
			batchEnd = latestFetched
		}

		// 1. Read blocks
		readStart := time.Now()
		blockBytes, err := r.readBlockBytes(batchStart, batchEnd)
		if err != nil {
			return err
		}
		statBlockRead.Add(time.Since(readStart).Microseconds())

		// 2. Parse blocks (rewards are already embedded in stored data)
		parseStart := time.Now()
		parsedBlocks, err := r.parseBlocksParallel(ctx, blockBytes)
		if err != nil {
			return err
		}
		statBlockParse.Add(time.Since(parseStart).Microseconds())

		// 3. Feed batch to each indexer that needs it
		writeStart := time.Now()
		for _, idx := range r.indexers {
			idxHeight := indexerHeights[idx.Name()]

			// Filter blocks this indexer needs
			var blocksForIdx []indexer.PBlock
			for _, blk := range parsedBlocks {
				if blk.Height > idxHeight {
					blocksForIdx = append(blocksForIdx, blk)
				}
			}

			if len(blocksForIdx) > 0 {
				if err := idx.ProcessPChainBatch(ctx, blocksForIdx); err != nil {
					return fmt.Errorf("%s process batch: %w", idx.Name(), err)
				}
				indexerHeights[idx.Name()] = blocksForIdx[len(blocksForIdx)-1].Height
			}
		}
		statIndexWrite.Add(time.Since(writeStart).Microseconds())

		processed := batchEnd - batchStart + 1
		totalProcessed += processed
		batchStart = batchEnd + 1

		// Log progress every 10k blocks or 5 seconds
		if totalProcessed-lastLogBlocks >= 10000 || time.Since(lastLogTime) > 5*time.Second {
			r.logProgress(batchEnd, totalProcessed, runStart, latestFetched)
			lastLogBlocks = totalProcessed
			lastLogTime = time.Now()

			statBlockRead.Store(0)
			statBlockParse.Store(0)
			statIndexWrite.Store(0)
		}
	}

	if totalProcessed > 0 {
		elapsed := time.Since(runStart).Seconds()
		if elapsed > 1 {
			log.Printf("[p-runner] indexed %d blocks in %.1fs (%.0f blk/s)", totalProcessed, elapsed, float64(totalProcessed)/elapsed)
		}
	}

	return nil
}

func (r *PRunner) logProgress(currentBlock, totalProcessed uint64, runStart time.Time, latestFetched uint64) {
	elapsed := time.Since(runStart).Seconds()
	if elapsed == 0 {
		elapsed = 0.001
	}
	avgRate := float64(totalProcessed) / elapsed
	remaining := latestFetched - currentBlock
	etaMin := float64(remaining) / avgRate / 60

	readMs := float64(statBlockRead.Load()) / 1000
	parseMs := float64(statBlockParse.Load()) / 1000
	writeMs := float64(statIndexWrite.Load()) / 1000

	log.Printf("[p-runner] block %d | %.0f blk/s | remaining %d | ETA %.1f min | read=%.0fms parse=%.0fms write=%.0fms",
		currentBlock, avgRate, remaining, etaMin, readMs, parseMs, writeMs)
}

// GetGlobalWatermark returns the minimum height processed by all indexers.
func (r *PRunner) GetGlobalWatermark() (uint64, error) {
	minH := uint64(0)
	first := true
	for _, idx := range r.indexers {
		h, err := idx.GetPChainWatermark()
		if err != nil {
			return 0, fmt.Errorf("%s watermark: %w", idx.Name(), err)
		}
		if first || h < minH {
			minH = h
			first = false
		}
	}
	if first {
		return 0, nil
	}
	return minH, nil
}

func (r *PRunner) getLatestFetched() uint64 {
	val, closer, err := r.blocksDB.Get(keyLatest)
	if err != nil {
		return 0
	}
	defer closer.Close()
	return decodeHeight(val)
}

func decodeHeight(b []byte) uint64 {
	if len(b) < 8 {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

// blockData holds raw block bytes and height for parsing.
type blockData struct {
	height uint64
	bytes  []byte
}

func (r *PRunner) readBlockBytes(start, end uint64) ([]blockData, error) {
	startKey := make([]byte, len(keyBlkPrefix)+8)
	copy(startKey, keyBlkPrefix)
	binary.BigEndian.PutUint64(startKey[len(keyBlkPrefix):], start)

	endKey := make([]byte, len(keyBlkPrefix)+8)
	copy(endKey, keyBlkPrefix)
	binary.BigEndian.PutUint64(endKey[len(keyBlkPrefix):], end+1)

	iter, err := r.blocksDB.NewIter(&pebble.IterOptions{
		LowerBound: startKey,
		UpperBound: endKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create iterator: %w", err)
	}
	defer iter.Close()

	blocks := make([]blockData, 0, end-start+1)
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		height := binary.BigEndian.Uint64(key[len(keyBlkPrefix):])

		bytes := make([]byte, len(iter.Value()))
		copy(bytes, iter.Value())

		blocks = append(blocks, blockData{height: height, bytes: bytes})
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return blocks, nil
}

func (r *PRunner) parseBlocksParallel(ctx context.Context, blocks []blockData) ([]indexer.PBlock, error) {
	n := len(blocks)
	if n == 0 {
		return nil, nil
	}

	results := make([]indexer.PBlock, n)

	var wg sync.WaitGroup
	workers := numParseWorkers
	if workers > n {
		workers = n
	}

	chunkSize := (n + workers - 1) / workers
	var parseErr error
	var errOnce sync.Once

	for w := 0; w < workers; w++ {
		start := w * chunkSize
		end := start + chunkSize
		if end > n {
			end = n
		}
		if start >= n {
			break
		}

		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			for i := start; i < end; i++ {
				select {
				case <-ctx.Done():
					errOnce.Do(func() { parseErr = ctx.Err() })
					return
				default:
				}

				bd := blocks[i]

				// Decode stored format: block bytes + reward UTXOs
				blockBytes, rewardUTXOBytes := pchain.DecodeBlockWithRewards(bd.bytes)

				blk, err := block.Parse(block.Codec, blockBytes)
				if err != nil {
					errOnce.Do(func() { parseErr = fmt.Errorf("parse block %d: %w", bd.height, err) })
					return
				}

				// Extract timestamp if available (Banff blocks and later)
				var timestamp int64
				if banff, ok := blk.(block.BanffBlock); ok {
					timestamp = banff.Timestamp().Unix()
				}

				// Build result with reward UTXOs
				pblk := indexer.PBlock{
					Height:      bd.height,
					Timestamp:   timestamp,
					Block:       blk,
					RewardUTXOs: make(map[ids.ID][]avax.UTXO),
				}

				// If this is a RewardValidatorTx, parse and attach the reward UTXOs
				if len(blk.Txs()) > 0 && len(rewardUTXOBytes) > 0 {
					if rewardTx, ok := blk.Txs()[0].Unsigned.(*txs.RewardValidatorTx); ok {
						rewards := make([]avax.UTXO, 0, len(rewardUTXOBytes))
						for _, raw := range rewardUTXOBytes {
							var utxo avax.UTXO
							if _, err := txs.Codec.Unmarshal(raw, &utxo); err != nil {
								errOnce.Do(func() {
									parseErr = fmt.Errorf("parse reward UTXO at height %d: %w", bd.height, err)
								})
								return
							}
							rewards = append(rewards, utxo)
						}
						pblk.RewardUTXOs[rewardTx.TxID] = rewards
					}
				}

				results[i] = pblk
			}
		}(start, end)
	}

	wg.Wait()

	if parseErr != nil {
		return nil, parseErr
	}

	return results, nil
}
