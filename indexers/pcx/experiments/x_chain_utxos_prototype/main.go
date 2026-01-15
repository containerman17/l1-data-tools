package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	avmblock "github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/cockroachdb/pebble/v2"
	"github.com/joho/godotenv"
)

// Test addresses for Fuji
var testAddresses = []string{
	"fuji1ur873jhz9qnaqv5qthk5sn3e8nj3e0kmafyxut",
}

// UTXO stored by address -> utxoKey -> UTXO
type UTXOIndex struct {
	byAddress   map[ids.ShortID]map[string]*UTXO
	utxoLookup  map[string]*UTXO     // Reverse index: utxoKey -> UTXO
	targetAddrs map[ids.ShortID]bool // Only index these addresses
}

type UTXO struct {
	TxID          ids.ID
	OutputIndex   uint32
	Amount        uint64
	AssetID       ids.ID
	ConsumingTxID ids.ID // Empty if unspent
}

func formatAVAX(nanoAVAX uint64) string {
	avax := float64(nanoAVAX) / 1e9
	return fmt.Sprintf("%.4f", avax)
}

func main() {
	godotenv.Load()

	blocksDir := flag.String("blocks", "./data/5/blocks/x", "X-Chain blocks directory")
	flag.Parse()

	fmt.Println("=== X-Chain UTXO Prototype ===")
	fmt.Printf("Data dir: %s\n\n", *blocksDir)

	// Step 1: Build UTXO index from pre-Cortina txs + post-Cortina blocks
	fmt.Println("Building UTXO index...")
	index, stats, err := buildUTXOIndex(*blocksDir)
	if err != nil {
		log.Fatalf("FATAL: %v", err)
	}
	fmt.Printf("Indexed: %d pre-Cortina txs + %d post-Cortina blocks (%d txs) = %d total txs\n",
		stats.preCortinaTxs, stats.postCortinaBlk, stats.postCortinaTxs, stats.totalTxs)
	fmt.Printf("Addresses with UTXOs: %d\n\n", len(index.byAddress))

	// Step 2: Test each address
	fmt.Printf("Testing %d addresses...\n\n", len(testAddresses))

	for i, addr := range testAddresses {
		fmt.Printf("[%d/%d] %s ... ", i+1, len(testAddresses), addr)

		targetAddr, err := address.ParseToID("X-" + addr)
		if err != nil {
			log.Fatalf("FATAL: parse address: %v", err)
		}

		result, err := compareAddress(addr, targetAddr, index)
		if err != nil {
			log.Fatalf("FATAL: %v", err)
		}

		if result.mismatch {
			os.Exit(1)
		}

		fmt.Printf("OK (%d UTXOs, %s AVAX)\n", result.utxoCount, formatAVAX(result.totalAmount))
	}

	fmt.Println("\n=== ALL PASSED ===")
}

type CompareResult struct {
	mismatch    bool
	utxoCount   int
	totalAmount uint64
}

