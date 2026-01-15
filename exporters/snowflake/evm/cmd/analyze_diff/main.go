// analyze_tx_diff analyzes differences between generated and golden transaction data
package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/containerman17/l1-data-tools/exporters/snowflake/evm/pkg/transform"
	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/rpc"
	"github.com/klauspost/compress/zstd"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	assetsDir := getAssetsDir()

	// Load golden CSV
	fmt.Println("Loading golden transactions...")
	goldenRows, err := loadGoldenCSV(filepath.Join(assetsDir, "GUNZILLA_TRANSACTIONS.csv.zst"))
	if err != nil {
		return fmt.Errorf("load golden: %w", err)
	}
	fmt.Printf("Loaded %d golden rows\n", len(goldenRows))

	// Load blocks and generate our data
	fmt.Println("Loading blocks and generating transactions...")
	blocks, err := loadBlocks(filepath.Join(assetsDir, "GUNZILLA_BLOCKS.jsonl.zst"))
	if err != nil {
		return fmt.Errorf("load blocks: %w", err)
	}

	batch := transform.Transform(blocks)
	fmt.Printf("Generated %d transaction rows\n", len(batch.Transactions))

	// Build map of generated rows by transaction hash
	generatedByHash := make(map[string]transform.TransactionRow)
	for _, row := range batch.Transactions {
		generatedByHash[strings.ToLower(row.TransactionHash)] = row
	}

	// Analyze differences
	var diffs []Difference
	var matched, missing, extra int

	for _, golden := range goldenRows {
		txHash := strings.ToLower(golden["TRANSACTIONHASH"])
		gen, ok := generatedByHash[txHash]
		if !ok {
			missing++
			continue
		}
		delete(generatedByHash, txHash)

		// Compare TransactionCost
		goldenCost := golden["TRANSACTIONCOST"]
		genCost := gen.TransactionCost

		if goldenCost != genCost {
			diff := compareBigInts(goldenCost, genCost)
			diff.TxHash = txHash
			diff.BlockNumber = gen.BlockNumber
			diff.Field = "TransactionCost"
			diffs = append(diffs, diff)
		} else {
			matched++
		}
	}
	extra = len(generatedByHash)

	fmt.Println("\n=== TRANSACTION COMPARISON RESULTS ===")
	fmt.Printf("Total golden rows: %d\n", len(goldenRows))
	fmt.Printf("Matched exactly: %d\n", matched)
	fmt.Printf("With differences: %d\n", len(diffs))
	fmt.Printf("Missing from generated: %d\n", missing)
	fmt.Printf("Extra in generated: %d\n", extra)

	if len(diffs) == 0 {
		fmt.Println("\nNo differences found!")
		return nil
	}

	// Calculate statistics
	fmt.Println("\n=== TRANSACTIONCOST DIFFERENCE STATISTICS ===")

	// Sort by absolute difference
	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].AbsDiff.Cmp(diffs[j].AbsDiff) < 0
	})

	fmt.Printf("Total rows with TransactionCost diff: %d\n", len(diffs))
	fmt.Printf("\nSmallest difference:\n")
	printDiff(diffs[0])
	fmt.Printf("\nLargest difference:\n")
	printDiff(diffs[len(diffs)-1])

	// Median
	medianIdx := len(diffs) / 2
	fmt.Printf("\nMedian difference:\n")
	printDiff(diffs[medianIdx])

	// Distribution
	fmt.Println("\n=== DIFFERENCE DISTRIBUTION ===")
	var under10, under100, under1000, under10000, over10000 int
	for _, d := range diffs {
		absVal := d.AbsDiff.Int64()
		switch {
		case absVal < 10:
			under10++
		case absVal < 100:
			under100++
		case absVal < 1000:
			under1000++
		case absVal < 10000:
			over10000++
		default:
			over10000++
		}
	}
	fmt.Printf("< 10 wei:     %d (%.1f%%)\n", under10, pct(under10, len(diffs)))
	fmt.Printf("10-99 wei:    %d (%.1f%%)\n", under100, pct(under100, len(diffs)))
	fmt.Printf("100-999 wei:  %d (%.1f%%)\n", under1000, pct(under1000, len(diffs)))
	fmt.Printf("1000-9999 wei: %d (%.1f%%)\n", under10000, pct(under10000, len(diffs)))
	fmt.Printf(">= 10000 wei: %d (%.1f%%)\n", over10000, pct(over10000, len(diffs)))

	// Show top 5 LARGEST differences
	fmt.Println("\n=== TOP 5 LARGEST DIFFERENCES ===")
	for i := len(diffs) - 1; i >= 0 && i >= len(diffs)-5; i-- {
		fmt.Printf("\n--- #%d ---\n", len(diffs)-i)
		printDiff(diffs[i])
	}

	// Check if golden is always greater or less
	var goldenGreater, goldenLess int
	for _, d := range diffs {
		if d.Diff.Sign() > 0 {
			goldenGreater++
		} else {
			goldenLess++
		}
	}
	fmt.Println("\n=== DIRECTION OF ERROR ===")
	fmt.Printf("Golden > Generated: %d (%.1f%%)\n", goldenGreater, pct(goldenGreater, len(diffs)))
	fmt.Printf("Golden < Generated: %d (%.1f%%)\n", goldenLess, pct(goldenLess, len(diffs)))

	return nil
}

