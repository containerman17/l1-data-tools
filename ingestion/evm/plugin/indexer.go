package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/subnet-evm/eth/tracers"
	"github.com/ava-labs/subnet-evm/rpc"
)

// Stats for periodic logging (protected by mutex)
var (
	statsMu      sync.Mutex
	statsBlocks  uint64
	statsMinMs   float64
	statsMaxMs   float64
	statsTotalMs float64
	statsSizeMB  float64
	statsStart   = time.Now()
)

// indexBlock indexes a single block synchronously.
// Called from Accept() BEFORE the underlying block is accepted.
func (vm *IndexingVM) indexBlock(ctx context.Context, height uint64) error {
	start := time.Now()

	// Get block from chain
	block := vm.chain.GetBlockByNumber(height)
	if block == nil {
		return fmt.Errorf("block %d not found in chain", height)
	}

	receipts := vm.chain.GetReceiptsByHash(block.Hash())
	if receipts == nil {
		return fmt.Errorf("receipts not found for block %d", height)
	}

	// Marshal block (RPC format)
	blockRPC := RPCMarshalBlock(block, true, true, vm.config)

	// Marshal receipts (RPC format)
	receiptsRPC := make([]map[string]interface{}, len(receipts))
	for i, receipt := range receipts {
		receiptsRPC[i] = marshalReceipt(receipt, block.Hash(), block.NumberU64(), uint64(i), block.Transactions()[i], vm.config)
	}

	// Get traces
	if vm.tracerAPI == nil {
		return fmt.Errorf("tracerAPI not available")
	}
	tracerName := "callTracer"
	cfg := &tracers.TraceConfig{Tracer: &tracerName}
	blockNum := rpc.BlockNumber(height)
	tracesRPC, err := vm.tracerAPI.TraceBlockByNumber(ctx, blockNum, cfg)
	if err != nil {
		return fmt.Errorf("trace block %d: %w", height, err)
	}

	// Build normalized block
	normalized := map[string]interface{}{
		"block":    blockRPC,
		"receipts": receiptsRPC,
		"traces":   tracesRPC,
	}

	data, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("marshal block %d: %w", height, err)
	}

	// Save to storage (writes to versiondb.mem, committed by wrappedBlock.Accept())
	if err := vm.store.SaveBlock(height, data); err != nil {
		return fmt.Errorf("save block %d: %w", height, err)
	}

	// NOTE: Do NOT update lastIndexedHeight here!
	// It must be updated in Accept() AFTER b.Block.Accept() succeeds.
	// Otherwise, if Accept() fails after indexBlock(), the skip check will
	// trigger on retry and we'll create a gap.

	// Update server (for live streaming - acceptable to be slightly ahead)
	if vm.server != nil {
		vm.server.UpdateLatestBlock(height)
	}

	// Update stats and log periodically
	elapsed := time.Since(start)
	vm.updateStats(height, len(data), elapsed)

	return nil
}

// updateStats tracks indexing performance and logs periodically
func (vm *IndexingVM) updateStats(height uint64, size int, elapsed time.Duration) {
	statsMu.Lock()
	defer statsMu.Unlock()

	ms := float64(elapsed.Milliseconds())
	statsBlocks++
	statsTotalMs += ms
	statsSizeMB += float64(size) / 1024 / 1024

	if statsMinMs == 0 || ms < statsMinMs {
		statsMinMs = ms
	}
	if ms > statsMaxMs {
		statsMaxMs = ms
	}

	// Log every 100 blocks or every 5 seconds
	sinceLast := time.Since(statsStart)
	if statsBlocks >= 100 || sinceLast >= 5*time.Second {
		if statsBlocks > 0 {
			avgMs := statsTotalMs / float64(statsBlocks)
			vm.logger.Info("IndexingVM: progress",
				logging.UserString("blocks", fmt.Sprintf("%d", statsBlocks)),
				logging.UserString("size_mb", fmt.Sprintf("%.1f", statsSizeMB)),
				logging.UserString("min_ms", fmt.Sprintf("%.1f", statsMinMs)),
				logging.UserString("max_ms", fmt.Sprintf("%.1f", statsMaxMs)),
				logging.UserString("avg_ms", fmt.Sprintf("%.1f", avgMs)),
				logging.UserString("height", fmt.Sprintf("%d", height)))
		}
		// Reset
		statsBlocks = 0
		statsMinMs = 0
		statsMaxMs = 0
		statsTotalMs = 0
		statsSizeMB = 0
		statsStart = time.Now()
	}
}
