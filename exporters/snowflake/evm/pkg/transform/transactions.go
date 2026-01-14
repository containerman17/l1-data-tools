package transform

import (
	"math/big"

	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/rpc"
)

// TransformTransaction converts an RPC Transaction to a TransactionRow.
func TransformTransaction(b *rpc.Block, tx *rpc.Transaction) TransactionRow {
	ts := parseHexInt(b.Timestamp)
	gasPrice := parseHexBigInt(tx.GasPrice)
	gas := parseHexBigInt(tx.Gas)
	value := parseHexBigInt(tx.Value)

	// TransactionCost = gas × gasPrice + value
	gasCost := new(big.Int).Mul(gas, gasPrice)
	cost := new(big.Int).Add(gasCost, value)

	// For pre-EIP-1559 transactions, MaxFeePerGas and MaxPriorityFeePerGas
	// default to GasPrice in the golden data
	maxFeePerGas := tx.MaxFeePerGas
	if maxFeePerGas == "" {
		maxFeePerGas = tx.GasPrice
	}
	maxPriorityFeePerGas := tx.MaxPriorityFeePerGas
	if maxPriorityFeePerGas == "" {
		maxPriorityFeePerGas = tx.GasPrice
	}

	return TransactionRow{
		BlockHash:                       b.Hash,
		BlockNumber:                     parseHexInt(b.Number),
		BlockTimestamp:                  ts,
		TransactionHash:                 tx.Hash,
		TransactionFrom:                 tx.From,
		TransactionTo:                   normalizeAddress(tx.To),
		TransactionGas:                  gas.Int64(),
		TransactionGasPrice:             gasPrice.Int64(),
		TransactionMaxFeePerGas:         hexToInt64Str(maxFeePerGas),
		TransactionMaxPriorityFeePerGas: hexToInt64Str(maxPriorityFeePerGas),
		TransactionInput:                tx.Input,
		TransactionNonce:                parseHexInt(tx.Nonce),
		TransactionIndex:                parseHexInt(tx.TransactionIndex),
		TransactionCost:                 cost.String(),
		TransactionValue:                hexToBigIntStr(tx.Value),
		TransactionType:                 hexToInt64Str(tx.Type),
		PartitionDate:                   partitionDate(ts),
	}
}
