package client

import (
	"sync"
	"time"

	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/metrics"
)

type BufferConfig struct {
	MaxBatchSize int64
	BufferSize   int64
}

func DefaultBufferConfig() BufferConfig {
	return BufferConfig{
		MaxBatchSize: 30 * 1024 * 1024,     // 30 MB
		BufferSize:   5 * 30 * 1024 * 1024, // 150 MB
	}
}

type receiveBuffer struct {
	mu        sync.Mutex
	items     []bufferedItem
	totalSize int64
	maxSize   int64
	cond      *sync.Cond
	closed    bool
}

type bufferedItem struct {
	compressedData []byte
	size           int64
}

func newReceiveBuffer(cfg BufferConfig) *receiveBuffer {
	b := &receiveBuffer{
		maxSize: cfg.BufferSize,
	}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *receiveBuffer) waitForSpace() bool {
	start := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()

	waited := false
	for b.totalSize >= b.maxSize && !b.closed {
		waited = true
		b.cond.Wait()
	}
	if waited {
		metrics.ClientBackpressureWaitSeconds.Observe(time.Since(start).Seconds())
	}
	return !b.closed
}

func (b *receiveBuffer) push(data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.items = append(b.items, bufferedItem{
		compressedData: data,
		size:           int64(len(data)),
	})
	b.totalSize += int64(len(data))
	metrics.ClientBufferUsedBytes.Set(float64(b.totalSize))
	b.cond.Signal()
}

func (b *receiveBuffer) sliceBatch(maxSize int64) []bufferedItem {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.items) == 0 {
		return nil
	}

	var batch []bufferedItem
	var batchSize int64

	for _, item := range b.items {
		if len(batch) > 0 && batchSize+item.size > maxSize {
			break
		}
		batch = append(batch, item)
		batchSize += item.size
	}

	b.items = b.items[len(batch):]
	b.totalSize -= batchSize
	metrics.ClientBufferUsedBytes.Set(float64(b.totalSize))
	metrics.ClientBatchesProcessedTotal.Inc()
	metrics.ClientBatchSizeBytes.Observe(float64(batchSize))
	b.cond.Signal()

	return batch
}

func (b *receiveBuffer) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.cond.Broadcast()
}
