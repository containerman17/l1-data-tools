// Package selftest provides comparison testing against Glacier API.
package selftest

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
	"gopkg.in/yaml.v3"
)

const GlacierBase = "https://glacier-api.avax.network"

// DiffData holds metadata and diff for a test case
type DiffData struct {
	TestCase     string
	GlacierURL   string
	LocalURL     string
	ResponseTime int64
	Glacier      map[string]any
	Local        map[string]any
}

// edit represents a single line in a diff
type edit struct {
	op   byte // ' ', '-', '+'
	line string
}

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBold   = "\033[1m"
)

// RunTests runs test cases against localhost and Glacier.
// Returns true if all pass.
func RunTests(localBase string, tests []indexer.TestCase) bool {
	if len(tests) == 0 {
		fmt.Println("No test cases.")
		return true
	}

	// Check if any test has Only: true
	var onlyTests []indexer.TestCase
	for _, tc := range tests {
		if tc.Only {
			onlyTests = append(onlyTests, tc)
		}
	}

	if len(onlyTests) > 0 {
		fmt.Printf("Running %d 'Only' test cases (out of %d total)...\n\n", len(onlyTests), len(tests))
		tests = onlyTests
	} else {
		fmt.Printf("Running %d test cases...\n\n", len(tests))
	}

	passed, failed := 0, 0
	for _, tc := range tests {
		if runTestCase(tc, localBase) {
			passed++
		} else {
			failed++
		}
		time.Sleep(1000 * time.Millisecond) // Rate limit
	}

	fmt.Printf("\n=== %sSUMMARY%s ===\n", colorBold, colorReset)
	fmt.Printf("Passed: %s%d%s, Failed: %s%d%s\n",
		colorGreen, passed, colorReset,
		map[bool]string{true: colorRed, false: colorReset}[failed > 0], failed, colorReset)

	return failed == 0
}

