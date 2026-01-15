package transform_test

import (
	"bufio"
	"bytes"
	"context"
	stdcsv "encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	csvutil "github.com/containerman17/l1-data-tools/exporters/snowflake/evm/internal/csv"
	"github.com/containerman17/l1-data-tools/exporters/snowflake/evm/pkg/transform"
	"github.com/containerman17/l1-data-tools/ingestion/evm/client"
	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/rpc"
	"github.com/klauspost/compress/zstd"
)

// Known chain IDs and their file prefixes
const (
	// C-Chain blockchain ID
	CChainID = "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5"
	// Gunzilla blockchain ID
	GunzillaChainID = "2M47TxWHGnhNtq6pM5zPXdATBtuqubxn5EPFgFmEawCQr9WFML"
)

// chainConfig holds test configuration for a specific chain
type chainConfig struct {
	chainID    string
	filePrefix string // e.g., "C_" or "GUNZILLA_"
}

var (
	testChainConfig *chainConfig
	configOnce      sync.Once
	configErr       error
)

// getChainConfig returns the chain configuration based on INGESTION_URL env var
func getChainConfig(t *testing.T) *chainConfig {
	t.Helper()
	configOnce.Do(func() {
		ingestionURL := os.Getenv("INGESTION_URL")
		if ingestionURL == "" {
			configErr = nil // No URL configured, will skip tests
			return
		}

		// Strip http:// prefix for client
		addr := strings.TrimPrefix(ingestionURL, "http://")
		addr = strings.TrimPrefix(addr, "https://")

		c := client.NewClient(addr)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		info, err := c.Info(ctx)
		if err != nil {
			configErr = err
			return
		}

		switch info.ChainID {
		case CChainID:
			testChainConfig = &chainConfig{
				chainID:    CChainID,
				filePrefix: "C_",
			}
		case GunzillaChainID:
			testChainConfig = &chainConfig{
				chainID:    GunzillaChainID,
				filePrefix: "GUNZILLA_",
			}
		default:
			t.Logf("Unknown chain ID: %s", info.ChainID)
			configErr = nil // Unknown chain, will skip
		}
	})

	if configErr != nil {
		t.Fatalf("failed to get chain config: %v", configErr)
	}

	return testChainConfig
}

// testDataDir returns the path to the test assets directory.
func testDataDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "notes", "assets")
}

// loadGoldenBlocks loads NormalizedBlocks from the zst-compressed JSONL file.
func loadGoldenBlocks(t *testing.T, prefix string) []rpc.NormalizedBlock {
	t.Helper()

	dataPath := filepath.Join(testDataDir(), prefix+"BLOCKS.jsonl.zst")
	compressed, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatalf("failed to read golden data from %s: %v", dataPath, err)
	}

	decoder, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatalf("failed to create zstd reader: %v", err)
	}
	defer decoder.Close()

	decompressed, err := decoder.DecodeAll(compressed, nil)
	if err != nil {
		t.Fatalf("failed to decompress golden data: %v", err)
	}

	var blocks []rpc.NormalizedBlock
	scanner := bufio.NewScanner(bytes.NewReader(decompressed))
	// Increase buffer size for large JSON lines
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var nb rpc.NormalizedBlock
		if err := json.Unmarshal(scanner.Bytes(), &nb); err != nil {
			t.Fatalf("failed to unmarshal block: %v", err)
		}
		blocks = append(blocks, nb)
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	return blocks
}

// loadGoldenCSVRows loads a zstd-compressed golden CSV file, decompresses it in memory,
// and returns rows as a set of strings for easy comparison.
func loadGoldenCSVRows(t *testing.T, filename string) map[string]bool {
	t.Helper()
	path := filepath.Join(testDataDir(), filename)
	compressed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read golden CSV %s: %v", filename, err)
	}

	decoder, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatalf("failed to create zstd reader: %v", err)
	}
	defer decoder.Close()

	decompressed, err := decoder.DecodeAll(compressed, nil)
	if err != nil {
		t.Fatalf("failed to decompress golden CSV %s: %v", filename, err)
	}

	reader := stdcsv.NewReader(bytes.NewReader(decompressed))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to parse golden CSV %s: %v", filename, err)
	}

	rows := make(map[string]bool)
	for i, record := range records {
		if i == 0 {
			continue // Skip header
		}
		// Normalize values
		for j := range record {
			record[j] = normalizeCSVValue(record[j])
		}
		rows[strings.Join(record, ",")] = true
	}

	return rows
}

// normalizeCSVValue normalizes a CSV value for comparison.
func normalizeCSVValue(s string) string {
	// Lowercase addresses for comparison
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		return strings.ToLower(s)
	}
	// Convert "true"/"false" to lowercase
	if s == "TRUE" || s == "True" {
		return "true"
	}
	if s == "FALSE" || s == "False" {
		return "false"
	}
	return s
}

// csvRowsToSet converts generated CSV output to a set of row strings.
func csvRowsToSet(csvContent string) map[string]bool {
	reader := stdcsv.NewReader(strings.NewReader(csvContent))
	records, _ := reader.ReadAll()

	rows := make(map[string]bool)
	for i, record := range records {
		if i == 0 {
			continue // Skip header
		}
		for j := range record {
			record[j] = normalizeCSVValue(record[j])
		}
		rows[strings.Join(record, ",")] = true
	}

	return rows
}