func compareAddress(addr string, targetAddr ids.ShortID, index *UTXOIndex) (CompareResult, error) {
	// Fetch from Glacier
	glacierUTXOs, err := getGlacierUTXOs([]string{addr}, true)
	if err != nil {
		return CompareResult{}, fmt.Errorf("glacier fetch: %w", err)
	}

	// Build remote set
	remoteSet := make(map[string]GlacierUTXO)
	for _, u := range glacierUTXOs {
		key := fmt.Sprintf("%s:%s", u.CreationTxHash, u.OutputIndex)
		remoteSet[key] = u
	}

	// Get local UTXOs for this address
	localUTXOs := index.byAddress[targetAddr]
	if localUTXOs == nil {
		localUTXOs = make(map[string]*UTXO)
	}

	// Find differences
	var problems []string

	xChainID := "2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm" // Fuji X-Chain

	for key, remote := range remoteSet {
		// Skip UTXOs created on other chains (imported UTXOs)
		if remote.CreatedOnChainId != xChainID {
			continue
		}

		local, exists := localUTXOs[key]
		if !exists {
			status := "unspent"
			if remote.ConsumingTxHash != "" {
				status = fmt.Sprintf("spent by %s", remote.ConsumingTxHash)
			}
			problems = append(problems, fmt.Sprintf("MISSING IN LOCAL: %s (amount: %s, %s)", key, remote.Asset.Amount, status))
			continue
		}

		// Compare values
		remoteAmount := remote.Asset.Amount
		localAmount := fmt.Sprintf("%d", local.Amount)
		if remoteAmount != localAmount {
			problems = append(problems, fmt.Sprintf("AMOUNT MISMATCH: %s - glacier=%s, local=%s", key, remoteAmount, localAmount))
		}

		remoteAsset := remote.Asset.AssetId
		localAsset := local.AssetID.String()
		if remoteAsset != localAsset {
			problems = append(problems, fmt.Sprintf("ASSET MISMATCH: %s - glacier=%s, local=%s", key, remoteAsset, localAsset))
		}

		// Skip spent verification for now - requires cross-chain data (P/C ImportTx)
		// TODO: Track cross-chain consumption for production
		_ = remote.ConsumingTxHash
		_ = local.ConsumingTxID
	}

	for key, u := range localUTXOs {
		if _, exists := remoteSet[key]; !exists {
			status := "unspent"
			if u.ConsumingTxID != ids.Empty {
				status = fmt.Sprintf("spent by %s", u.ConsumingTxID)
			}
			problems = append(problems, fmt.Sprintf("MISSING IN GLACIER: %s (amount: %d, %s)", key, u.Amount, status))
		}
	}

	if len(problems) > 0 {
		fmt.Printf("FAIL\n\n")
		fmt.Printf("Glacier: %d UTXOs, Local: %d UTXOs\n", len(remoteSet), len(localUTXOs))
		fmt.Printf("\nProblems (%d):\n", len(problems))
		for _, p := range problems {
			fmt.Printf("  %s\n", p)
		}
		return CompareResult{mismatch: true}, nil
	}

	// Calculate totals
	var totalAmount uint64
	for _, u := range localUTXOs {
		totalAmount += u.Amount
	}

	return CompareResult{
		mismatch:    false,
		utxoCount:   len(localUTXOs),
		totalAmount: totalAmount,
	}, nil
}

type IndexStats struct {
	preCortinaTxs  int
	postCortinaBlk int
	postCortinaTxs int
	totalTxs       int
}

