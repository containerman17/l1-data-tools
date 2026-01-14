package transform

import (
	"fmt"
	"strings"

	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/rpc"
)

// FlattenCallTrace recursively flattens a CallTrace tree into InternalTxRow slices.
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

	ts := parseHexInt(b.Timestamp)

	// Build call index: "call_0", "call_0_0", etc.
	var callIndex string
	if parentIndex == "" {
		callIndex = "call_0"
	} else {
		callIndex = fmt.Sprintf("%s_%d", parentIndex, depth)
	}

	// Trace position is just the depth level
	tracePosition := fmt.Sprintf("%d", depth)

	// Determine if reverted
	hasError := trace.Error != ""

	row := InternalTxRow{
		BlockHash:       b.Hash,
		BlockNumber:     parseHexInt(b.Number),
		BlockTimestamp:  ts,
		TransactionHash: txHash,
		Type:            strings.ToUpper(trace.Type),
		From:            strings.ToLower(trace.From),
		To:              normalizeAddress(strings.ToLower(trace.To)),
		Value:           hexToBigIntStr(trace.Value),
		Gas:             parseHexInt(trace.Gas),
		GasUsed:         parseHexInt(trace.GasUsed),
		Revert:          revertBoolStr(hasError),
		Error:           trace.Error,
		RevertReason:    trace.RevertReason,
		Input:           trace.Input,
		Output:          trace.Output,
		CallIndex:       callIndex,
		TracePosition:   tracePosition,
		PartitionDate:   partitionDate(ts),
	}

	rows := []InternalTxRow{row}

	// Recursively flatten child calls
	for i, child := range trace.Calls {
		childRows := FlattenCallTrace(b, txHash, &child, callIndex, i)
		rows = append(rows, childRows...)
	}

	return rows
}