type Difference struct {
	TxHash      string
	BlockNumber int64
	Field       string
	Golden      string
	Generated   string
	Diff        *big.Int // Golden - Generated
	AbsDiff     *big.Int
	PctDiff     float64
}

func compareBigInts(golden, generated string) Difference {
	g := new(big.Int)
	g.SetString(golden, 10)

	gen := new(big.Int)
	gen.SetString(generated, 10)

	diff := new(big.Int).Sub(g, gen)
	absDiff := new(big.Int).Abs(diff)

	var pctDiff float64
	if gen.Sign() != 0 {
		// Calculate percentage: (diff / generated) * 100
		diffFloat := new(big.Float).SetInt(absDiff)
		genFloat := new(big.Float).SetInt(gen)
		pct := new(big.Float).Quo(diffFloat, genFloat)
		pct.Mul(pct, big.NewFloat(100))
		pctDiff, _ = pct.Float64()
	}

	return Difference{
		Golden:    golden,
		Generated: generated,
		Diff:      diff,
		AbsDiff:   absDiff,
		PctDiff:   pctDiff,
	}
}

func printDiff(d Difference) {
	fmt.Printf("  TxHash: %s\n", d.TxHash)
	fmt.Printf("  Block: %d\n", d.BlockNumber)
	fmt.Printf("  Golden:    %s\n", d.Golden)
	fmt.Printf("  Generated: %s\n", d.Generated)
	fmt.Printf("  Diff: %s wei (%.10f%%)\n", d.Diff.String(), d.PctDiff)
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}

func getAssetsDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "notes", "assets")
}

func loadGoldenCSV(path string) ([]map[string]string, error) {
	compressed, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer decoder.Close()

	decompressed, err := decoder.DecodeAll(compressed, nil)
	if err != nil {
		return nil, err
	}

	reader := csv.NewReader(bytes.NewReader(decompressed))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("no data rows")
	}

	headers := records[0]
	var rows []map[string]string
	for _, record := range records[1:] {
		row := make(map[string]string)
		for i, val := range record {
			row[headers[i]] = val
		}
		rows = append(rows, row)
	}

	return rows, nil
}

func loadBlocks(path string) ([]rpc.NormalizedBlock, error) {
	compressed, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer decoder.Close()

	decompressed, err := decoder.DecodeAll(compressed, nil)
	if err != nil {
		return nil, err
	}

	var blocks []rpc.NormalizedBlock
	scanner := bufio.NewScanner(bytes.NewReader(decompressed))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var nb rpc.NormalizedBlock
		if err := json.Unmarshal(scanner.Bytes(), &nb); err != nil {
			return nil, err
		}
		blocks = append(blocks, nb)
	}

	return blocks, scanner.Err()
}