func buildUTXOIndex(blocksDir string) (*UTXOIndex, IndexStats, error) {
	blocksDB, err := pebble.Open(blocksDir, &pebble.Options{
		Logger:   noopLogger{},
		ReadOnly: true,
	})
	if err != nil {
		return nil, IndexStats{}, fmt.Errorf("failed to open blocks DB: %w", err)
	}
	defer blocksDB.Close()

	var stats IndexStats

	// Phase 1: Load all data into memory
	fmt.Println("  Loading data into memory...")
	start := time.Now()

	var preCortinaTxBytes [][]byte
	var blockBytes [][]byte

	// Load pre-Cortina txs
	keyTxPrefix := []byte("tx:")
	txIter, _ := blocksDB.NewIter(&pebble.IterOptions{LowerBound: keyTxPrefix})
	for txIter.First(); txIter.Valid(); txIter.Next() {
		key := txIter.Key()
		if len(key) < 3 || string(key[:3]) != "tx:" {
			break
		}
		data := make([]byte, len(txIter.Value()))
		copy(data, txIter.Value())
		preCortinaTxBytes = append(preCortinaTxBytes, data)
		if len(preCortinaTxBytes)%100000 == 0 {
			fmt.Printf("    pre-Cortina: %d txs loaded...\n", len(preCortinaTxBytes))
		}
	}
	txIter.Close()
	fmt.Printf("    pre-Cortina: %d txs total\n", len(preCortinaTxBytes))

	// Load post-Cortina blocks
	keyBlkPrefix := []byte("blk:")
	blkIter, _ := blocksDB.NewIter(&pebble.IterOptions{LowerBound: keyBlkPrefix})
	for blkIter.First(); blkIter.Valid(); blkIter.Next() {
		key := blkIter.Key()
		if len(key) < 4 || string(key[:4]) != "blk:" {
			break
		}
		data := make([]byte, len(blkIter.Value()))
		copy(data, blkIter.Value())
		blockBytes = append(blockBytes, data)
		if len(blockBytes)%10000 == 0 {
			fmt.Printf("    post-Cortina: %d blocks loaded...\n", len(blockBytes))
		}
	}
	blkIter.Close()
	fmt.Printf("    post-Cortina: %d blocks total\n", len(blockBytes))

	fmt.Printf("  Loaded in %v\n", time.Since(start).Round(time.Millisecond))

	// Phase 2: Parse in parallel
	fmt.Print("  Parsing transactions... ")
	start = time.Now()
	numWorkers := runtime.NumCPU()

	// Parse pre-Cortina txs in parallel
	parsedPreCortinaTxs := make([]*txs.Tx, len(preCortinaTxBytes))
	var wg sync.WaitGroup
	var parseErr error
	var errOnce sync.Once

	chunkSize := (len(preCortinaTxBytes) + numWorkers - 1) / numWorkers
	for w := 0; w < numWorkers; w++ {
		start := w * chunkSize
		end := start + chunkSize
		if end > len(preCortinaTxBytes) {
			end = len(preCortinaTxBytes)
		}
		if start >= len(preCortinaTxBytes) {
			break
		}

		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			parser, _ := avmblock.NewParser([]fxs.Fx{
				&secp256k1fx.Fx{},
				&nftfx.Fx{},
				&propertyfx.Fx{},
			})
			for i := start; i < end; i++ {
				// Strip timestamp prefix [8 bytes]
				txBytes := preCortinaTxBytes[i]
				if len(txBytes) < 8 {
					errOnce.Do(func() { parseErr = fmt.Errorf("pre-Cortina tx %d: too short", i) })
					return
				}
				tx, err := parser.ParseTx(txBytes[8:])
				if err != nil {
					errOnce.Do(func() { parseErr = fmt.Errorf("pre-Cortina tx %d: %w", i, err) })
					return
				}
				parsedPreCortinaTxs[i] = tx
			}
		}(start, end)
	}
	wg.Wait()
	if parseErr != nil {
		return nil, IndexStats{}, parseErr
	}

	// Parse blocks in parallel
	type blockTxs struct {
		txs []*txs.Tx
	}
	parsedBlocks := make([]blockTxs, len(blockBytes))

	chunkSize = (len(blockBytes) + numWorkers - 1) / numWorkers
	for w := 0; w < numWorkers; w++ {
		start := w * chunkSize
		end := start + chunkSize
		if end > len(blockBytes) {
			end = len(blockBytes)
		}
		if start >= len(blockBytes) {
			break
		}

		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			parser, _ := avmblock.NewParser([]fxs.Fx{
				&secp256k1fx.Fx{},
				&nftfx.Fx{},
				&propertyfx.Fx{},
			})
			for i := start; i < end; i++ {
				blk, err := parser.ParseBlock(blockBytes[i])
				if err != nil {
					errOnce.Do(func() { parseErr = fmt.Errorf("block %d: %w", i, err) })
					return
				}
				parsedBlocks[i] = blockTxs{txs: blk.Txs()}
			}
		}(start, end)
	}
	wg.Wait()
	if parseErr != nil {
		return nil, IndexStats{}, parseErr
	}

	fmt.Printf("done in %v\n", time.Since(start).Round(time.Millisecond))

	// Phase 3: Build index (sequential - modifies shared state)
	fmt.Print("  Building UTXO index... ")
	start = time.Now()

	// Convert test addresses to target set
	targetAddrs := make(map[ids.ShortID]bool)
	for _, addr := range testAddresses {
		shortID, err := address.ParseToID("X-" + addr)
		if err != nil {
			return nil, IndexStats{}, fmt.Errorf("parse target address %s: %w", addr, err)
		}
		targetAddrs[shortID] = true
	}

	index := &UTXOIndex{
		byAddress:   make(map[ids.ShortID]map[string]*UTXO),
		utxoLookup:  make(map[string]*UTXO),
		targetAddrs: targetAddrs,
	}

	for _, tx := range parsedPreCortinaTxs {
		if tx != nil {
			processTx(tx, index)
			stats.preCortinaTxs++
		}
	}

	for _, blk := range parsedBlocks {
		stats.postCortinaBlk++
		for _, tx := range blk.txs {
			processTx(tx, index)
			stats.postCortinaTxs++
		}
	}

	fmt.Printf("done in %v\n", time.Since(start).Round(time.Millisecond))

	stats.totalTxs = stats.preCortinaTxs + stats.postCortinaTxs
	return index, stats, nil
}

