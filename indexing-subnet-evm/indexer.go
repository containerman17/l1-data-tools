package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/subnet-evm/eth/tracers"
	"github.com/ava-labs/subnet-evm/rpc"
)

// indexingLoop runs in background, indexes blocks as they become available
func (vm *IndexingVM) indexingLoop() {
	vm.logger.Info("IndexingVM: indexing loop started")

	// Stats for periodic logging
	var statsBlocks uint64
	var statsMinMs, statsMaxMs, statsTotalMs float64
	var statsSizeMB float64
	statsStart := time.Now()

	for {
		select {
		case <-vm.stopIndexer:
			vm.logger.Info("IndexingVM: indexing loop stopped")
			return
		default:
		}

		lastIndexed := vm.lastIndexedHeight.Load()
		lastAccepted := vm.lastAcceptedHeight.Load()

		// Log stats every second (or every 5s when idle)
		sinceLast := time.Since(statsStart)
		if (statsBlocks > 0 && sinceLast >= time.Second) || sinceLast >= 5*time.Second {
			if statsBlocks > 0 {
				avgMs := statsTotalMs / float64(statsBlocks)
				vm.logger.Info("IndexingVM: progress",
					logging.UserString("blocks", fmt.Sprintf("%d", statsBlocks)),
					logging.UserString("size_mb", fmt.Sprintf("%.1f", statsSizeMB)),
					logging.UserString("min_ms", fmt.Sprintf("%.1f", statsMinMs)),
					logging.UserString("max_ms", fmt.Sprintf("%.1f", statsMaxMs)),
					logging.UserString("avg_ms", fmt.Sprintf("%.1f", avgMs)),
					logging.UserString("height", fmt.Sprintf("%d", lastIndexed)))
			} else if lastIndexed != 0 || lastAccepted != 0 {
				vm.logger.Info("IndexingVM: idle",
					logging.UserString("lastIndexed", fmt.Sprintf("%d", lastIndexed)),
					logging.UserString("lastAccepted", fmt.Sprintf("%d", lastAccepted)))
			}
			// Reset stats
			statsBlocks = 0
			statsMinMs = 0
			statsMaxMs = 0
			statsTotalMs = 0
			statsSizeMB = 0
			statsStart = time.Now()
		}

		if lastIndexed >= lastAccepted {
			time.Sleep(1 * time.Millisecond)
			continue
		}

		// Index everything we're behind
		count, sizeMB, elapsed, err := vm.indexBatch(context.Background(), lastIndexed+1, lastAccepted)
		if err != nil {
			vm.logger.Error("IndexingVM: FATAL indexing failed",
				logging.UserString("error", err.Error()))
			os.Exit(1)
		}

		// Update stats
		perBlockMs := float64(elapsed.Milliseconds()) / float64(count)
		statsBlocks += count
		statsSizeMB += sizeMB
		statsTotalMs += float64(elapsed.Milliseconds())
		if statsMinMs == 0 || perBlockMs < statsMinMs {
			statsMinMs = perBlockMs
		}
		if perBlockMs > statsMaxMs {
			statsMaxMs = perBlockMs
		}
	}
}

// blockEntry holds prepared data for one block
type blockEntry struct {
	height uint64
	data   []byte
}

// prepareBlockData fetches and marshals a single block
func (vm *IndexingVM) prepareBlockData(ctx context.Context, height uint64) (*blockEntry, error) {
	block := vm.chain.GetBlockByNumber(height)
	if block == nil {
		return nil, fmt.Errorf("block %d not found in chain", height)
	}

	receipts := vm.chain.GetReceiptsByHash(block.Hash())
	if receipts == nil {
		return nil, fmt.Errorf("receipts not found for block %d", height)
	}

	// Marshal block (RPC format)
	blockRPC := RPCMarshalBlock(block, true, true, vm.config)

	// Marshal receipts (RPC format)
	receiptsRPC := make([]map[string]interface{}, len(receipts))
	for i, receipt := range receipts {
		receiptsRPC[i] = marshalReceipt(receipt, block.Hash(), block.NumberU64(), uint64(i), block.Transactions()[i], vm.config)
	}

	// Get traces (MANDATORY)
	if vm.tracerAPI == nil {
		return nil, fmt.Errorf("tracerAPI not available")
	}
	tracerName := "callTracer"
	cfg := &tracers.TraceConfig{Tracer: &tracerName}
	blockNum := rpc.BlockNumber(height)
	tracesRPC, err := vm.tracerAPI.TraceBlockByNumber(ctx, blockNum, cfg)
	if err != nil {
		return nil, fmt.Errorf("trace block %d: %w", height, err)
	}

	// Build normalized block
	normalized := map[string]interface{}{
		"block":    blockRPC,
		"receipts": receiptsRPC,
		"traces":   tracesRPC,
	}

	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("marshal block %d: %w", height, err)
	}

	return &blockEntry{height: height, data: data}, nil
}

// indexBatch indexes a range of blocks [from, to] inclusive
// Returns: count, sizeMB, elapsed, error
func (vm *IndexingVM) indexBatch(ctx context.Context, from, to uint64) (uint64, float64, time.Duration, error) {
	start := time.Now()
	count := to - from + 1

	// Parallel fetch/marshal
	workers := runtime.NumCPU() * 2
	entries := make([]*blockEntry, count)
	var mu sync.Mutex
	var firstErr error

	heights := make(chan uint64, count)
	for h := from; h <= to; h++ {
		heights <- h
	}
	close(heights)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for height := range heights {
				entry, err := vm.prepareBlockData(ctx, height)
				mu.Lock()
				if err != nil && firstErr == nil {
					firstErr = err
				}
				if err == nil {
					entries[height-from] = entry
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if firstErr != nil {
		return 0, 0, 0, firstErr
	}

	// Save blocks to storage
	var totalSize int64
	for _, entry := range entries {
		if entry == nil {
			return 0, 0, 0, fmt.Errorf("missing entry in batch")
		}
		if err := vm.store.SaveBlock(entry.height, entry.data); err != nil {
			return 0, 0, 0, err
		}
		totalSize += int64(len(entry.data))
	}

	vm.lastIndexedHeight.Store(to)

	// Notify server of new blocks
	if vm.server != nil {
		vm.server.UpdateLatestBlock(to)
	}

	elapsed := time.Since(start)
	sizeMB := float64(totalSize) / 1024 / 1024

	return count, sizeMB, elapsed, nil
}
