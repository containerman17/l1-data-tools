package transform

import (
	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/rpc"
)

// Transform processes a batch of NormalizedBlocks and returns all transformed rows.
func Transform(blocks []rpc.NormalizedBlock) *ExportBatch {
	batch := &ExportBatch{}

	for _, nb := range blocks {
		b := &nb.Block

		// Transform block
		batch.Blocks = append(batch.Blocks, TransformBlock(b))

		// Transform transactions and messages
		for i := range b.Transactions {
			tx := &b.Transactions[i]
			batch.Transactions = append(batch.Transactions, TransformTransaction(b, tx))
			batch.Messages = append(batch.Messages, TransformMessage(b, tx))
		}

		// Transform receipts and logs
		for _, r := range nb.Receipts {
			batch.Receipts = append(batch.Receipts, TransformReceipt(b, &r))

			for _, log := range r.Logs {
				batch.Logs = append(batch.Logs, TransformLog(b, &log))
			}
		}

		// Transform traces (internal transactions)
		for _, traceResult := range nb.Traces {
			if traceResult.Result != nil {
				rows := FlattenCallTrace(b, traceResult.TxHash, traceResult.Result, "", 0)
				batch.InternalTxs = append(batch.InternalTxs, rows...)
			}
		}
	}

	return batch
}
