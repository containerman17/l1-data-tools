package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/cockroachdb/pebble/v2"
	"github.com/joho/godotenv"

	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/pchain"
)

type LocalUTXO struct {
	TxID          ids.ID
	OutputIndex   uint32
	Amount        uint64
	AssetID       ids.ID
	Staked        bool
	ConsumingTxID ids.ID // Empty if unspent
}

var cachedClient *pchain.CachedClient

func initClient() error {
	baseURL := "http://localhost:9650"
	if url := os.Getenv("RPC_URL"); url != "" {
		baseURL = url
	}

	client := pchain.NewClient(baseURL)
	var err error
	cachedClient, err = pchain.NewCachedClient(client, "./data/5/rpc_cache")
	return err
}

func main() {
	godotenv.Load()

	stressTest := flag.Bool("stress", false, "Run stress test with validator addresses")
	flag.Parse()

	if err := initClient(); err != nil {
		log.Fatalf("Failed to init client: %v", err)
	}
	defer cachedClient.Close()

	blocksDir := "./data/5/blocks"

	// Catch up blocks first
	fmt.Println("=== CATCHING UP BLOCKS ===")
	if err := CatchUpBlocks(blocksDir); err != nil {
		log.Fatalf("Failed to catch up blocks: %v", err)
	}
	fmt.Println()

	if *stressTest {
		runStressTest(blocksDir)
		return
	}

	// Single address mode
	// testAddr :
	// = "fuji1deswvjzkwst3uehwf2cpvxf6g8vsrvyk76a43g"
	testAddr := "fuji1udpqdsrf5hydtl96d7qexgvkvc8tgn0d3fgrym"
	if flag.NArg() > 0 {
		testAddr = flag.Arg(0)
	}

	fmt.Printf("Comparing UTXOs for: %s\n\n", testAddr)
	missingLocal, missingRemote, err := compareAddress(testAddr, blocksDir)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	if missingLocal == 0 && missingRemote == 0 {
		fmt.Println("✅ Perfect match!")
	} else {
		fmt.Printf("❌ Mismatch: missing in local=%d, missing in remote=%d\n", missingLocal, missingRemote)
	}
}

