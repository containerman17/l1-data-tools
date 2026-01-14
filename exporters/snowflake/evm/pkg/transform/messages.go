package transform

import (
	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/rpc"
)

// TransformMessage converts an RPC Transaction to a MessageRow.
// Messages represent the "message" aspect of transactions (from/to/gasPrice).
func TransformMessage(b *rpc.Block, tx *rpc.Transaction) MessageRow {
	ts := parseHexInt(b.Timestamp)

	return MessageRow{
		BlockHash:                  b.Hash,
		BlockNumber:                parseHexInt(b.Number),
		BlockTimestamp:             ts,
		TransactionHash:            tx.Hash,
		TransactionMessageFrom:     tx.From,
		TransactionMessageTo:       tx.To,
		TransactionMessageGasPrice: parseHexInt(tx.GasPrice),
		PartitionDate:              partitionDate(ts),
	}
}
