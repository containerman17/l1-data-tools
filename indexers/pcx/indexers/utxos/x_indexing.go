package utxos

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	avmblock "github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/runner"
	ts "github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/xchain/pre_cortina_timestamps"
	"github.com/cockroachdb/pebble/v2"
)

// parsedXTx holds a parsed pre-Cortina transaction with metadata
type parsedXTx struct {
	tx        *txs.Tx
	timestamp int64
	creds     []Credential // Pre-extracted credentials (parallelized)
}

func (u *UTXOs) ProcessXChainPreCortinaTxs(ctx context.Context, inputTxs []indexer.XTx) error {
	if len(inputTxs) == 0 {
		return nil
	}

	// Load timestamp archive (cached after first call)
	network := "fuji"
	if u.networkID == 1 {
		network = "mainnet"
	}
	archive, err := ts.GetFujiArchive() // TODO: support mainnet when archive is built
	if err != nil {
		return fmt.Errorf("loading timestamp archive: %w", err)
	}

	// 1. Parse transactions in parallel
	parseStart := time.Now()
	parsedTxs, err := u.parseXChainTxsParallel(inputTxs)
	if err != nil {
		return err
	}
	runner.XStatParse.Add(time.Since(parseStart).Microseconds())

	// 2. Fix timestamps: Replace Index API bogus timestamps with real network timestamps
	for i := range parsedTxs {
		txID := parsedTxs[i].tx.ID()
		realTimestamp, found := archive.Lookup(txID)
		if !found {
			return fmt.Errorf("tx %s (index %d) not found in %s archive", txID, inputTxs[i].Index, network)
		}
		parsedTxs[i].timestamp = realTimestamp
	}

	// 3. Process (write) sequentially
	u.mu.Lock()
	defer u.mu.Unlock()

	batch := u.db.NewIndexedBatch()
	defer batch.Close()

	// Determine X-Chain ID based on network
	xChainID := xChainIDFuji
	if u.networkID == 1 {
		xChainID = xChainIDMainnet
	}

	for _, ptx := range parsedTxs {
		u.processXChainTx(batch, ptx.tx, ptx.timestamp, xChainID, ptx.creds)
	}

	lastIndex := inputTxs[len(inputTxs)-1].Index
	indexBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(indexBytes, lastIndex)
	batch.Set([]byte("x:preCortina:watermark"), indexBytes, nil)

	return batch.Commit(pebble.NoSync)
}

// parseXChainTxsParallel parses transactions in parallel using worker pool
func (u *UTXOs) parseXChainTxsParallel(inputTxs []indexer.XTx) ([]parsedXTx, error) {
	n := len(inputTxs)
	results := make([]parsedXTx, n)

	numWorkers := runtime.NumCPU()
	if numWorkers > n {
		numWorkers = n
	}

	var wg sync.WaitGroup
	var parseErr error
	var errOnce sync.Once

	chunkSize := (n + numWorkers - 1) / numWorkers

	for w := 0; w < numWorkers; w++ {
		start := w * chunkSize
		end := start + chunkSize
		if end > n {
			end = n
		}
		if start >= n {
			break
		}

		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()

			// Each worker needs its own parser (not thread-safe)
			parser, err := avmblock.NewParser([]fxs.Fx{
				&secp256k1fx.Fx{},
				&nftfx.Fx{},
				&propertyfx.Fx{},
			})
			if err != nil {
				errOnce.Do(func() { parseErr = fmt.Errorf("create parser: %w", err) })
				return
			}

			for i := start; i < end; i++ {
				xtx := inputTxs[i]
				tx, err := parser.ParseTx(xtx.Bytes)
				if err != nil {
					errOnce.Do(func() { parseErr = fmt.Errorf("parse pre-Cortina tx %d: %w", xtx.Index, err) })
					return
				}
				// Extract credentials in parallel (RecoverPublicKey is CPU-bound)
				creds := extractAVMCredentials(tx)
				results[i] = parsedXTx{tx: tx, timestamp: xtx.Timestamp, creds: creds}
			}
		}(start, end)
	}

	wg.Wait()

	if parseErr != nil {
		return nil, parseErr
	}

	return results, nil
}

func (u *UTXOs) ProcessXChainBlocks(ctx context.Context, blocks []indexer.XBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	batch := u.db.NewIndexedBatch()
	defer batch.Close()

	// Determine X-Chain ID based on network
	xChainID := xChainIDFuji
	if u.networkID == 1 {
		xChainID = xChainIDMainnet
	}

	for _, blk := range blocks {
		avmBlk, err := u.avmParser.ParseBlock(blk.Bytes)
		if err != nil {
			return fmt.Errorf("parse X-Chain block %d: %w", blk.Height, err)
		}

		timestamp := avmBlk.Timestamp().Unix()

		for _, tx := range avmBlk.Txs() {
			// Post-Cortina has fewer txs (~40k blocks), extract credentials inline
			creds := extractAVMCredentials(tx)
			u.processXChainTx(batch, tx, timestamp, xChainID, creds)
		}
	}

	lastHeight := blocks[len(blocks)-1].Height
	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, lastHeight)
	batch.Set([]byte("x:block:watermark"), heightBytes, nil)

	return batch.Commit(pebble.NoSync)
}