func runTestCase(tc indexer.TestCase, localBase string) bool {
	// Skip tests marked with Skip field
	if tc.Skip {
		fmt.Printf("%-40s - %s⚠️  SKIPPED%s\n", tc.Name, colorYellow, colorReset)
		return true
	}

	localURL := buildURL(localBase, tc.Path, tc.Params)

	// LocalOnly tests skip Glacier comparison
	if tc.LocalOnly {
		start := time.Now()
		localResp, localSize, localErr := fetchJSON(localURL)
		elapsed := time.Since(start)

		if localErr != nil {
			fmt.Printf("%-40s - %s❌ Local ERROR%s (%dms, %d bytes)\n", tc.Name, colorRed, colorReset, elapsed.Milliseconds(), localSize)
			fmt.Printf("  Local: %s\n", localURL)
			fmt.Printf("  Error: %v\n", localErr)
			return false
		}

		if tc.MaxTimeMs > 0 && elapsed.Milliseconds() > int64(tc.MaxTimeMs) {
			fmt.Printf("%-40s - %s❌ TOO SLOW%s (%dms > %dms, %d bytes)\n", tc.Name, colorRed, colorReset, elapsed.Milliseconds(), tc.MaxTimeMs, localSize)
			fmt.Printf("  Local: %s\n", localURL)
			return false
		}

		if localResp == nil {
			fmt.Printf("%-40s - %s❌ Empty response%s (%d bytes)\n", tc.Name, colorRed, colorReset, localSize)
			fmt.Printf("  Local: %s\n", localURL)
			return false
		}

		fmt.Printf("%-40s - %s✅ OK%s (%dms, %d bytes)\n", tc.Name, colorGreen, colorReset, elapsed.Milliseconds(), localSize)
		return true
	}

	// Standard Glacier comparison test
	glacierURL := buildURL(GlacierBase, tc.Path, tc.Params)

	// Fetch both in parallel
	var glacierResp, localResp map[string]any
	var glacierErr, localErr error
	var glacierSize, localSize int
	var localElapsed time.Duration
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		glacierResp, glacierSize, glacierErr = fetchJSON(glacierURL)
	}()
	go func() {
		defer wg.Done()
		start := time.Now()
		localResp, localSize, localErr = fetchJSON(localURL)
		localElapsed = time.Since(start)
	}()
	wg.Wait()

	if glacierErr != nil {
		fmt.Printf("%-40s - %s❌ Glacier ERROR%s (%d bytes)\n", tc.Name, colorRed, colorReset, glacierSize)
		fmt.Printf("  Glacier: %s\n", glacierURL)
		fmt.Printf("  Local:   %s\n", localURL)
		fmt.Printf("  Error:   %v\n", glacierErr)
		return false
	}
	if localErr != nil {
		fmt.Printf("%-40s - %s❌ Local ERROR%s (%dms, %d bytes)\n", tc.Name, colorRed, colorReset, localElapsed.Milliseconds(), localSize)
		fmt.Printf("  Glacier: %s\n", glacierURL)
		fmt.Printf("  Local:   %s\n", localURL)
		fmt.Printf("  Error:   %v\n", localErr)
		return false
	}

	if tc.MaxTimeMs > 0 && localElapsed.Milliseconds() > int64(tc.MaxTimeMs) {
		fmt.Printf("%-40s - %s❌ TOO SLOW%s (%dms > %dms, %d bytes)\n", tc.Name, colorRed, colorReset, localElapsed.Milliseconds(), tc.MaxTimeMs, localSize)
		fmt.Printf("  Glacier: %s\n", glacierURL)
		fmt.Printf("  Local:   %s\n", localURL)
		return false
	}

	if tc.FilterGlacier != nil {
		glacierResp = tc.FilterGlacier(glacierResp)
	}

	// Compare
	skipSet := make(map[string]bool)
	for _, f := range tc.SkipFields {
		skipSet[f] = true
	}

	approxFields := tc.ApproxFields
	if approxFields == nil {
		approxFields = make(map[string]float64)
	}

	diffs := compareResponses(glacierResp, localResp, skipSet, approxFields)
	if len(diffs) == 0 {
		fmt.Printf("%-40s - %s✅ MATCH%s (%dms, %d bytes)\n", tc.Name, colorGreen, colorReset, localElapsed.Milliseconds(), localSize)
		return true
	}

	// Write diff file
	diffData := DiffData{
		TestCase:     tc.Name,
		GlacierURL:   glacierURL,
		LocalURL:     localURL,
		ResponseTime: localElapsed.Milliseconds(),
		Glacier:      glacierResp,
		Local:        localResp,
	}

	diffPath, glacierPath, localPath, err := writeDiffFile(diffData)
	if err != nil {
		fmt.Printf("%-40s - %s❌ DIFFERENCES%s (%dms, %d bytes)\n", tc.Name, colorRed, colorReset, localElapsed.Milliseconds(), localSize)
		fmt.Printf("  Glacier: %s\n", glacierURL)
		fmt.Printf("  Local:   %s\n", localURL)
		fmt.Printf("  ❌ Failed to write diff file: %v\n", err)
		return false
	}

	fmt.Printf("%-40s - %s❌ DIFFERENCES%s (%dms, %d bytes)\n", tc.Name, colorRed, colorReset, localElapsed.Milliseconds(), localSize)
	fmt.Printf("  Glacier: %s\n", glacierURL)
	fmt.Printf("  Local:   %s\n", localURL)
	fmt.Printf("  ❌ DIFFERENCES (%dms, %d bytes) - Diff file: %s\n", localElapsed.Milliseconds(), localSize, diffPath)
	fmt.Printf("Glacier YAML: %s\n", glacierPath)
	fmt.Printf("Local YAML:   %s\n", localPath)
	return false
}

