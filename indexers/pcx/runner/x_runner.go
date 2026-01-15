package runner

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
	"github.com/cockroachdb/pebble/v2"
)

// Stats for X-chain timing (exported so indexers can update parse time)
var (
	xStatRead  atomic.Int64
	XStatParse atomic.Int64 // Exported for indexer use
	xStatWrite atomic.Int64
)

const (
	xPollInterval = 100 * time.Millisecond
	xBatchSize    = 50000
)

var (
	xKeyBlkPrefix       = []byte("blk:")
	xKeyTxPrefix        = []byte("tx:")
	xKeyLatest          = []byte("meta:latest")
	xKeyPreCortinaDone  = []byte("meta:preCortinaDone")
	xKeyPreCortinaIndex = []byte("meta:preCortinaTxIndex")
)

// XRunner reads X-chain blocks/transactions and feeds them to XChainIndexer instances.
type XRunner struct {
	blocksDB  *pebble.DB
	indexers  []indexer.XChainIndexer
	networkID uint32
}

// NewXRunner creates a new X-chain runner.
func NewXRunner(blocksDB *pebble.DB, indexers []indexer.XChainIndexer, networkID uint32) *XRunner {
	return &XRunner{
		blocksDB:  blocksDB,
		indexers:  indexers,
		networkID: networkID,
	}
}

// Init initializes all indexers.
func (r *XRunner) Init(ctx context.Context, baseDir string) error {
	for _, idx := range r.indexers {
		if err := idx.Init(ctx, baseDir+"/"+idx.Name(), r.networkID); err != nil {
			return fmt.Errorf("init %s: %w", idx.Name(), err)
		}
	}
	return nil
}

// RunPreCortina processes pre-Cortina transactions (sequential index from Index API).
func (r *XRunner) RunPreCortina(ctx context.Context) error {
	// Check if pre-Cortina data exists
	if !r.isPreCortinaDone() {
		log.Printf("[x-runner] pre-Cortina data not yet fetched, skipping")
		return nil
	}

	totalTxs := r.latestPreCortinaIndex()
	if totalTxs == 0 {
		return nil
	}

	log.Printf("[x-runner] processing %d pre-Cortina transactions...", totalTxs)

	// Determine starting point (minimum across all indexers)
	minProcessed := totalTxs
	indexerWatermarks := make(map[string]uint64)

	for _, idx := range r.indexers {
		h, err := idx.GetXChainPreCortinaWatermark()
		if err != nil {
			return fmt.Errorf("%s pre-cortina watermark: %w", idx.Name(), err)
		}
		indexerWatermarks[idx.Name()] = h
		if h < minProcessed {
			minProcessed = h
		}
	}

	if minProcessed >= totalTxs {
		log.Printf("[x-runner] pre-Cortina already processed")
		return nil
	}

	startIndex := minProcessed
	if minProcessed > 0 {
		startIndex = minProcessed + 1 // Start after last processed
	}

	runStart := time.Now()
	totalProcessed := uint64(0)
	lastLogTxs := uint64(0)
	lastLogTime := time.Now()

	// Reset stats
	xStatRead.Store(0)
	XStatParse.Store(0)
	xStatWrite.Store(0)

	for batchStart := startIndex; batchStart < totalTxs; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		batchEnd := batchStart + xBatchSize
		if batchEnd > totalTxs {
			batchEnd = totalTxs
		}

		// Read transactions
		readStart := time.Now()
		txs, err := r.readTxBytes(batchStart, batchEnd)
		if err != nil {
			return err
		}
		xStatRead.Add(time.Since(readStart).Microseconds())

		// Feed to each indexer (parse + write happens inside)
		writeStart := time.Now()
		for _, idx := range r.indexers {
			idxWm := indexerWatermarks[idx.Name()]

			// Filter txs this indexer needs
			var txsForIdx []indexer.XTx
			for _, tx := range txs {
				if tx.Index > idxWm {
					txsForIdx = append(txsForIdx, tx)
				}
			}

			if len(txsForIdx) > 0 {
				if err := idx.ProcessXChainPreCortinaTxs(ctx, txsForIdx); err != nil {
					return fmt.Errorf("%s process pre-cortina txs: %w", idx.Name(), err)
				}
				indexerWatermarks[idx.Name()] = txsForIdx[len(txsForIdx)-1].Index
			}
		}
		xStatWrite.Add(time.Since(writeStart).Microseconds())

		processed := batchEnd - batchStart
		totalProcessed += processed
		batchStart = batchEnd

		// Log progress every 50k txs or 5 seconds
		if totalProcessed-lastLogTxs >= 50000 || time.Since(lastLogTime) > 5*time.Second {
			elapsed := time.Since(runStart).Seconds()
			if elapsed > 0 {
				rate := float64(totalProcessed) / elapsed
				remaining := totalTxs - batchStart

				readMs := float64(xStatRead.Load()) / 1000
				parseMs := float64(XStatParse.Load()) / 1000
				writeMs := float64(xStatWrite.Load()) / 1000

				log.Printf("[x-runner] pre-Cortina tx %d/%d | %.0f tx/s | remaining %d | read=%.0fms parse=%.0fms write=%.0fms",
					batchStart, totalTxs, rate, remaining, readMs, parseMs, writeMs)

				// Reset stats for next window
				xStatRead.Store(0)
				XStatParse.Store(0)
				xStatWrite.Store(0)
				lastLogTxs = totalProcessed
				lastLogTime = time.Now()
			}
		}
	}

	if totalProcessed > 0 {
		log.Printf("[x-runner] pre-Cortina complete: %d transactions", totalProcessed)
	}

	return nil
}