func fetchValidatorAddresses() ([]string, error) {
	url := "https://glacier-api.avax.network/v1/networks/fuji/validators?pageSize=100"

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Validators []struct {
			PotentialRewards struct {
				RewardAddresses []string `json:"rewardAddresses"`
			} `json:"potentialRewards"`
		} `json:"validators"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	// Collect unique addresses
	addrSet := make(map[string]bool)
	for _, v := range result.Validators {
		for _, addr := range v.PotentialRewards.RewardAddresses {
			addr = strings.TrimPrefix(addr, "P-")
			addrSet[addr] = true
		}
	}

	addrs := make([]string, 0, len(addrSet))
	for addr := range addrSet {
		addrs = append(addrs, addr)
	}
	return addrs, nil
}

func compareAddress(addr string, blocksDir string) (missingLocal, missingRemote int, err error) {
	// Fetch from Glacier
	glacierUTXOs, err := getGlacierUTXOs([]string{addr}, true)
	if err != nil {
		return 0, 0, fmt.Errorf("glacier fetch: %w", err)
	}

	// Build remote set
	remoteSet := make(map[string]GlacierUTXO)
	for _, u := range glacierUTXOs {
		key := fmt.Sprintf("%s:%d", u.TxHash, u.OutputIndex)
		remoteSet[key] = u
	}

	// Parse target address
	targetAddr, err := address.ParseToID("P-" + addr)
	if err != nil {
		return 0, 0, fmt.Errorf("parse address: %w", err)
	}

	// Build local set
	localUTXOs := buildLocalUTXOSet(blocksDir, targetAddr)

	fmt.Printf("Glacier (remote): %d UTXOs\n", len(remoteSet))
	fmt.Printf("Local (blocks):   %d UTXOs\n", len(localUTXOs))

	// Find differences
	var onlyInGlacier []string
	for key, u := range remoteSet {
		if _, exists := localUTXOs[key]; !exists {
			onlyInGlacier = append(onlyInGlacier, fmt.Sprintf("  %s (amount: %s, staked: %v)",
				key, u.Amount, u.Staked))
			missingLocal++
		}
	}

	var onlyInLocal []string
	for key, u := range localUTXOs {
		if _, exists := remoteSet[key]; !exists {
			status := "unspent"
			if u.ConsumingTxID != ids.Empty {
				status = fmt.Sprintf("spent by %s", u.ConsumingTxID)
			}
			onlyInLocal = append(onlyInLocal, fmt.Sprintf("  %s (amount: %d, staked: %v, %s)",
				key, u.Amount, u.Staked, status))
			missingRemote++
		}
	}

	if len(onlyInGlacier) > 0 {
		fmt.Printf("\n❌ Missing in LOCAL (%d):\n", len(onlyInGlacier))
		for _, s := range onlyInGlacier {
			fmt.Println(s)
		}
	}

	if len(onlyInLocal) > 0 {
		fmt.Printf("\n❌ Missing in GLACIER (%d):\n", len(onlyInLocal))
		for _, s := range onlyInLocal {
			fmt.Println(s)
		}
	}

	return missingLocal, missingRemote, nil
}

func runStressTest(blocksDir string) {
	fmt.Println("=== UTXO STRESS TEST ===")
	fmt.Println("Fetching validator addresses from Glacier...")

	addrs, err := fetchValidatorAddresses()
	if err != nil {
		log.Fatalf("Failed to fetch validators: %v", err)
	}
	fmt.Printf("Found %d unique reward addresses\n\n", len(addrs))

	passed := 0
	failed := 0
	skipped := 0
	var failures []string

	for i, addr := range addrs {
		// Quick check for UTXOs first
		glacierUTXOs, err := getGlacierUTXOs([]string{addr}, true)
		if err != nil {
			failed++
			failures = append(failures, fmt.Sprintf("%s: glacier error: %v", addr, err))
			continue
		}

		if len(glacierUTXOs) == 0 {
			skipped++
			continue
		}

		// Parse and compare
		targetAddr, err := address.ParseToID("P-" + addr)
		if err != nil {
			failed++
			failures = append(failures, fmt.Sprintf("%s: parse error: %v", addr, err))
			continue
		}

		remoteSet := make(map[string]bool)
		for _, u := range glacierUTXOs {
			key := fmt.Sprintf("%s:%d", u.TxHash, u.OutputIndex)
			remoteSet[key] = true
		}

		localUTXOs := buildLocalUTXOSet(blocksDir, targetAddr)

		// Count differences
		missingLocal := 0
		for key := range remoteSet {
			if _, exists := localUTXOs[key]; !exists {
				missingLocal++
			}
		}

		missingRemote := 0
		for key := range localUTXOs {
			if _, exists := remoteSet[key]; !exists {
				missingRemote++
			}
		}

		if missingLocal == 0 && missingRemote == 0 {
			passed++
			fmt.Printf("[%d/%d] ✅ %s (%d UTXOs)\n", i+1, len(addrs), addr, len(remoteSet))
		} else {
			failed++
			failures = append(failures, fmt.Sprintf("%s: glacier=%d local=%d missing_local=%d missing_remote=%d",
				addr, len(remoteSet), len(localUTXOs), missingLocal, missingRemote))
			fmt.Printf("[%d/%d] ❌ %s (glacier=%d local=%d)\n", i+1, len(addrs), addr, len(remoteSet), len(localUTXOs))
		}
	}

	fmt.Println("\n=== RESULTS ===")
	fmt.Printf("Passed:  %d\n", passed)
	fmt.Printf("Failed:  %d\n", failed)
	fmt.Printf("Skipped: %d (no UTXOs)\n", skipped)

	if len(failures) > 0 {
		fmt.Println("\n=== FAILURES ===")
		for _, f := range failures {
			fmt.Println(f)
		}
	} else {
		fmt.Println("\n✅ All addresses with UTXOs matched!")
	}
}

func buildLocalUTXOSet(blocksDir string, targetAddr ids.ShortID) map[string]LocalUTXO {
	blocksDB, err := pebble.Open(blocksDir, &pebble.Options{
		Logger: noopLogger{},
	})
	if err != nil {
		log.Fatalf("Failed to open blocks DB: %v", err)
	}
	defer blocksDB.Close()

	localUTXOs := make(map[string]LocalUTXO)
	stakingTxs := make(map[ids.ID]bool)

	keyBlkPrefix := []byte("blk:")
	iter, err := blocksDB.NewIter(&pebble.IterOptions{LowerBound: keyBlkPrefix})
	if err != nil {
		log.Fatalf("Failed to create iterator: %v", err)
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		if string(key[:len(keyBlkPrefix)]) != string(keyBlkPrefix) {
			break
		}

		blk, err := block.Parse(block.Codec, iter.Value())
		if err != nil {
			continue
		}

		for _, tx := range blk.Txs() {
			unsigned := tx.Unsigned
			outs := unsigned.Outputs()

			var ins []*avax.TransferableInput
			var stakeOuts []*avax.TransferableOutput

			switch t := unsigned.(type) {
			case *txs.AddDelegatorTx:
				ins = t.Ins
				stakeOuts = t.StakeOuts
				trackStakingTx(t.StakeOuts, targetAddr, tx.ID(), stakingTxs)
			case *txs.AddValidatorTx:
				ins = t.Ins
				stakeOuts = t.StakeOuts
				trackStakingTx(t.StakeOuts, targetAddr, tx.ID(), stakingTxs)
			case *txs.AddPermissionlessDelegatorTx:
				ins = t.Ins
				stakeOuts = t.StakeOuts
				trackStakingTx(t.StakeOuts, targetAddr, tx.ID(), stakingTxs)
			case *txs.AddPermissionlessValidatorTx:
				ins = t.Ins
				stakeOuts = t.StakeOuts
				trackStakingTx(t.StakeOuts, targetAddr, tx.ID(), stakingTxs)
			case *txs.ImportTx:
				ins = t.Ins
				processImportTx(t, tx.ID(), outs, targetAddr, localUTXOs)
			case *txs.ExportTx:
				ins = t.Ins
				// ExportTx has ExportedOutputs which come after regular Outs
				// Process them with correct index offset
				processOutputs(t.ExportedOutputs, false, len(t.Outs), tx.ID(), targetAddr, localUTXOs)
			case *txs.CreateSubnetTx:
				ins = t.Ins
			case *txs.TransferSubnetOwnershipTx:
				ins = t.Ins
			case *txs.AddSubnetValidatorTx:
				ins = t.Ins
			case *txs.CreateChainTx:
				ins = t.Ins
			case *txs.BaseTx:
				ins = t.Ins
			case *txs.TransformSubnetTx:
				ins = t.Ins
			case *txs.RemoveSubnetValidatorTx:
				ins = t.Ins
			case *txs.ConvertSubnetToL1Tx:
				ins = t.Ins
			case *txs.RewardValidatorTx:
				processRewardTx(t, targetAddr, stakingTxs, localUTXOs)
			}

			// Consume inputs
			for _, in := range ins {
				utxoID := in.UTXOID.String()
				if u, exists := localUTXOs[utxoID]; exists {
					fmt.Printf("[PROTO CONSUME] utxoID=%s consumedBy=%s\n", utxoID, tx.ID())
					u.ConsumingTxID = tx.ID()
					localUTXOs[utxoID] = u
				}
			}

			// Process outputs
			processOutputs(outs, false, 0, tx.ID(), targetAddr, localUTXOs)
			processOutputs(stakeOuts, true, len(outs), tx.ID(), targetAddr, localUTXOs)
		}
	}

	return localUTXOs
}

func trackStakingTx(stakeOuts []*avax.TransferableOutput, targetAddr ids.ShortID, txID ids.ID, stakingTxs map[ids.ID]bool) {
	for _, out := range stakeOuts {
		if transferOut, ok := out.Out.(*secp256k1fx.TransferOutput); ok {
			for _, addr := range transferOut.Addrs {
				if addr == targetAddr {
					stakingTxs[txID] = true
					return
				}
			}
		}
	}
}

func processImportTx(t *txs.ImportTx, txID ids.ID, outs []*avax.TransferableOutput, targetAddr ids.ShortID, localUTXOs map[string]LocalUTXO) {
	hasOutputForTarget := false
	for _, out := range outs {
		if transferOut, ok := out.Out.(*secp256k1fx.TransferOutput); ok {
			for _, addr := range transferOut.Addrs {
				if addr == targetAddr {
					hasOutputForTarget = true
					break
				}
			}
		}
		if hasOutputForTarget {
			break
		}
	}

	if hasOutputForTarget {
		for _, importedIn := range t.ImportedInputs {
			utxoID := importedIn.UTXOID.String()
			amount := uint64(0)
			if secpIn, ok := importedIn.In.(*secp256k1fx.TransferInput); ok {
				amount = secpIn.Amount()
			}
			localUTXOs[utxoID] = LocalUTXO{
				TxID:          importedIn.UTXOID.TxID,
				OutputIndex:   importedIn.UTXOID.OutputIndex,
				Amount:        amount,
				Staked:        false,
				ConsumingTxID: txID,
			}
			fmt.Printf("[PROTO IMPORT] utxoID=%s amount=%d consumedBy=%s\n", utxoID, amount, txID)
		}
	}
}

func processRewardTx(t *txs.RewardValidatorTx, targetAddr ids.ShortID, stakingTxs map[ids.ID]bool, localUTXOs map[string]LocalUTXO) {
	if !stakingTxs[t.TxID] {
		return
	}

	ctx := context.Background()
	rewardUTXOBytes, err := cachedClient.GetRewardUTXOs(ctx, t.TxID.String())
	if err != nil {
		return
	}

	for _, utxoBytes := range rewardUTXOBytes {
		utxo := &avax.UTXO{}
		if _, err := txs.Codec.Unmarshal(utxoBytes, utxo); err != nil {
			continue
		}

		if transferOut, ok := utxo.Out.(*secp256k1fx.TransferOutput); ok {
			for _, addr := range transferOut.Addrs {
				if addr == targetAddr {
					utxoID := fmt.Sprintf("%s:%d", utxo.TxID, utxo.OutputIndex)
					fmt.Printf("[PROTO REWARD] utxoID=%s amount=%d\n", utxoID, transferOut.Amount())
					localUTXOs[utxoID] = LocalUTXO{
						TxID:        utxo.TxID,
						OutputIndex: utxo.OutputIndex,
						Amount:      transferOut.Amount(),
						AssetID:     utxo.AssetID(),
						Staked:      false,
					}
					break
				}
			}
		}
	}
}

func processOutputs(outputs []*avax.TransferableOutput, staked bool, startIdx int, txID ids.ID, targetAddr ids.ShortID, localUTXOs map[string]LocalUTXO) {
	for idx, out := range outputs {
		outputIdx := startIdx + idx
		if transferOut, ok := out.Out.(*secp256k1fx.TransferOutput); ok {
			for _, addr := range transferOut.Addrs {
				if addr == targetAddr {
					utxoID := fmt.Sprintf("%s:%d", txID, outputIdx)
					fmt.Printf("[PROTO CREATE] utxoID=%s amount=%d staked=%v\n", utxoID, transferOut.Amount(), staked)
					localUTXOs[utxoID] = LocalUTXO{
						TxID:        txID,
						OutputIndex: uint32(outputIdx),
						Amount:      transferOut.Amount(),
						AssetID:     out.AssetID(),
						Staked:      staked,
					}
					break
				}
			}
		}
	}
}

// noopLogger silences pebble's verbose logging
type noopLogger struct{}

func (noopLogger) Infof(format string, args ...interface{})  {}
func (noopLogger) Errorf(format string, args ...interface{}) {}
func (noopLogger) Fatalf(format string, args ...interface{}) {}