func buildURL(base, path string, params map[string]string) string {
	u, _ := url.Parse(base + path)
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func fetchJSON(url string) (map[string]any, int, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	if resp.StatusCode != 200 {
		return nil, len(body), fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, len(body), err
	}

	return result, len(body), nil
}

func compareResponses(glacier, local map[string]any, skipFields map[string]bool, approxFields map[string]float64) []string {
	var diffs []string

	for field := range skipFields {
		delete(glacier, field)
		delete(local, field)
	}

	allKeys := make(map[string]bool)
	for k := range glacier {
		allKeys[k] = true
	}
	for k := range local {
		allKeys[k] = true
	}

	for k := range allKeys {
		gVal, gOk := glacier[k]
		lVal, lOk := local[k]

		if !gOk {
			diffs = append(diffs, fmt.Sprintf("field '%s': missing in Glacier", k))
			continue
		}
		if !lOk {
			diffs = append(diffs, fmt.Sprintf("field '%s': missing in Local", k))
			continue
		}

		fieldDiffs := compareValues(k, gVal, lVal, skipFields, approxFields)
		diffs = append(diffs, fieldDiffs...)
	}

	return diffs
}

func compareValues(path string, gVal, lVal any, skipFields map[string]bool, approxFields map[string]float64) []string {
	var diffs []string

	if skipFields[path] {
		return nil
	}

	for pattern, tolerance := range approxFields {
		if strings.HasSuffix(path, pattern) {
			return compareApprox(path, gVal, lVal, tolerance)
		}
	}

	gArr, gIsArr := gVal.([]any)
	lArr, lIsArr := lVal.([]any)

	if gIsArr && lIsArr {
		if len(gArr) != len(lArr) {
			diffs = append(diffs, fmt.Sprintf("%s: array length mismatch (Glacier=%d, Local=%d)", path, len(gArr), len(lArr)))
		}

		gMap := arrayToMap(gArr)
		lMap := arrayToMap(lArr)

		for id, gItem := range gMap {
			lItem, ok := lMap[id]
			if !ok {
				if len(diffs) < 10 {
					diffs = append(diffs, fmt.Sprintf("%s: item %s only in Glacier", path, truncateID(id)))
				}
				continue
			}
			itemDiffs := compareMapDeep(fmt.Sprintf("%s[%s]", path, truncateID(id)), gItem, lItem, skipFields, approxFields)
			diffs = append(diffs, itemDiffs...)
		}

		for id := range lMap {
			if _, ok := gMap[id]; !ok {
				if len(diffs) < 10 {
					diffs = append(diffs, fmt.Sprintf("%s: item %s only in Local", path, truncateID(id)))
				}
			}
		}
		return diffs
	}

	gMap, gIsMap := gVal.(map[string]any)
	lMap, lIsMap := lVal.(map[string]any)

	if gIsMap && lIsMap {
		return compareMapDeep(path, gMap, lMap, skipFields, approxFields)
	}

	if !reflect.DeepEqual(gVal, lVal) {
		diffs = append(diffs, fmt.Sprintf("%s: Glacier=%v, Local=%v", path, gVal, lVal))
	}

	return diffs
}

func compareMapDeep(path string, gMap, lMap map[string]any, skipFields map[string]bool, approxFields map[string]float64) []string {
	var diffs []string

	allKeys := make(map[string]bool)
	for k := range gMap {
		allKeys[k] = true
	}
	for k := range lMap {
		allKeys[k] = true
	}

	keys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if skipFields[k] {
			continue
		}

		gVal, gOk := gMap[k]
		lVal, lOk := lMap[k]

		fieldPath := path + "." + k

		if !gOk && lOk {
			diffs = append(diffs, fmt.Sprintf("%s: missing in Glacier", fieldPath))
			continue
		}
		if gOk && !lOk {
			diffs = append(diffs, fmt.Sprintf("%s: missing in Local", fieldPath))
			continue
		}

		subDiffs := compareValues(fieldPath, gVal, lVal, skipFields, approxFields)
		diffs = append(diffs, subDiffs...)
	}

	return diffs
}

func arrayToMap(arr []any) map[string]map[string]any {
	result := make(map[string]map[string]any)
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := m["utxoId"].(string); ok {
			result[id] = m
		} else if txHash, ok := m["txHash"].(string); ok {
			if rewardType, ok := m["rewardType"].(string); ok {
				result[txHash+":"+rewardType] = m
			} else {
				result[txHash] = m
			}
		}
	}
	return result
}

func truncateID(id string) string {
	if len(id) > 12 {
		return id[:12] + "..."
	}
	return id
}

func compareApprox(path string, gVal, lVal any, tolerance float64) []string {
	gFloat := toFloat64(gVal)
	lFloat := toFloat64(lVal)

	if gFloat == 0 && lFloat == 0 {
		return nil
	}

	base := math.Max(math.Abs(gFloat), math.Abs(lFloat))
	if base == 0 {
		base = 1
	}
	diff := math.Abs(gFloat-lFloat) / base

	if diff <= tolerance {
		return nil
	}

	return []string{fmt.Sprintf("%s: Glacier=%v, Local=%v (diff %.4f%% > %.2f%%)",
		path, gVal, lVal, diff*100, tolerance*100)}
}

