// Package transform implements EVM data transformation to Snowflake-compatible CSV rows.
package transform

// BlockRow represents a C_BLOCKS row.
type BlockRow struct {
	BlockTimestamp      int64  `csv:"BLOCKTIMESTAMP"`
	BlockHash           string `csv:"BLOCKHASH"`
	BlockNumber         int64  `csv:"BLOCKNUMBER"`
	BlockBaseFeePerGas  string `csv:"BLOCKBASEFEEPERGAS"` // Empty if not present
	BlockGasLimit       int64  `csv:"BLOCKGASLIMIT"`
	BlockGasUsed        int64  `csv:"BLOCKGASUSED"`
	BlockParentHash     string `csv:"BLOCKPARENTHASH"`
	BlockReceiptHash    string `csv:"BLOCKRECEIPTHASH"` // Actually the receiptsRoot field
	BlockReceiptsRoot   string `csv:"BLOCKRECEIPTSROOT"`
	BlockSize           int64  `csv:"BLOCKSIZE"`
	BlockStateRoot      string `csv:"BLOCKSTATEROOT"`
	BlockTransactionLen int64  `csv:"BLOCKTRANSACTIONLEN"`
	BlockExtraData      string `csv:"BLOCKEXTRADATA"`
	BlockExtDataGasUsed string `csv:"BLOCKEXTDATAGASUSED"` // Empty if not present
	BlockGasCost        string `csv:"BLOCKGASCOST"`        // Empty if not present
	PartitionDate       string `csv:"PARTITION_DATE"`      // YYYY-MM-DD derived from timestamp
}

// TransactionRow represents a C_TRANSACTIONS row.
type TransactionRow struct {
	BlockHash                       string `csv:"BLOCKHASH"`
	BlockNumber                     int64  `csv:"BLOCKNUMBER"`
	BlockTimestamp                  int64  `csv:"BLOCKTIMESTAMP"`
	TransactionHash                 string `csv:"TRANSACTIONHASH"`
	TransactionFrom                 string `csv:"TRANSACTIONFROM"`
	TransactionTo                   string `csv:"TRANSACTIONTO"`
	TransactionGas                  int64  `csv:"TRANSACTIONGAS"`
	TransactionGasPrice             int64  `csv:"TRANSACTIONGASPRICE"`
	TransactionMaxFeePerGas         string `csv:"TRANSACTIONMAXFEEPERGAS"`         // Empty or value
	TransactionMaxPriorityFeePerGas string `csv:"TRANSACTIONMAXPRIORITYFEEPERGAS"` // Empty or value
	TransactionInput                string `csv:"TRANSACTIONINPUT"`
	TransactionNonce                int64  `csv:"TRANSACTIONNONCE"`
	TransactionIndex                int64  `csv:"TRANSACTIONINDEX"`
	TransactionCost                 string `csv:"TRANSACTIONCOST"`  // gas * gasPrice as string
	TransactionValue                string `csv:"TRANSACTIONVALUE"` // Wei as string
	TransactionType                 string `csv:"TRANSACTIONTYPE"`  // Numeric type
	PartitionDate                   string `csv:"PARTITION_DATE"`
}

// ReceiptRow represents a C_RECEIPTS row.
type ReceiptRow struct {
	BlockHash                           string `csv:"BLOCKHASH"`
	BlockNumber                         int64  `csv:"BLOCKNUMBER"`
	BlockTimestamp                      int64  `csv:"BLOCKTIMESTAMP"`
	TransactionHash                     string `csv:"TRANSACTIONHASH"`
	TransactionReceiptGasUsed           int64  `csv:"TRANSACTIONRECEIPTGASUSED"`
	TransactionReceiptCumulativeGasUsed int64  `csv:"TRANSACTIONRECEIPTCUMULATIVEGASUSED"`
	TransactionReceiptStatus            int64  `csv:"TRANSACTIONRECEIPTSTATUS"` // 0 or 1
	TransactionReceiptContractAddress   string `csv:"TRANSACTIONRECEIPTCONTRACTADDRESS"`
	TransactionReceiptPostState         string `csv:"TRANSACTIONRECEIPTPOSTSTATE"` // Empty usually
	TransactionReceiptEffectiveGasPrice int64  `csv:"TRANSACTIONRECEIPTEFFECTIVEGASPRICE"`
	PartitionDate                       string `csv:"PARTITION_DATE"`
}

