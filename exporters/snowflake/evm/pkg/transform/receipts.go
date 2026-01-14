package transform

import (
	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/rpc"
)

// TransformReceipt converts an RPC Receipt to a ReceiptRow.
func TransformReceipt(b *rpc.Block, r *rpc.Receipt) ReceiptRow {
	ts := parseHexInt(b.Timestamp)

	// ContractAddress normalization: nil pointer means no contract created
	contractAddr := ""
	if r.ContractAddress != nil && *r.ContractAddress != "" {
		contractAddr = *r.ContractAddress
	} else {
		// CSV shows 0x0000...0000 for non-contract-creation txs
		contractAddr = "0x0000000000000000000000000000000000000000"
	}

	return ReceiptRow{
		BlockHash:                           b.Hash,
		BlockNumber:                         parseHexInt(b.Number),
		BlockTimestamp:                      ts,
		TransactionHash:                     r.TransactionHash,
		TransactionReceiptGasUsed:           parseHexInt(r.GasUsed),
		TransactionReceiptCumulativeGasUsed: parseHexInt(r.CumulativeGasUsed),
		TransactionReceiptStatus:            parseHexInt(r.Status),
		TransactionReceiptContractAddress:   contractAddr,
		TransactionReceiptPostState:         "", // Usually empty
		TransactionReceiptEffectiveGasPrice: parseHexInt(r.EffectiveGasPrice),
		PartitionDate:                       partitionDate(ts),
	}
}
