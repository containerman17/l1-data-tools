package runner

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"time"

	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/cchain"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
	"github.com/cockroachdb/pebble/v2"
)

const (
	cPollInterval = 100 * time.Millisecond
	cBatchSize    = 1000000
)

var (
	cKeyBlkPrefix = []byte("blk:")
	cKeyLatest    = []byte("meta:latest")
)

// CRunner reads C-chain blocks and feeds them to CChainIndexer instances.
type CRunner struct {
	blocksDB  *pebble.DB
	indexers  []indexer.CChainIndexer
	networkID uint32
}

// NewCRunner creates a new C-chain runner.
func NewCRunner(blocksDB *pebble.DB, indexers []indexer.CChainIndexer, networkID uint32) *CRunner {
	return &CRunner{
		blocksDB:  blocksDB,
		indexers:  indexers,
		networkID: networkID,
	}
}

// Init initializes all indexers.
func (r *CRunner) Init(ctx context.Context, baseDir string) error {
	for _, idx := range r.indexers {
		if err := idx.Init(ctx, baseDir+"/"+idx.Name(), r.networkID); err != nil {
			return fmt.Errorf("init %s: %w", idx.Name(), err)
		}
	}
	return nil
}

// Run processes blocks continuously.
func (r *CRunner) Run(ctx context.Context) error {
	ticker := time.NewTicker(cPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.processNewBlocks(ctx); err != nil {
				return fmt.Errorf("[c-runner] fatal: %w", err)
			}
		}
	}
}

func (r *CRunner) processNewBlocks(ctx context.Context) error {
	latestFetched := r.getLatestFetched()
	if latestFetched == 0 {
		return nil
	}

	// Determine minimum processed height
	minProcessed := latestFetched
	indexerHeights := make(map[string]uint64)

	for _, idx := range r.indexers {
		h, err := idx.GetCChainWatermark()
		if err != nil {
			return fmt.Errorf("%s watermark: %w", idx.Name(), err)
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

		batchEnd := batchStart + cBatchSize - 1
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

			var blocksForIdx []indexer.CBlock
			for _, blk := range blocks {
				if blk.Height > idxHeight {
					blocksForIdx = append(blocksForIdx, blk)
				}
			}

			if len(blocksForIdx) > 0 {
				if err := idx.ProcessCChainBatch(ctx, blocksForIdx); err != nil {
					return fmt.Errorf("%s process batch: %w", idx.Name(), err)
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
			log.Printf("[c-runner] indexed %d blocks in %.1fs", totalProcessed, elapsed)
		}
	}

	return nil
}

func (r *CRunner) getLatestFetched() uint64 {
	val, closer, err := r.blocksDB.Get(cKeyLatest)
	if err != nil {
		return 0
	}
	defer closer.Close()
	return decodeHeight(val)
}

func (r *CRunner) readBlockBytes(start, end uint64) ([]indexer.CBlock, error) {
	startKey := make([]byte, len(cKeyBlkPrefix)+8)
	copy(startKey, cKeyBlkPrefix)
	binary.BigEndian.PutUint64(startKey[len(cKeyBlkPrefix):], start)

	endKey := make([]byte, len(cKeyBlkPrefix)+8)
	copy(endKey, cKeyBlkPrefix)
	binary.BigEndian.PutUint64(endKey[len(cKeyBlkPrefix):], end+1)

	iter, err := r.blocksDB.NewIter(&pebble.IterOptions{
		LowerBound: startKey,
		UpperBound: endKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create iterator: %w", err)
	}
	defer iter.Close()

	var blocks []indexer.CBlock
	for iter.First(); iter.Valid(); iter.Next() {
		// Decode binary-encoded block data
		var stored cchain.BlockData
		if err := stored.Decode(iter.Value()); err != nil {
			key := iter.Key()
			height := binary.BigEndian.Uint64(key[len(cKeyBlkPrefix):])
			return nil, fmt.Errorf("decode C-Chain block %d: %w (delete data/*/blocks/c and re-sync)", height, err)
		}

		blocks = append(blocks, indexer.CBlock{
			Height:        stored.Height,
			Timestamp:     stored.Timestamp,
			Hash:          stored.Hash,
			ParentHash:    stored.ParentHash,
			Size:          stored.Size,
			TxCount:       stored.TxCount,
			GasLimit:      stored.GasLimit,
			GasUsed:       stored.GasUsed,
			BaseFeePerGas: stored.BaseFeePerGas,
			Miner:         stored.Miner,
			ExtraData:     stored.ExtraData,
			ExtDataHash:   stored.ExtDataHash,
		})
	}

	return blocks, iter.Error()
}

// GetGlobalWatermark returns the minimum height processed by all indexers.
func (r *CRunner) GetGlobalWatermark() (uint64, error) {
	minH := uint64(0)
	first := true
	for _, idx := range r.indexers {
		h, err := idx.GetCChainWatermark()
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