// LogRow represents a C_LOGS row.
type LogRow struct {
	BlockHash        string `csv:"BLOCKHASH"`
	BlockNumber      int64  `csv:"BLOCKNUMBER"`
	BlockTimestamp   int64  `csv:"BLOCKTIMESTAMP"`
	TransactionHash  string `csv:"TRANSACTIONHASH"`
	LogAddress       string `csv:"LOGADDRESS"`
	TopicHex0        string `csv:"TOPICHEX_0"`
	TopicHex1        string `csv:"TOPICHEX_1"`
	TopicHex2        string `csv:"TOPICHEX_2"`
	TopicHex3        string `csv:"TOPICHEX_3"`
	TopicDec0        string `csv:"TOPICDEC_0"` // Decimal representation
	TopicDec1        string `csv:"TOPICDEC_1"`
	TopicDec2        string `csv:"TOPICDEC_2"`
	TopicDec3        string `csv:"TOPICDEC_3"`
	LogData          string `csv:"LOGDATA"`
	LogIndex         int64  `csv:"LOGINDEX"`
	TransactionIndex int64  `csv:"TRANSACTIONINDEX"`
	Removed          bool   `csv:"REMOVED"`
	PartitionDate    string `csv:"PARTITION_DATE"`
}

// InternalTxRow represents a C_INTERNAL_TRANSACTIONS row.
type InternalTxRow struct {
	BlockHash       string `csv:"BLOCKHASH"`
	BlockNumber     int64  `csv:"BLOCKNUMBER"`
	BlockTimestamp  int64  `csv:"BLOCKTIMESTAMP"`
	TransactionHash string `csv:"TRANSACTIONHASH"`
	Type            string `csv:"TYPE"`
	From            string `csv:"FROM"`
	To              string `csv:"TO"`
	Value           string `csv:"VALUE"` // Wei as string
	Gas             int64  `csv:"GAS"`
	GasUsed         int64  `csv:"GASUSED"`
	Revert          string `csv:"REVERT"` // "true"/"false"
	Error           string `csv:"ERROR"`
	RevertReason    string `csv:"REVERTREASON"`
	Input           string `csv:"INPUT"`
	Output          string `csv:"OUTPUT"`
	CallIndex       string `csv:"CALLINDEX"`      // e.g. "call_0", "call_0_0"
	TracePosition   string `csv:"TRACE_POSITION"` // e.g. "0", "1"
	PartitionDate   string `csv:"PARTITION_DATE"`
}

// MessageRow represents a C_MESSAGES row.
type MessageRow struct {
	BlockHash                  string `csv:"BLOCKHASH"`
	BlockNumber                int64  `csv:"BLOCKNUMBER"`
	BlockTimestamp             int64  `csv:"BLOCKTIMESTAMP"`
	TransactionHash            string `csv:"TRANSACTIONHASH"`
	TransactionMessageFrom     string `csv:"TRANSACTIONMESSAGEFROM"`
	TransactionMessageTo       string `csv:"TRANSACTIONMESSAGETO"` // May be empty for contract creation
	TransactionMessageGasPrice int64  `csv:"TRANSACTIONMESSAGEGASPRICE"`
	PartitionDate              string `csv:"PARTITION_DATE"`
}

// ExportBatch contains all transformed rows from a batch of blocks.
type ExportBatch struct {
	Blocks       []BlockRow
	Transactions []TransactionRow
	Receipts     []ReceiptRow
	Logs         []LogRow
	InternalTxs  []InternalTxRow
	Messages     []MessageRow
}