// compareRowSets compares two row sets and reports differences.
func compareRowSets(t *testing.T, name string, got, want map[string]bool) {
	t.Helper()

	// Find missing rows (in want but not in got)
	var missing []string
	for row := range want {
		if !got[row] {
			missing = append(missing, row)
		}
	}

	// Find extra rows (in got but not in want)
	var extra []string
	for row := range got {
		if !want[row] {
			extra = append(extra, row)
		}
	}

	if len(missing) > 0 || len(extra) > 0 {
		sort.Strings(missing)
		sort.Strings(extra)

		if len(missing) > 0 {
			t.Errorf("%s: missing %d rows", name, len(missing))
			for i, row := range missing {
				if i >= 3 {
					t.Errorf("  ... and %d more", len(missing)-3)
					break
				}
				t.Errorf("  missing: %s", truncate(row, 1000))
			}
		}

		if len(extra) > 0 {
			t.Errorf("%s: extra %d rows", name, len(extra))
			for i, row := range extra {
				if i >= 3 {
					t.Errorf("  ... and %d more", len(extra)-3)
					break
				}
				t.Errorf("  extra: %s", truncate(row, 1000))
			}
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func TestTransformBlocks(t *testing.T) {
	cfg := getChainConfig(t)
	if cfg == nil {
		t.Skip("no chain configured (set INGESTION_URL or chain has no golden files)")
	}

	blocks := loadGoldenBlocks(t, cfg.filePrefix)
	batch := transform.Transform(blocks)

	var buf bytes.Buffer
	if err := csvutil.Write(&buf, batch.Blocks); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	got := csvRowsToSet(buf.String())
	want := loadGoldenCSVRows(t, cfg.filePrefix+"BLOCKS.csv.zst")

	compareRowSets(t, "blocks", got, want)
}

func TestTransformTransactions(t *testing.T) {
	cfg := getChainConfig(t)
	if cfg == nil {
		t.Skip("no chain configured (set INGESTION_URL or chain has no golden files)")
	}

	blocks := loadGoldenBlocks(t, cfg.filePrefix)
	batch := transform.Transform(blocks)

	var buf bytes.Buffer
	if err := csvutil.Write(&buf, batch.Transactions); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	got := csvRowsToSet(buf.String())
	want := loadGoldenCSVRows(t, cfg.filePrefix+"TRANSACTIONS.csv.zst")

	compareRowSets(t, "transactions", got, want)
}

func TestTransformReceipts(t *testing.T) {
	cfg := getChainConfig(t)
	if cfg == nil {
		t.Skip("no chain configured (set INGESTION_URL or chain has no golden files)")
	}

	blocks := loadGoldenBlocks(t, cfg.filePrefix)
	batch := transform.Transform(blocks)

	var buf bytes.Buffer
	if err := csvutil.Write(&buf, batch.Receipts); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	got := csvRowsToSet(buf.String())
	want := loadGoldenCSVRows(t, cfg.filePrefix+"RECEIPTS.csv.zst")

	compareRowSets(t, "receipts", got, want)
}

func TestTransformLogs(t *testing.T) {
	cfg := getChainConfig(t)
	if cfg == nil {
		t.Skip("no chain configured (set INGESTION_URL or chain has no golden files)")
	}

	blocks := loadGoldenBlocks(t, cfg.filePrefix)
	batch := transform.Transform(blocks)

	var buf bytes.Buffer
	if err := csvutil.Write(&buf, batch.Logs); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	got := csvRowsToSet(buf.String())
	want := loadGoldenCSVRows(t, cfg.filePrefix+"LOGS.csv.zst")

	compareRowSets(t, "logs", got, want)
}

func TestTransformInternalTxs(t *testing.T) {
	cfg := getChainConfig(t)
	if cfg == nil {
		t.Skip("no chain configured (set INGESTION_URL or chain has no golden files)")
	}

	blocks := loadGoldenBlocks(t, cfg.filePrefix)
	batch := transform.Transform(blocks)

	var buf bytes.Buffer
	if err := csvutil.Write(&buf, batch.InternalTxs); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	got := csvRowsToSet(buf.String())
	want := loadGoldenCSVRows(t, cfg.filePrefix+"INTERNAL_TRANSACTIONS.csv.zst")

	compareRowSets(t, "internal_txs", got, want)
}

func TestTransformMessages(t *testing.T) {
	cfg := getChainConfig(t)
	if cfg == nil {
		t.Skip("no chain configured (set INGESTION_URL or chain has no golden files)")
	}

	blocks := loadGoldenBlocks(t, cfg.filePrefix)
	batch := transform.Transform(blocks)

	var buf bytes.Buffer
	if err := csvutil.Write(&buf, batch.Messages); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	got := csvRowsToSet(buf.String())
	want := loadGoldenCSVRows(t, cfg.filePrefix+"MESSAGES.csv.zst")

	compareRowSets(t, "messages", got, want)
}
