package transform

import (
	"encoding/base64"
	"encoding/hex"

	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/rpc"
)

// TransformBlock converts a NormalizedBlock to a BlockRow.
func TransformBlock(b *rpc.Block) BlockRow {
	ts := parseHexInt(b.Timestamp)

	return BlockRow{
		BlockTimestamp:      ts,
		BlockHash:           b.Hash,
		BlockNumber:         parseHexInt(b.Number),
		BlockBaseFeePerGas:  hexToInt64Str(b.BaseFeePerGas),
		BlockGasLimit:       parseHexInt(b.GasLimit),
		BlockGasUsed:        parseHexInt(b.GasUsed),
		BlockParentHash:     b.ParentHash,
		BlockReceiptHash:    b.ReceiptsRoot, // Golden data maps ReceiptsRoot here
		BlockReceiptsRoot:   b.StateRoot,    // Golden data maps StateRoot here (seems swapped in source)
		BlockSize:           parseHexInt(b.Size),
		BlockStateRoot:      b.StateRoot,
		BlockTransactionLen: int64(len(b.Transactions)),
		BlockExtraData:      hexToBase64(b.ExtraData), // Golden data uses base64-encoded extradata
		BlockExtDataGasUsed: hexToInt64Str(b.ExtDataGasUsed),
		BlockGasCost:        hexToInt64Str(b.BlockGasCost),
		PartitionDate:       partitionDate(ts),
	}
}

// hexToBase64 converts a hex string (0x...) to base64 encoded string.
func hexToBase64(hexStr string) string {
	if hexStr == "" {
		return ""
	}
	// Remove 0x prefix
	if len(hexStr) >= 2 && (hexStr[:2] == "0x" || hexStr[:2] == "0X") {
		hexStr = hexStr[2:]
	}
	// Decode hex to bytes
	data, err := hex.DecodeString(hexStr)
	if err != nil {
		return ""
	}
	// Encode to base64
	return base64.StdEncoding.EncodeToString(data)
}
