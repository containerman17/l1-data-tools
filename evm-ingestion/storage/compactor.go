package storage

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/containerman17/l1-data-tools/evm-ingestion/consts"
)

// CompactorLogger allows custom logging (for plugin integration)
type CompactorLogger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// defaultLogger uses standard log package
type defaultLogger struct{}

func (l *defaultLogger) Info(msg string, args ...any) {
	log.Printf("[Compactor] %s %v", msg, args)
}
func (l *defaultLogger) Warn(msg string, args ...any) {
	log.Printf("[Compactor] WARN %s %v", msg, args)
}
func (l *defaultLogger) Error(msg string, args ...any) {
	log.Printf("[Compactor] ERROR %s %v", msg, args)
}

func formatSize(bytes int) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := unit, 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMG"[exp])
}

const (
	MinBlocksBeforeCompaction = consts.StorageMinBlocksBeforeCompaction
	CompactionCheckInterval   = consts.StorageCompactionInterval
)

type Compactor struct {
	store  *Storage
	logger CompactorLogger
	stopCh chan struct{}
	doneCh chan struct{}
}

// NewCompactor creates a compactor with default logger
func NewCompactor(store *Storage) *Compactor {
	return NewCompactorWithLogger(store, &defaultLogger{})
}

// NewCompactorWithLogger creates a compactor with custom logger
func NewCompactorWithLogger(store *Storage, logger CompactorLogger) *Compactor {
	return &Compactor{
		store:  store,
		logger: logger,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

func (c *Compactor) Start(ctx context.Context) {
	go c.run(ctx)
}

func (c *Compactor) Stop() {
	close(c.stopCh)
	<-c.doneCh
}

func (c *Compactor) run(ctx context.Context) {
	defer close(c.doneCh)

	ticker := time.NewTicker(CompactionCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.compactAll(ctx)
		}
	}
}

func (c *Compactor) compactAll(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !c.compactOneBatch() {
			return
		}
	}
}

// compactOneBatch compacts exactly 100 blocks (one batch) and returns true if more work remains.
func (c *Compactor) compactOneBatch() bool {
	firstBlock, ok := c.store.FirstBlock()
	if !ok {
		return false
	}

	latestBlock, ok := c.store.LatestBlock()
	if !ok {
		return false
	}

	// Need enough blocks buffered before we compact
	blockCount := latestBlock - firstBlock + 1
	if blockCount < MinBlocksBeforeCompaction+BatchSize {
		return false
	}

	// Align to batch boundary
	batchStart := BatchStart(firstBlock)
	batchEnd := BatchEnd(batchStart)

	// Don't compact if we'd get too close to tip
	if latestBlock < batchEnd+MinBlocksBeforeCompaction {
		return false
	}

	// Read 100 blocks
	blocks := make([][]byte, BatchSize)
	for i := uint64(0); i < BatchSize; i++ {
		data, err := c.store.GetBlock(batchStart + i)
		if err != nil || data == nil {
			c.logger.Warn("missing block", "block", batchStart+i)
			return false
		}
		blocks[i] = data
	}

	// Compress
	compressed, err := CompressBlocks(blocks)
	if err != nil {
		c.logger.Error("compress failed", "error", err)
		return false
	}

	// Save compressed batch to local storage
	if err := c.store.SaveBatch(batchStart, batchEnd, compressed); err != nil {
		c.logger.Error("save batch failed", "error", err)
		return false
	}

	// Update meta
	if err := c.store.SaveMeta(batchEnd); err != nil {
		c.logger.Error("meta update failed", "error", err)
		return false
	}

	// Delete individual blocks
	if err := c.store.DeleteBlockRange(batchStart, batchEnd); err != nil {
		c.logger.Error("delete failed", "error", err)
	}

	c.logger.Info("compacted", "range", fmt.Sprintf("%d-%d", batchStart, batchEnd), "size", formatSize(len(compressed)))

	return true
}