// RunBlocks processes post-Cortina X-chain blocks continuously.
func (r *XRunner) RunBlocks(ctx context.Context) error {
	ticker := time.NewTicker(xPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.processNewBlocks(ctx); err != nil {
				return fmt.Errorf("[x-runner] fatal: %w", err)
			}
		}
	}
}

func (r *XRunner) processNewBlocks(ctx context.Context) error {
	latestFetched := r.getLatestFetched()
	if latestFetched == 0 {
		return nil
	}

	// Determine minimum processed height
	minProcessed := latestFetched
	indexerHeights := make(map[string]uint64)

	for _, idx := range r.indexers {
		h, err := idx.GetXChainBlockWatermark()
		if err != nil {
			return fmt.Errorf("%s block watermark: %w", idx.Name(), err)
		}
		indexerHeights[idx.Name()] = h
		if h < minProcessed {
			minProcessed = h
		}
	}

	if minProcessed >= latestFetched {
		return nil
	}

	startHeight := minProcessed + 1
	runStart := time.Now()
	totalProcessed := uint64(0)

	for batchStart := startHeight; batchStart <= latestFetched; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		batchEnd := batchStart + xBatchSize - 1
		if batchEnd > latestFetched {
			batchEnd = latestFetched
		}

		// Read blocks
		blocks, err := r.readBlockBytes(batchStart, batchEnd)
		if err != nil {
			return err
		}

		// Feed to each indexer
		for _, idx := range r.indexers {
			idxHeight := indexerHeights[idx.Name()]

			var blocksForIdx []indexer.XBlock
			for _, blk := range blocks {
				if blk.Height > idxHeight {
					blocksForIdx = append(blocksForIdx, blk)
				}
			}

			if len(blocksForIdx) > 0 {
				if err := idx.ProcessXChainBlocks(ctx, blocksForIdx); err != nil {
					return fmt.Errorf("%s process blocks: %w", idx.Name(), err)
				}
				indexerHeights[idx.Name()] = blocksForIdx[len(blocksForIdx)-1].Height
			}
		}

		processed := batchEnd - batchStart + 1
		totalProcessed += processed
		batchStart = batchEnd + 1
	}

	if totalProcessed > 0 {
		elapsed := time.Since(runStart).Seconds()
		if elapsed > 1 {
			log.Printf("[x-runner] indexed %d blocks in %.1fs", totalProcessed, elapsed)
		}
	}

	return nil
}

func (r *XRunner) isPreCortinaDone() bool {
	_, closer, err := r.blocksDB.Get(xKeyPreCortinaDone)
	if err != nil {
		return false
	}
	closer.Close()
	return true
}

func (r *XRunner) latestPreCortinaIndex() uint64 {
	val, closer, err := r.blocksDB.Get(xKeyPreCortinaIndex)
	if err != nil {
		return 0
	}
	defer closer.Close()
	return decodeHeight(val)
}

func (r *XRunner) getLatestFetched() uint64 {
	val, closer, err := r.blocksDB.Get(xKeyLatest)
	if err != nil {
		return 0
	}
	defer closer.Close()
	return decodeHeight(val)
}

func (r *XRunner) readTxBytes(start, end uint64) ([]indexer.XTx, error) {
	startKey := make([]byte, len(xKeyTxPrefix)+8)
	copy(startKey, xKeyTxPrefix)
	binary.BigEndian.PutUint64(startKey[len(xKeyTxPrefix):], start)

	endKey := make([]byte, len(xKeyTxPrefix)+8)
	copy(endKey, xKeyTxPrefix)
	binary.BigEndian.PutUint64(endKey[len(xKeyTxPrefix):], end)

	iter, err := r.blocksDB.NewIter(&pebble.IterOptions{
		LowerBound: startKey,
		UpperBound: endKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create tx iterator: %w", err)
	}
	defer iter.Close()

	var txs []indexer.XTx
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		index := binary.BigEndian.Uint64(key[len(xKeyTxPrefix):])

		val := iter.Value()
		if len(val) < 8 {
			continue // invalid entry
		}

		// Stored format: [8 bytes timestamp][tx bytes]
		timestamp := int64(binary.BigEndian.Uint64(val[:8]))
		txBytes := make([]byte, len(val)-8)
		copy(txBytes, val[8:])

		txs = append(txs, indexer.XTx{Index: index, Timestamp: timestamp, Bytes: txBytes})
	}

	return txs, iter.Error()
}

func (r *XRunner) readBlockBytes(start, end uint64) ([]indexer.XBlock, error) {
	startKey := make([]byte, len(xKeyBlkPrefix)+8)
	copy(startKey, xKeyBlkPrefix)
	binary.BigEndian.PutUint64(startKey[len(xKeyBlkPrefix):], start)

	endKey := make([]byte, len(xKeyBlkPrefix)+8)
	copy(endKey, xKeyBlkPrefix)
	binary.BigEndian.PutUint64(endKey[len(xKeyBlkPrefix):], end+1)

	iter, err := r.blocksDB.NewIter(&pebble.IterOptions{
		LowerBound: startKey,
		UpperBound: endKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create block iterator: %w", err)
	}
	defer iter.Close()

	var blocks []indexer.XBlock
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		height := binary.BigEndian.Uint64(key[len(xKeyBlkPrefix):])

		bytes := make([]byte, len(iter.Value()))
		copy(bytes, iter.Value())

		blocks = append(blocks, indexer.XBlock{Height: height, Bytes: bytes})
	}

	return blocks, iter.Error()
}

// GetGlobalBlockWatermark returns the minimum block height processed by all indexers.
func (r *XRunner) GetGlobalBlockWatermark() (uint64, error) {
	minH := uint64(0)
	first := true
	for _, idx := range r.indexers {
		h, err := idx.GetXChainBlockWatermark()
		if err != nil {
			return 0, fmt.Errorf("%s block watermark: %w", idx.Name(), err)
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
