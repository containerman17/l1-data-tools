package transform

import (
	"fmt"
	"strings"

	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/rpc"
)

// FlattenCallTrace recursively flattens a CallTrace tree into InternalTxRow slices.
// This is the entry point that initializes the position counter.
func FlattenCallTrace(
	b *rpc.Block,
	txHash string,
	trace *rpc.CallTrace,
	parentIndex string,
	depth int,
) []InternalTxRow {
	if trace == nil {
		return nil
	}

	// Initialize position counter starting at 0
	position := 0
	return flattenCallTraceInternal(b, txHash, trace, parentIndex, depth, &position)
}

// flattenCallTraceInternal is the recursive implementation with position counter.
func flattenCallTraceInternal(
	b *rpc.Block,
	txHash string,
	trace *rpc.CallTrace,
	parentIndex string,
	depth int,
	position *int,
) []InternalTxRow {
	if trace == nil {
		return nil
	}

	ts := parseHexInt(b.Timestamp)

	// Build call index: "call_0", "call_0_0", etc.
	var callIndex string
	isRootCall := parentIndex == ""
	if isRootCall {
		callIndex = "call_0"
	} else {
		callIndex = fmt.Sprintf("%s_%d", parentIndex, depth)
	}

	// Trace position is a global counter across the entire call tree
	tracePosition := fmt.Sprintf("%d", *position)
	*position++

	// Determine if reverted
	hasError := trace.Error != ""

	// Calculate gas values
	// For root-level calls, subtract intrinsic gas to get execution gas only
	gas := parseHexInt(trace.Gas)
	gasUsed := parseHexInt(trace.GasUsed)

	if isRootCall {
		traceType := strings.ToUpper(trace.Type)
		isCreate := traceType == "CREATE" || traceType == "CREATE2"
		intrinsicGas := calculateIntrinsicGas(trace.Input, isCreate)

		gas -= intrinsicGas
		gasUsed -= intrinsicGas

		// Ensure non-negative values
		if gas < 0 {
			gas = 0
		}
		if gasUsed < 0 {
			gasUsed = 0
		}
	}

	row := InternalTxRow{
		BlockHash:       b.Hash,
		BlockNumber:     parseHexInt(b.Number),
		BlockTimestamp:  ts,
		TransactionHash: txHash,
		Type:            strings.ToUpper(trace.Type),
		From:            strings.ToLower(trace.From),
		To:              normalizeAddress(strings.ToLower(trace.To)),
		Value:           hexToBigIntStr(trace.Value),
		Gas:             gas,
		GasUsed:         gasUsed,
		Revert:          revertBoolStr(hasError),
		Error:           trace.Error,
		RevertReason:    trace.RevertReason,
		Input:           trace.Input,
		Output:          normalizeHexOutput(trace.Output),
		CallIndex:       callIndex,
		TracePosition:   tracePosition,
		PartitionDate:   partitionDate(ts),
	}

	rows := []InternalTxRow{row}

	// Recursively flatten child calls
	for i, child := range trace.Calls {
		childRows := flattenCallTraceInternal(b, txHash, &child, callIndex, i, position)
		rows = append(rows, childRows...)
	}

	return rows
}