func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case uint64:
		return float64(n)
	}
	return 0
}

func writeDiffFile(diff DiffData) (string, string, string, error) {
	diffDir := "/tmp/dev/diffs"
	if err := os.MkdirAll(diffDir, 0755); err != nil {
		return "", "", "", fmt.Errorf("failed to create diffs directory: %w", err)
	}

	safeName := strings.ReplaceAll(diff.TestCase, " ", "_")
	safeName = strings.ReplaceAll(safeName, "/", "_")
	safeName = strings.ReplaceAll(safeName, ":", "_")
	timestamp := time.Now().Format("20060102_150405")
	baseFilename := fmt.Sprintf("%s_%s", safeName, timestamp)

	diffPath := filepath.Join(diffDir, baseFilename+".diff")
	glacierPath := filepath.Join(diffDir, baseFilename+"_glacier.yaml")
	localPath := filepath.Join(diffDir, baseFilename+"_local.yaml")

	glacierYAML, err := yaml.Marshal(diff.Glacier)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to marshal Glacier response: %w", err)
	}
	localYAML, err := yaml.Marshal(diff.Local)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to marshal Local response: %w", err)
	}

	if err := os.WriteFile(glacierPath, glacierYAML, 0644); err != nil {
		return "", "", "", fmt.Errorf("failed to write Glacier YAML: %w", err)
	}
	if err := os.WriteFile(localPath, localYAML, 0644); err != nil {
		return "", "", "", fmt.Errorf("failed to write Local YAML: %w", err)
	}

	glacierLines := strings.Split(string(glacierYAML), "\n")
	localLines := strings.Split(string(localYAML), "\n")
	diffText := unifiedDiff(glacierLines, localLines, glacierPath, localPath)

	header := fmt.Sprintf("# Test: %s\n# Glacier URL: %s\n# Local URL: %s\n# Glacier YAML: %s\n# Local YAML: %s\n# Response time: %dms\n\n",
		diff.TestCase, diff.GlacierURL, diff.LocalURL, glacierPath, localPath, diff.ResponseTime)

	if err := os.WriteFile(diffPath, []byte(header+diffText), 0644); err != nil {
		return "", "", "", fmt.Errorf("failed to write diff file: %w", err)
	}

	return diffPath, glacierPath, localPath, nil
}

func unifiedDiff(a, b []string, nameA, nameB string) string {
	var out strings.Builder
	out.WriteString(fmt.Sprintf("--- %s\n+++ %s\n", nameA, nameB))

	m, n := len(a), len(b)

	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				lcs[i][j] = lcs[i-1][j-1] + 1
			} else {
				lcs[i][j] = max(lcs[i-1][j], lcs[i][j-1])
			}
		}
	}

	var edits []edit
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			edits = append(edits, edit{' ', a[i-1]})
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			edits = append(edits, edit{'+', b[j-1]})
			j--
		} else {
			edits = append(edits, edit{'-', a[i-1]})
			i--
		}
	}

	for i, j := 0, len(edits)-1; i < j; i, j = i+1, j-1 {
		edits[i], edits[j] = edits[j], edits[i]
	}

	const context = 3
	for idx := 0; idx < len(edits); {
		for idx < len(edits) && edits[idx].op == ' ' {
			idx++
		}
		if idx >= len(edits) {
			break
		}

		start := max(0, idx-context)
		end := idx
		for end < len(edits) && (edits[end].op != ' ' || (end+1 < len(edits) && hasChangeWithin(edits, end, context))) {
			end++
		}
		end = min(len(edits), end+context)

		out.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", start+1, end-start, start+1, end-start))
		for k := start; k < end; k++ {
			if edits[k].line != "" || edits[k].op != ' ' {
				out.WriteByte(edits[k].op)
				out.WriteString(edits[k].line)
				out.WriteByte('\n')
			}
		}

		idx = end
	}

	return out.String()
}

func hasChangeWithin(edits []edit, pos, distance int) bool {
	for i := pos + 1; i < len(edits) && i <= pos+distance; i++ {
		if edits[i].op != ' ' {
			return true
		}
	}
	return false
}
