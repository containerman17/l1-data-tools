package transform_test

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	csvutil "github.com/containerman17/l1-data-tools/exporters/snowflake/evm/internal/csv"
	"github.com/containerman17/l1-data-tools/exporters/snowflake/evm/pkg/transform"
	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/rpc"
	"github.com/klauspost/compress/zstd"
)

// testDataDir returns the path to the test assets directory.
func testDataDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "notes", "assets")
}

// loadGoldenBlocks loads NormalizedBlocks from the zst-compressed JSONL file.
func loadGoldenBlocks(t *testing.T) []rpc.NormalizedBlock {
	t.Helper()

	dataPath := filepath.Join(testDataDir(), "blocks_1_100.zst")
	compressed, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatalf("failed to read golden data: %v", err)
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

// loadGoldenCSVRows loads a golden CSV file and returns rows as a set of strings.
// Each row is joined by a delimiter for easy comparison.
func loadGoldenCSVRows(t *testing.T, filename string) map[string]bool {
	t.Helper()
	path := filepath.Join(testDataDir(), filename)
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open golden CSV %s: %v", filename, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read golden CSV %s: %v", filename, err)
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
	reader := csv.NewReader(strings.NewReader(csvContent))
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
				t.Errorf("  missing: %s", truncate(row, 200))
			}
		}

		if len(extra) > 0 {
			t.Errorf("%s: extra %d rows", name, len(extra))
			for i, row := range extra {
				if i >= 3 {
					t.Errorf("  ... and %d more", len(extra)-3)
					break
				}
				t.Errorf("  extra: %s", truncate(row, 200))
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
	blocks := loadGoldenBlocks(t)
	batch := transform.Transform(blocks)

	var buf bytes.Buffer
	if err := csvutil.Write(&buf, batch.Blocks); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	got := csvRowsToSet(buf.String())
	want := loadGoldenCSVRows(t, "C_BLOCKS_1_100.csv")

	compareRowSets(t, "blocks", got, want)
}

func TestTransformTransactions(t *testing.T) {
	blocks := loadGoldenBlocks(t)
	batch := transform.Transform(blocks)

	var buf bytes.Buffer
	if err := csvutil.Write(&buf, batch.Transactions); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	got := csvRowsToSet(buf.String())
	want := loadGoldenCSVRows(t, "C_TRANSACTIONS_1_100.csv")

	compareRowSets(t, "transactions", got, want)
}

func TestTransformReceipts(t *testing.T) {
	blocks := loadGoldenBlocks(t)
	batch := transform.Transform(blocks)

	var buf bytes.Buffer
	if err := csvutil.Write(&buf, batch.Receipts); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	got := csvRowsToSet(buf.String())
	want := loadGoldenCSVRows(t, "C_RECEIPTS_1_100.csv")

	compareRowSets(t, "receipts", got, want)
}

func TestTransformLogs(t *testing.T) {
	blocks := loadGoldenBlocks(t)
	batch := transform.Transform(blocks)

	var buf bytes.Buffer
	if err := csvutil.Write(&buf, batch.Logs); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	got := csvRowsToSet(buf.String())
	want := loadGoldenCSVRows(t, "C_LOGS_1_100.csv")

	compareRowSets(t, "logs", got, want)
}

func TestTransformInternalTxs(t *testing.T) {
	blocks := loadGoldenBlocks(t)
	batch := transform.Transform(blocks)

	var buf bytes.Buffer
	if err := csvutil.Write(&buf, batch.InternalTxs); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	got := csvRowsToSet(buf.String())
	want := loadGoldenCSVRows(t, "C_INTERNAL_TRANSACTIONS_1_100.csv")

	compareRowSets(t, "internal_txs", got, want)
}

func TestTransformMessages(t *testing.T) {
	blocks := loadGoldenBlocks(t)
	batch := transform.Transform(blocks)

	var buf bytes.Buffer
	if err := csvutil.Write(&buf, batch.Messages); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	got := csvRowsToSet(buf.String())
	want := loadGoldenCSVRows(t, "C_MESSAGES_1_100.csv")

	compareRowSets(t, "messages", got, want)
}
