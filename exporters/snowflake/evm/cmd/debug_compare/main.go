package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	csvutil "github.com/containerman17/l1-data-tools/exporters/snowflake/evm/internal/csv"
	"github.com/containerman17/l1-data-tools/exporters/snowflake/evm/pkg/transform"
	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/rpc"
	"github.com/klauspost/compress/zstd"
)

func main() {
	assetsDir := "notes/assets"

	// Load all blocks
	compressed, _ := os.ReadFile(filepath.Join(assetsDir, "blocks_1_100.zst"))
	decoder, _ := zstd.NewReader(nil)
	defer decoder.Close()
	decompressed, _ := decoder.DecodeAll(compressed, nil)

	var blocks []rpc.NormalizedBlock
	scanner := bufio.NewScanner(bytes.NewReader(decompressed))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		var nb rpc.NormalizedBlock
		json.Unmarshal(scanner.Bytes(), &nb)
		blocks = append(blocks, nb)
	}

	// Transform
	batch := transform.Transform(blocks)

	// === Debug the remaining transaction ===
	fmt.Println("=== TRANSACTION DEBUG (tx 0x580bb509...) ===")
	{
		var buf bytes.Buffer
		csvutil.Write(&buf, batch.Transactions)
		genReader := csv.NewReader(&buf)
		genRecords, _ := genReader.ReadAll()

		file, _ := os.Open(filepath.Join(assetsDir, "C_TRANSACTIONS_1_100.csv"))
		defer file.Close()
		goldReader := csv.NewReader(file)
		goldRecords, _ := goldReader.ReadAll()

		targetHash := "0x580bb509cc234b640920a0e07f817e6724ad1d0722361e65b91ceeee1a01ec26"

		for _, row := range genRecords[1:] {
			if strings.ToLower(row[3]) == strings.ToLower(targetHash) {
				fmt.Println("Generated row:")
				for i, v := range row {
					fmt.Printf("  %d %s: '%s'\n", i, goldRecords[0][i], v)
				}
			}
		}

		for _, row := range goldRecords[1:] {
			if strings.ToLower(row[3]) == strings.ToLower(targetHash) {
				fmt.Println("Golden row:")
				for i, v := range row {
					fmt.Printf("  %d %s: '%s'\n", i, goldRecords[0][i], v)
				}
			}
		}
	}

	// === Debug internal transactions ===
	fmt.Println("\n=== INTERNAL_TXS DEBUG ===")
	{
		var buf bytes.Buffer
		csvutil.Write(&buf, batch.InternalTxs)
		genReader := csv.NewReader(&buf)
		genRecords, _ := genReader.ReadAll()

		file, _ := os.Open(filepath.Join(assetsDir, "C_INTERNAL_TRANSACTIONS_1_100.csv"))
		defer file.Close()
		goldReader := csv.NewReader(file)
		goldRecords, _ := goldReader.ReadAll()

		fmt.Println("Headers comparison:")
		for i, h := range genRecords[0] {
			gh := goldRecords[0][i]
			if h != gh {
				fmt.Printf("  DIFF [%d]: gen='%s' gold='%s'\n", i, h, gh)
			}
		}

		// Find first tx hash match and compare
		genMap := make(map[string][]string)
		for _, row := range genRecords[1:] {
			key := strings.ToLower(row[3]) + "|" + row[15] // txhash + callindex
			genMap[key] = row
		}
		goldMap := make(map[string][]string)
		for _, row := range goldRecords[1:] {
			key := strings.ToLower(row[3]) + "|" + row[15]
			goldMap[key] = row
		}

		count := 0
		for key, gen := range genMap {
			gold, found := goldMap[key]
			if !found {
				fmt.Printf("\nKey not found in golden: %s\n", key)
				count++
				if count >= 3 {
					break
				}
				continue
			}

			hasDiff := false
			for i := range gen {
				genVal := strings.ToLower(gen[i])
				goldVal := strings.ToLower(gold[i])
				if genVal != goldVal {
					if !hasDiff {
						fmt.Printf("\nKey %s differences:\n", key)
						hasDiff = true
					}
					fmt.Printf("  DIFF [%d] %s:\n    GEN:  '%s'\n    GOLD: '%s'\n", i, goldRecords[0][i], gen[i], gold[i])
				}
			}

			if hasDiff {
				count++
				if count >= 3 {
					break
				}
			}
		}
	}
}
