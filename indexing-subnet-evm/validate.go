package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/subnet-evm/eth/tracers"
	"github.com/ava-labs/subnet-evm/rpc"
)

// validateAfterBootstrap compares a DB-stored block with fresh RPC fetch
// to ensure our marshaling matches the RPC format
func (vm *IndexingVM) validateAfterBootstrap() {
	lastIndexed := vm.lastIndexedHeight.Load()
	if lastIndexed == 0 {
		vm.logger.Info("IndexingVM: validation skipped (no indexed blocks)")
		return
	}

	// Use the current tip - always has state available
	currentHeight := vm.chain.CurrentBlock().Number.Uint64()

	// Check if we have this block indexed
	if currentHeight > lastIndexed {
		vm.logger.Info("IndexingVM: validation skipped (tip not indexed yet)",
			logging.UserString("tip", fmt.Sprintf("%d", currentHeight)),
			logging.UserString("lastIndexed", fmt.Sprintf("%d", lastIndexed)))
		return
	}

	validateHeight := currentHeight

	vm.logger.Info("IndexingVM: starting format validation",
		logging.UserString("height", fmt.Sprintf("%d", validateHeight)))

	ctx := context.Background()

	// 1. Get from DB
	dbData, err := vm.store.GetBlock(validateHeight)
	if err != nil {
		vm.logger.Error("IndexingVM: validation failed - can't read from DB",
			logging.UserString("error", err.Error()))
		return
	}

	// 2. Fetch fresh via RPC
	rpcData, err := vm.fetchBlockViaRPC(ctx, validateHeight)
	if err != nil {
		vm.logger.Error("IndexingVM: validation failed - RPC fetch failed",
			logging.UserString("error", err.Error()))
		return
	}

	// 3. Compare (normalize JSON for comparison)
	dbNorm, err := normalizeJSON(dbData)
	if err != nil {
		vm.logger.Error("IndexingVM: validation failed - can't normalize DB JSON",
			logging.UserString("error", err.Error()))
		return
	}

	rpcNorm, err := normalizeJSON(rpcData)
	if err != nil {
		vm.logger.Error("IndexingVM: validation failed - can't normalize RPC JSON",
			logging.UserString("error", err.Error()))
		return
	}

	if bytes.Equal(dbNorm, rpcNorm) {
		vm.logger.Info("IndexingVM: validation PASSED - DB matches RPC format",
			logging.UserString("height", fmt.Sprintf("%d", validateHeight)))
		return
	}

	// Mismatch - write files for debugging
	dbFile := fmt.Sprintf("/tmp/indexer_validate_db_%d.json", validateHeight)
	rpcFile := fmt.Sprintf("/tmp/indexer_validate_rpc_%d.json", validateHeight)

	os.WriteFile(dbFile, prettyJSON(dbData), 0644)
	os.WriteFile(rpcFile, prettyJSON(rpcData), 0644)

	// Run diff
	diffOut, _ := exec.Command("diff", "-u", rpcFile, dbFile).CombinedOutput()

	vm.logger.Error("IndexingVM: validation FAILED - DB does not match RPC format",
		logging.UserString("height", fmt.Sprintf("%d", validateHeight)),
		logging.UserString("db_file", dbFile),
		logging.UserString("rpc_file", rpcFile),
		logging.UserString("diff_preview", truncate(string(diffOut), 500)))
}

// fetchBlockViaRPC fetches a block using RPC (same format as external clients)
func (vm *IndexingVM) fetchBlockViaRPC(ctx context.Context, height uint64) ([]byte, error) {
	// Get block via eth API
	blockNum := rpc.BlockNumber(height)
	block, err := vm.eth.APIBackend.BlockByNumber(ctx, blockNum)
	if err != nil {
		return nil, fmt.Errorf("BlockByNumber: %w", err)
	}
	if block == nil {
		return nil, fmt.Errorf("block %d not found", height)
	}

	// Get receipts
	receipts, err := vm.eth.APIBackend.GetReceipts(ctx, block.Hash())
	if err != nil {
		return nil, fmt.Errorf("GetReceipts: %w", err)
	}

	// Get traces
	tracerName := "callTracer"
	cfg := &tracers.TraceConfig{Tracer: &tracerName}
	traces, err := vm.tracerAPI.TraceBlockByNumber(ctx, blockNum, cfg)
	if err != nil {
		return nil, fmt.Errorf("TraceBlockByNumber: %w", err)
	}

	// Marshal using RPC format (same as our indexer)
	blockRPC := RPCMarshalBlock(block, true, true, vm.config)

	receiptsRPC := make([]map[string]interface{}, len(receipts))
	for i, receipt := range receipts {
		receiptsRPC[i] = marshalReceipt(receipt, block.Hash(), block.NumberU64(), uint64(i), block.Transactions()[i], vm.config)
	}

	normalized := map[string]interface{}{
		"block":    blockRPC,
		"receipts": receiptsRPC,
		"traces":   traces,
	}

	return json.Marshal(normalized)
}

// normalizeJSON re-marshals JSON to normalize formatting
func normalizeJSON(data []byte) ([]byte, error) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// prettyJSON formats JSON with indentation
func prettyJSON(data []byte) []byte {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return data
	}
	pretty, _ := json.MarshalIndent(v, "", "  ")
	return pretty
}

// truncate limits string length for logging
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