func processTx(tx *txs.Tx, index *UTXOIndex) {
	txID := tx.ID()
	unsigned := tx.Unsigned

	var ins []*avax.TransferableInput
	var importedIns []*avax.TransferableInput

	switch t := unsigned.(type) {
	case *txs.BaseTx:
		ins = t.Ins
		processOutputs(t.Outs, txID, index)

	case *txs.CreateAssetTx:
		ins = t.Ins
		processOutputs(t.Outs, txID, index)
		processInitialStates(t.States, txID, len(t.Outs), index)

	case *txs.OperationTx:
		ins = t.Ins
		processOutputs(t.Outs, txID, index)
		processOperations(t.Ops, txID, len(t.Outs), index)

	case *txs.ImportTx:
		ins = t.Ins
		importedIns = t.ImportedIns
		processOutputs(t.Outs, txID, index)

	case *txs.ExportTx:
		ins = t.Ins
		processOutputs(t.Outs, txID, index)
		// ExportedOuts go to shared memory (P/C chain) but are still X-Chain UTXOs
		processOutputsWithStartIdx(t.ExportedOuts, txID, len(t.Outs), index)
	}

	// Consume inputs
	for _, in := range ins {
		consumeUTXO(in.UTXOID.TxID, in.UTXOID.OutputIndex, txID, index)
	}

	for _, in := range importedIns {
		consumeUTXO(in.UTXOID.TxID, in.UTXOID.OutputIndex, txID, index)
	}
}

func consumeUTXO(utxoTxID ids.ID, outputIndex uint32, consumingTxID ids.ID, index *UTXOIndex) {
	utxoKey := fmt.Sprintf("%s:%d", utxoTxID, outputIndex)
	if u, exists := index.utxoLookup[utxoKey]; exists {
		u.ConsumingTxID = consumingTxID
	}
}

func processOutputs(outputs []*avax.TransferableOutput, txID ids.ID, index *UTXOIndex) {
	processOutputsWithStartIdx(outputs, txID, 0, index)
}

func processOutputsWithStartIdx(outputs []*avax.TransferableOutput, txID ids.ID, startIdx int, index *UTXOIndex) {
	for i, out := range outputs {
		addAnyUTXO(out.Out, txID, uint32(startIdx+i), out.AssetID(), index)
	}
}

func processInitialStates(states []*txs.InitialState, txID ids.ID, startIdx int, index *UTXOIndex) {
	idx := uint32(startIdx)
	for _, state := range states {
		for _, out := range state.Outs {
			addAnyUTXO(out, txID, idx, txID, index) // AssetID is txID for CreateAssetTx
			idx++
		}
	}
}

func processOperations(ops []*txs.Operation, txID ids.ID, startIdx int, index *UTXOIndex) {
	idx := uint32(startIdx)
	for _, op := range ops {
		assetID := op.AssetID()
		for _, out := range op.Op.Outs() {
			addAnyUTXO(out, txID, idx, assetID, index)
			idx++
		}
	}
}

func addAnyUTXO(out any, txID ids.ID, outputIndex uint32, assetID ids.ID, index *UTXOIndex) {
	var addrs []ids.ShortID
	var amount uint64

	switch o := out.(type) {
	case *secp256k1fx.TransferOutput:
		addrs = o.Addrs
		amount = o.Amt
	case *nftfx.TransferOutput:
		addrs = o.Addrs
		amount = 0
	case *nftfx.MintOutput:
		addrs = o.Addrs
		amount = 0
	case *propertyfx.OwnedOutput:
		addrs = o.Addrs
		amount = 0
	case *propertyfx.MintOutput:
		addrs = o.Addrs
		amount = 0
	default:
		return
	}

	for _, addr := range addrs {
		addUTXO(addr, txID, outputIndex, amount, assetID, index)
	}
}

func addUTXO(addr ids.ShortID, txID ids.ID, outputIndex uint32, amount uint64, assetID ids.ID, index *UTXOIndex) {
	// Only index target addresses
	if !index.targetAddrs[addr] {
		return
	}

	if index.byAddress[addr] == nil {
		index.byAddress[addr] = make(map[string]*UTXO)
	}
	utxoKey := fmt.Sprintf("%s:%d", txID, outputIndex)
	utxo := &UTXO{
		TxID:        txID,
		OutputIndex: outputIndex,
		Amount:      amount,
		AssetID:     assetID,
	}
	index.byAddress[addr][utxoKey] = utxo
	index.utxoLookup[utxoKey] = utxo // Add to reverse index
}

type noopLogger struct{}

func (noopLogger) Infof(format string, args ...interface{})  {}
func (noopLogger) Errorf(format string, args ...interface{}) {}
func (noopLogger) Fatalf(format string, args ...interface{}) { os.Exit(1) }