func (u *UTXOs) processXChainTx(batch *pebble.Batch, tx *txs.Tx, timestamp int64, xChainID string, creds []Credential) {
	txID := tx.ID()
	unsigned := tx.Unsigned

	// 1. Extract inputs and outputs
	var ins []*avax.TransferableInput
	var outs []*avax.TransferableOutput
	var exportedOuts []*avax.TransferableOutput
	var destChain ids.ID

	switch t := unsigned.(type) {
	case *txs.BaseTx:
		ins = t.Ins
		outs = t.Outs
	case *txs.ImportTx:
		ins = t.Ins
		outs = t.Outs
		// Also handles imported inputs (mark consumed on source chain)
		u.processXChainImportTx(batch, t, txID, timestamp, xChainID, creds)
	case *txs.ExportTx:
		ins = t.Ins
		outs = t.Outs
		exportedOuts = t.ExportedOuts // Fixed field name
		destChain = t.DestinationChain
	case *txs.CreateAssetTx:
		ins = t.Ins
		outs = t.Outs
		// Asset metadata!
		saveAssetMetadata(batch, &AssetMetadata{
			AssetID:      txID.String(),
			Name:         t.Name,
			Symbol:       t.Symbol,
			Denomination: int(t.Denomination),
		})
	case *txs.OperationTx:
		ins = t.Ins
		outs = t.Outs
	}

	// 2. Mark consumed inputs
	for _, in := range ins {
		utxoID := in.UTXOID.InputID().String()
		u.markXChainConsumed(batch, utxoID, txID.String(), timestamp, creds)
	}

	// 2b. Mark inputs from Operations (X-Chain specific)
	if t, ok := unsigned.(*txs.OperationTx); ok {
		for _, op := range t.Ops {
			for _, utxoID := range op.UTXOIDs {
				u.markXChainConsumed(batch, utxoID.InputID().String(), txID.String(), timestamp, creds)
			}
		}
	}

	// 3. Index regular outputs
	u.indexXChainOutputs(batch, txID, 0, outs, timestamp, xChainID, xChainID)

	// 4. Index other outputs (InitialState, Operations)
	currentIdx := uint32(len(outs))
	switch t := unsigned.(type) {
	case *txs.CreateAssetTx:
		for _, state := range t.States {
			for _, out := range state.Outs {
				u.indexAnyOutputToChain(batch, txID, currentIdx, out, txID, 0, timestamp, xChainID, xChainID, prefixXChainUTXO, prefixXChainAddr)
				currentIdx++
			}
		}
	case *txs.OperationTx:
		for _, op := range t.Ops {
			for _, out := range op.Op.Outs() {
				u.indexAnyOutputToChain(batch, txID, currentIdx, out, op.AssetID(), 0, timestamp, xChainID, xChainID, prefixXChainUTXO, prefixXChainAddr)
				currentIdx++
			}
		}
	}

	// 5. Index exported outputs
	if len(exportedOuts) > 0 {
		destChainStr := destChain.String()
		u.indexXChainOutputs(batch, txID, int(currentIdx), exportedOuts, timestamp, xChainID, destChainStr)

		// Double-write to destination chain storage
		if destChainStr == pChainID {
			u.indexOutputsToChain(batch, txID, int(currentIdx), exportedOuts, 0, timestamp, xChainID, destChainStr, prefixPChainUTXO, prefixPChainAddr)
		} else if destChainStr == cChainIDFuji || destChainStr == cChainIDMainnet {
			u.indexOutputsToChain(batch, txID, int(currentIdx), exportedOuts, 0, timestamp, xChainID, destChainStr, prefixCChainUTXO, prefixCChainAddr)
		}
	}
}

func (u *UTXOs) indexXChainOutputs(batch *pebble.Batch, txID ids.ID, startIdx int, outputs []*avax.TransferableOutput, timestamp int64, createdOnChain, consumedOnChain string) {
	for i, out := range outputs {
		outputIdx := uint32(startIdx + i)
		u.indexAnyOutputToChain(batch, txID, outputIdx, out.Out, out.AssetID(), 0, timestamp, createdOnChain, consumedOnChain, prefixXChainUTXO, prefixXChainAddr)
	}
}

func (u *UTXOs) markXChainConsumed(batch *pebble.Batch, utxoID string, consumingTxHash string, consumingTimestamp int64, credentials []Credential) {
	// Use write-only spend index instead of read-modify-write upsert
	markSpent(batch, "x:", utxoID, &SpendInfo{
		ConsumingTxHash: consumingTxHash,
		ConsumingTime:   consumingTimestamp,
		Credentials:     credentials,
	})
}

func (u *UTXOs) processXChainImportTx(batch *pebble.Batch, t *txs.ImportTx, consumingTxID ids.ID, consumingTimestamp int64, xChainID string, credentials []Credential) {
	sourceChainID := t.SourceChain.String()

	for _, importedIn := range t.ImportedIns {
		utxoID := importedIn.UTXOID.InputID().String()

		// Use write-only spend index for X-Chain (cross-chain import)
		markSpent(batch, "x:", utxoID, &SpendInfo{
			ConsumingTxHash:   consumingTxID.String(),
			ConsumingTime:     consumingTimestamp,
			Credentials:       credentials,
			ConsumedOnChainID: xChainID,
			CreatedOnChainID:  sourceChainID,
		})

		// Also mark spent on source chain
		if sourceChainID == pChainID {
			markSpent(batch, "p:", utxoID, &SpendInfo{
				ConsumingTxHash: consumingTxID.String(),
				ConsumingTime:   consumingTimestamp,
			})
		} else if sourceChainID == cChainIDFuji || sourceChainID == cChainIDMainnet {
			markSpent(batch, "c:", utxoID, &SpendInfo{
				ConsumingTxHash: consumingTxID.String(),
				ConsumingTime:   consumingTimestamp,
			})
		}
	}
}
