package snowflake

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/containerman17/l1-data-tools/exporters/snowflake/evm/pkg/transform"
	sf "github.com/snowflakedb/gosnowflake"
)

// WriteBatch writes all transformed data to Snowflake atomically.
func (c *Client) WriteBatch(ctx context.Context, batch *transform.ExportBatch) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := c.insertBlocks(ctx, tx, batch.Blocks); err != nil {
		return err
	}

	if err := c.insertTransactions(ctx, tx, batch.Transactions); err != nil {
		return err
	}

	if err := c.insertReceipts(ctx, tx, batch.Receipts); err != nil {
		return err
	}

	if err := c.insertLogs(ctx, tx, batch.Logs); err != nil {
		return err
	}

	if err := c.insertInternalTxs(ctx, tx, batch.InternalTxs); err != nil {
		return err
	}

	if err := c.insertMessages(ctx, tx, batch.Messages); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (c *Client) insertBlocks(ctx context.Context, tx *sql.Tx, blocks []transform.BlockRow) error {
	if len(blocks) == 0 {
		return nil
	}

	blockNumbers := make([]int64, len(blocks))
	blockHashes := make([]string, len(blocks))
	timestamps := make([]int64, len(blocks))
	baseFeePerGas := make([]string, len(blocks))
	gasLimits := make([]int64, len(blocks))
	gasUsed := make([]int64, len(blocks))
	parentHashes := make([]string, len(blocks))
	receiptHashes := make([]string, len(blocks))
	receiptsRoots := make([]string, len(blocks))
	sizes := make([]int64, len(blocks))
	stateRoots := make([]string, len(blocks))
	transactionLens := make([]int64, len(blocks))
	extraData := make([]string, len(blocks))
	extDataGasUsed := make([]string, len(blocks))
	gasCost := make([]string, len(blocks))
	partitionDates := make([]string, len(blocks))

	for i, b := range blocks {
		blockNumbers[i] = b.BlockNumber
		blockHashes[i] = b.BlockHash
		timestamps[i] = b.BlockTimestamp
		baseFeePerGas[i] = b.BlockBaseFeePerGas
		gasLimits[i] = b.BlockGasLimit
		gasUsed[i] = b.BlockGasUsed
		parentHashes[i] = b.BlockParentHash
		receiptHashes[i] = b.BlockReceiptHash
		receiptsRoots[i] = b.BlockReceiptsRoot
		sizes[i] = b.BlockSize
		stateRoots[i] = b.BlockStateRoot
		transactionLens[i] = b.BlockTransactionLen
		extraData[i] = b.BlockExtraData
		extDataGasUsed[i] = b.BlockExtDataGasUsed
		gasCost[i] = b.BlockGasCost
		partitionDates[i] = b.PartitionDate
	}

	query := fmt.Sprintf(
		`INSERT INTO %sBLOCKS (
			BLOCKNUMBER, BLOCKHASH, BLOCKTIMESTAMP, BLOCKBASEFEEPERGAS,
			BLOCKGASLIMIT, BLOCKGASUSED, BLOCKPARENTHASH, BLOCKRECEIPTHASH,
			BLOCKRECEIPTSROOT, BLOCKSIZE, BLOCKSTATEROOT, BLOCKTRANSACTIONLEN,
			BLOCKEXTRADATA, BLOCKEXTDATAGASUSED, BLOCKGASCOST, PARTITION_DATE
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.prefix)

	_, err := tx.ExecContext(ctx, query,
		sf.Array(&blockNumbers),
		sf.Array(&blockHashes),
		sf.Array(&timestamps),
		sf.Array(&baseFeePerGas),
		sf.Array(&gasLimits),
		sf.Array(&gasUsed),
		sf.Array(&parentHashes),
		sf.Array(&receiptHashes),
		sf.Array(&receiptsRoots),
		sf.Array(&sizes),
		sf.Array(&stateRoots),
		sf.Array(&transactionLens),
		sf.Array(&extraData),
		sf.Array(&extDataGasUsed),
		sf.Array(&gasCost),
		sf.Array(&partitionDates),
	)
	if err != nil {
		return fmt.Errorf("insert blocks: %w", err)
	}

	return nil
}

func (c *Client) insertTransactions(ctx context.Context, tx *sql.Tx, txs []transform.TransactionRow) error {
	if len(txs) == 0 {
		return nil
	}

	blockHashes := make([]string, len(txs))
	blockNumbers := make([]int64, len(txs))
	timestamps := make([]int64, len(txs))
	txHashes := make([]string, len(txs))
	froms := make([]string, len(txs))
	tos := make([]string, len(txs))
	gas := make([]int64, len(txs))
	gasPrices := make([]int64, len(txs))
	maxFeePerGas := make([]string, len(txs))
	maxPriorityFeePerGas := make([]string, len(txs))
	inputs := make([]string, len(txs))
	nonces := make([]int64, len(txs))
	indexes := make([]int64, len(txs))
	costs := make([]string, len(txs))
	values := make([]string, len(txs))
	txTypes := make([]string, len(txs))
	partitionDates := make([]string, len(txs))

	for i, t := range txs {
		blockHashes[i] = t.BlockHash
		blockNumbers[i] = t.BlockNumber
		timestamps[i] = t.BlockTimestamp
		txHashes[i] = t.TransactionHash
		froms[i] = t.TransactionFrom
		tos[i] = t.TransactionTo
		gas[i] = t.TransactionGas
		gasPrices[i] = t.TransactionGasPrice
		maxFeePerGas[i] = t.TransactionMaxFeePerGas
		maxPriorityFeePerGas[i] = t.TransactionMaxPriorityFeePerGas
		inputs[i] = t.TransactionInput
		nonces[i] = t.TransactionNonce
		indexes[i] = t.TransactionIndex
		costs[i] = t.TransactionCost
		values[i] = t.TransactionValue
		txTypes[i] = t.TransactionType
		partitionDates[i] = t.PartitionDate
	}

	query := fmt.Sprintf(
		`INSERT INTO %sTRANSACTIONS (
			BLOCKHASH, BLOCKNUMBER, BLOCKTIMESTAMP, TRANSACTIONHASH,
			TRANSACTIONFROM, TRANSACTIONTO, TRANSACTIONGAS, TRANSACTIONGASPRICE,
			TRANSACTIONMAXFEEPERGAS, TRANSACTIONMAXPRIORITYFEEPERGAS, TRANSACTIONINPUT,
			TRANSACTIONNONCE, TRANSACTIONINDEX, TRANSACTIONCOST, TRANSACTIONVALUE,
			TRANSACTIONTYPE, PARTITION_DATE
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.prefix)

	_, err := tx.ExecContext(ctx, query,
		sf.Array(&blockHashes),
		sf.Array(&blockNumbers),
		sf.Array(&timestamps),
		sf.Array(&txHashes),
		sf.Array(&froms),
		sf.Array(&tos),
		sf.Array(&gas),
		sf.Array(&gasPrices),
		sf.Array(&maxFeePerGas),
		sf.Array(&maxPriorityFeePerGas),
		sf.Array(&inputs),
		sf.Array(&nonces),
		sf.Array(&indexes),
		sf.Array(&costs),
		sf.Array(&values),
		sf.Array(&txTypes),
		sf.Array(&partitionDates),
	)
	if err != nil {
		return fmt.Errorf("insert transactions: %w", err)
	}

	return nil
}

func (c *Client) insertReceipts(ctx context.Context, tx *sql.Tx, receipts []transform.ReceiptRow) error {
	if len(receipts) == 0 {
		return nil
	}

	blockHashes := make([]string, len(receipts))
	blockNumbers := make([]int64, len(receipts))
	timestamps := make([]int64, len(receipts))
	txHashes := make([]string, len(receipts))
	gasUsed := make([]int64, len(receipts))
	cumulativeGasUsed := make([]int64, len(receipts))
	status := make([]int64, len(receipts))
	contractAddresses := make([]string, len(receipts))
	postState := make([]string, len(receipts))
	effectiveGasPrice := make([]int64, len(receipts))
	partitionDates := make([]string, len(receipts))

	for i, r := range receipts {
		blockHashes[i] = r.BlockHash
		blockNumbers[i] = r.BlockNumber
		timestamps[i] = r.BlockTimestamp
		txHashes[i] = r.TransactionHash
		gasUsed[i] = r.TransactionReceiptGasUsed
		cumulativeGasUsed[i] = r.TransactionReceiptCumulativeGasUsed
		status[i] = r.TransactionReceiptStatus
		contractAddresses[i] = r.TransactionReceiptContractAddress
		postState[i] = r.TransactionReceiptPostState
		effectiveGasPrice[i] = r.TransactionReceiptEffectiveGasPrice
		partitionDates[i] = r.PartitionDate
	}

	query := fmt.Sprintf(
		`INSERT INTO %sRECEIPTS (
			BLOCKHASH, BLOCKNUMBER, BLOCKTIMESTAMP, TRANSACTIONHASH,
			TRANSACTIONRECEIPTGASUSED, TRANSACTIONRECEIPTCUMULATIVEGASUSED,
			TRANSACTIONRECEIPTSTATUS, TRANSACTIONRECEIPTCONTRACTADDRESS,
			TRANSACTIONRECEIPTPOSTSTATE, TRANSACTIONRECEIPTEFFECTIVEGASPRICE,
			PARTITION_DATE
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.prefix)

	_, err := tx.ExecContext(ctx, query,
		sf.Array(&blockHashes),
		sf.Array(&blockNumbers),
		sf.Array(&timestamps),
		sf.Array(&txHashes),
		sf.Array(&gasUsed),
		sf.Array(&cumulativeGasUsed),
		sf.Array(&status),
		sf.Array(&contractAddresses),
		sf.Array(&postState),
		sf.Array(&effectiveGasPrice),
		sf.Array(&partitionDates),
	)
	if err != nil {
		return fmt.Errorf("insert receipts: %w", err)
	}

	return nil
}

func (c *Client) insertLogs(ctx context.Context, tx *sql.Tx, logs []transform.LogRow) error {
	if len(logs) == 0 {
		return nil
	}

	blockHashes := make([]string, len(logs))
	blockNumbers := make([]int64, len(logs))
	timestamps := make([]int64, len(logs))
	txHashes := make([]string, len(logs))
	addresses := make([]string, len(logs))
	topicHex0 := make([]string, len(logs))
	topicHex1 := make([]string, len(logs))
	topicHex2 := make([]string, len(logs))
	topicHex3 := make([]string, len(logs))
	topicDec0 := make([]string, len(logs))
	topicDec1 := make([]string, len(logs))
	topicDec2 := make([]string, len(logs))
	topicDec3 := make([]string, len(logs))
	data := make([]string, len(logs))
	logIndexes := make([]int64, len(logs))
	txIndexes := make([]int64, len(logs))
	removed := make([]bool, len(logs))
	partitionDates := make([]string, len(logs))

	for i, l := range logs {
		blockHashes[i] = l.BlockHash
		blockNumbers[i] = l.BlockNumber
		timestamps[i] = l.BlockTimestamp
		txHashes[i] = l.TransactionHash
		addresses[i] = l.LogAddress
		topicHex0[i] = l.TopicHex0
		topicHex1[i] = l.TopicHex1
		topicHex2[i] = l.TopicHex2
		topicHex3[i] = l.TopicHex3
		topicDec0[i] = l.TopicDec0
		topicDec1[i] = l.TopicDec1
		topicDec2[i] = l.TopicDec2
		topicDec3[i] = l.TopicDec3
		data[i] = l.LogData
		logIndexes[i] = l.LogIndex
		txIndexes[i] = l.TransactionIndex
		removed[i] = l.Removed
		partitionDates[i] = l.PartitionDate
	}

	query := fmt.Sprintf(
		`INSERT INTO %sLOGS (
			BLOCKHASH, BLOCKNUMBER, BLOCKTIMESTAMP, TRANSACTIONHASH,
			LOGADDRESS, TOPICHEX_0, TOPICHEX_1, TOPICHEX_2, TOPICHEX_3,
			TOPICDEC_0, TOPICDEC_1, TOPICDEC_2, TOPICDEC_3,
			LOGDATA, LOGINDEX, TRANSACTIONINDEX, REMOVED, PARTITION_DATE
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.prefix)

	_, err := tx.ExecContext(ctx, query,
		sf.Array(&blockHashes),
		sf.Array(&blockNumbers),
		sf.Array(&timestamps),
		sf.Array(&txHashes),
		sf.Array(&addresses),
		sf.Array(&topicHex0),
		sf.Array(&topicHex1),
		sf.Array(&topicHex2),
		sf.Array(&topicHex3),
		sf.Array(&topicDec0),
		sf.Array(&topicDec1),
		sf.Array(&topicDec2),
		sf.Array(&topicDec3),
		sf.Array(&data),
		sf.Array(&logIndexes),
		sf.Array(&txIndexes),
		sf.Array(&removed),
		sf.Array(&partitionDates),
	)
	if err != nil {
		return fmt.Errorf("insert logs: %w", err)
	}

	return nil
}

func (c *Client) insertInternalTxs(ctx context.Context, tx *sql.Tx, txs []transform.InternalTxRow) error {
	if len(txs) == 0 {
		return nil
	}

	blockHashes := make([]string, len(txs))
	blockNumbers := make([]int64, len(txs))
	timestamps := make([]int64, len(txs))
	txHashes := make([]string, len(txs))
	txTypes := make([]string, len(txs))
	froms := make([]string, len(txs))
	tos := make([]string, len(txs))
	values := make([]string, len(txs))
	gas := make([]int64, len(txs))
	gasUsed := make([]int64, len(txs))
	revert := make([]string, len(txs))
	errors := make([]string, len(txs))
	revertReasons := make([]string, len(txs))
	inputs := make([]string, len(txs))
	outputs := make([]string, len(txs))
	callIndexes := make([]string, len(txs))
	tracePositions := make([]string, len(txs))
	partitionDates := make([]string, len(txs))

	for i, t := range txs {
		blockHashes[i] = t.BlockHash
		blockNumbers[i] = t.BlockNumber
		timestamps[i] = t.BlockTimestamp
		txHashes[i] = t.TransactionHash
		txTypes[i] = t.Type
		froms[i] = t.From
		tos[i] = t.To
		values[i] = t.Value
		gas[i] = t.Gas
		gasUsed[i] = t.GasUsed
		revert[i] = t.Revert
		errors[i] = t.Error
		revertReasons[i] = t.RevertReason
		inputs[i] = t.Input
		outputs[i] = t.Output
		callIndexes[i] = t.CallIndex
		tracePositions[i] = t.TracePosition
		partitionDates[i] = t.PartitionDate
	}

	query := fmt.Sprintf(
		`INSERT INTO %sINTERNAL_TRANSACTIONS (
			BLOCKHASH, BLOCKNUMBER, BLOCKTIMESTAMP, TRANSACTIONHASH,
			TYPE, FROM, TO, VALUE, GAS, GASUSED, REVERT, ERROR,
			REVERTREASON, INPUT, OUTPUT, CALLINDEX, TRACE_POSITION, PARTITION_DATE
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.prefix)

	_, err := tx.ExecContext(ctx, query,
		sf.Array(&blockHashes),
		sf.Array(&blockNumbers),
		sf.Array(&timestamps),
		sf.Array(&txHashes),
		sf.Array(&txTypes),
		sf.Array(&froms),
		sf.Array(&tos),
		sf.Array(&values),
		sf.Array(&gas),
		sf.Array(&gasUsed),
		sf.Array(&revert),
		sf.Array(&errors),
		sf.Array(&revertReasons),
		sf.Array(&inputs),
		sf.Array(&outputs),
		sf.Array(&callIndexes),
		sf.Array(&tracePositions),
		sf.Array(&partitionDates),
	)
	if err != nil {
		return fmt.Errorf("insert internal transactions: %w", err)
	}

	return nil
}

func (c *Client) insertMessages(ctx context.Context, tx *sql.Tx, messages []transform.MessageRow) error {
	if len(messages) == 0 {
		return nil
	}

	blockHashes := make([]string, len(messages))
	blockNumbers := make([]int64, len(messages))
	timestamps := make([]int64, len(messages))
	txHashes := make([]string, len(messages))
	froms := make([]string, len(messages))
	tos := make([]string, len(messages))
	gasPrices := make([]int64, len(messages))
	partitionDates := make([]string, len(messages))

	for i, m := range messages {
		blockHashes[i] = m.BlockHash
		blockNumbers[i] = m.BlockNumber
		timestamps[i] = m.BlockTimestamp
		txHashes[i] = m.TransactionHash
		froms[i] = m.TransactionMessageFrom
		tos[i] = m.TransactionMessageTo
		gasPrices[i] = m.TransactionMessageGasPrice
		partitionDates[i] = m.PartitionDate
	}

	query := fmt.Sprintf(
		`INSERT INTO %sMESSAGES (
			BLOCKHASH, BLOCKNUMBER, BLOCKTIMESTAMP, TRANSACTIONHASH,
			TRANSACTIONMESSAGEFROM, TRANSACTIONMESSAGETO, TRANSACTIONMESSAGEGASPRICE,
			PARTITION_DATE
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, c.prefix)

	_, err := tx.ExecContext(ctx, query,
		sf.Array(&blockHashes),
		sf.Array(&blockNumbers),
		sf.Array(&timestamps),
		sf.Array(&txHashes),
		sf.Array(&froms),
		sf.Array(&tos),
		sf.Array(&gasPrices),
		sf.Array(&partitionDates),
	)
	if err != nil {
		return fmt.Errorf("insert messages: %w", err)
	}

	return nil
}
