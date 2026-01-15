package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"time"

	ts "github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/xchain/pre_cortina_timestamps"
	"github.com/joho/godotenv"
)

type VertexResponse struct {
	Vertices      []Vertex `json:"vertices"`
	NextPageToken string   `json:"nextPageToken"`
}

type Vertex struct {
	VertexHash      string   `json:"vertexHash"`
	VertexTimestamp int64    `json:"vertexTimestamp"`
	Transactions    []string `json:"transactions"` // Transaction IDs within this vertex
}

type VertexData struct {
	Hash      string `json:"hash"`
	Timestamp int64  `json:"timestamp"`
}

type TxData struct {
	Hash      string `json:"hash"`
	Timestamp int64  `json:"timestamp"`
}

type Progress struct {
	Network         string       `json:"network"`
	Vertices        []VertexData `json:"vertices"`
	Transactions    []TxData     `json:"transactions"`
	ScrapedAt       int64        `json:"scrapedAt"`
	InProgress      bool         `json:"inProgress"`
	VertexPageToken string       `json:"vertexPageToken,omitempty"`
	Stage           string       `json:"stage"` // "vertices" or "done"
	Sorted          bool         `json:"sorted,omitempty"`
}

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "testnet" && os.Args[1] != "mainnet") {
		fmt.Println("Usage: scrape_timestamps <testnet|mainnet>")
		os.Exit(1)
	}

	network := os.Args[1]
	if network == "testnet" {
		network = "fuji"
	}

	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	apiKey := os.Getenv("DATA_API_KEY")
	if apiKey == "" {
		log.Fatal("DATA_API_KEY environment variable is required")
	}

	log.Printf("=== Starting %s scrape ===", network)
	if err := scrapeNetwork(network, apiKey); err != nil {
		log.Fatalf("Failed to scrape %s: %v", network, err)
	}
	log.Printf("=== %s scraped successfully ===", network)
}

func scrapeNetwork(network, apiKey string) error {
	// Output files to xchain/pre_cortina_timestamps for go:embed
	outputDir := "xchain/pre_cortina_timestamps"
	outputFile := fmt.Sprintf("%s/timestamps-%s.json", outputDir, network)
	binaryFile := fmt.Sprintf("%s/%s.bin", outputDir, network)

	var p Progress
	if _, err := os.Stat(outputFile); err == nil {
		data, err := os.ReadFile(outputFile)
		if err == nil {
			if err := json.Unmarshal(data, &p); err == nil {
				if p.InProgress {
					log.Printf("%s: Resuming from stage %s (vertices: %d, txs: %d)", network, p.Stage, len(p.Vertices), len(p.Transactions))
				} else if p.ScrapedAt > 0 && !p.InProgress && p.Sorted {
					log.Printf("%s: Already finished and sorted at %v", network, time.Unix(p.ScrapedAt, 0))
					log.Printf("%s: Rebuilding binary archive from JSON...", network)
					return buildBinaryArchive(network, p.Vertices, p.Transactions, binaryFile)
				}
			}
		}
	}

	if p.Stage == "" {
		p.Stage = "vertices"
		p.Network = network
	}

	// Scrape vertices (and extract transactions from them)
	if p.Stage == "vertices" {
		vertices, txs, token, err := scrapeVertices(network, apiKey, outputFile, p.Vertices, p.Transactions, p.VertexPageToken)
		if err != nil {
			return fmt.Errorf("scraping vertices: %w", err)
		}
		p.Vertices = vertices
		p.Transactions = txs
		p.VertexPageToken = token
		p.Stage = "done"
		p.InProgress = true
		saveJSON(outputFile, p)
	}
	log.Printf("%s: Scraped %d vertices and %d transactions", network, len(p.Vertices), len(p.Transactions))

	// Sort by timestamp for optimal delta compression
	log.Printf("%s: Sorting data...", network)
	sort.Slice(p.Vertices, func(i, j int) bool {
		return p.Vertices[i].Timestamp < p.Vertices[j].Timestamp
	})
	sort.Slice(p.Transactions, func(i, j int) bool {
		return p.Transactions[i].Timestamp < p.Transactions[j].Timestamp
	})

	// Final save with sorted data
	p.ScrapedAt = time.Now().Unix()
	p.InProgress = false
	p.Sorted = true
	if err := saveJSON(outputFile, p); err != nil {
		return fmt.Errorf("saving JSON: %w", err)
	}
	log.Printf("%s: Final JSON saved to %s", network, outputFile)

	// Build binary archive immediately
	log.Printf("%s: Building binary archive...", network)
	if err := buildBinaryArchive(network, p.Vertices, p.Transactions, binaryFile); err != nil {
		return fmt.Errorf("building binary archive: %w", err)
	}
	log.Printf("%s: Binary archive built: %s", network, binaryFile)

	return nil
}

func buildBinaryArchive(network string, vertices []VertexData, txs []TxData, binaryFile string) error {
	log.Printf("  Merging %d vertices and %d txs...", len(vertices), len(txs))

	// Merge into single map
	data := make(map[string]int64, len(vertices)+len(txs))
	for _, v := range vertices {
		data[v.Hash] = v.Timestamp
	}
	for _, tx := range txs {
		data[tx.Hash] = tx.Timestamp
	}

	// Build archive
	log.Printf("  Building archive from %d entries...", len(data))
	archive, err := ts.Build(data)
	if err != nil {
		return fmt.Errorf("building archive: %w", err)
	}
	log.Printf("  Using %d-byte prefixes", archive.PrefixLen())

	// Write to file
	log.Printf("  Writing to %s...", binaryFile)
	f, err := os.Create(binaryFile)
	if err != nil {
		return err
	}
	defer f.Close()

	n, err := archive.WriteTo(f)
	if err != nil {
		return fmt.Errorf("writing archive: %w", err)
	}

	log.Printf("  Archive size: %.2f MB (%d entries, %d bytes written)", float64(n)/1024/1024, archive.Len(), n)

	return nil
}

func scrapeVertices(network, apiKey, outputFile string, initialVertices []VertexData, initialTxs []TxData, initialToken string) ([]VertexData, []TxData, string, error) {
	baseURL := fmt.Sprintf("https://data-api.avax.network/v1/networks/%s/blockchains/x-chain/vertices", network)
	vertices := initialVertices
	txs := initialTxs
	pageToken := initialToken
	pageNum := len(vertices) / 100

	for {
		pageNum++
		url := fmt.Sprintf("%s?pageSize=100", baseURL)
		if pageToken != "" {
			url = fmt.Sprintf("%s&pageToken=%s", url, pageToken)
		}

		log.Printf("  Fetching vertices page %d...", pageNum)

		var resp VertexResponse
		if err := fetchWithRetry(url, apiKey, &resp); err != nil {
			return vertices, txs, pageToken, err
		}

		for _, v := range resp.Vertices {
			vertices = append(vertices, VertexData{
				Hash:      v.VertexHash,
				Timestamp: v.VertexTimestamp,
			})
			// Extract transactions from this vertex
			for _, txHash := range v.Transactions {
				txs = append(txs, TxData{
					Hash:      txHash,
					Timestamp: v.VertexTimestamp, // Same timestamp as vertex
				})
			}
		}

		log.Printf("    Got %d vertices (total: %d vertices, %d txs)", len(resp.Vertices), len(vertices), len(txs))

		if len(vertices)%10000 == 0 {
			if err := saveJSON(outputFile, Progress{
				Network:         network,
				Vertices:        vertices,
				Transactions:    txs,
				ScrapedAt:       time.Now().Unix(),
				InProgress:      true,
				VertexPageToken: resp.NextPageToken,
				Stage:           "vertices",
			}); err != nil {
				log.Printf("    Warning: failed to save progress: %v", err)
			}
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return vertices, txs, pageToken, nil
}

func fetchWithRetry(url, apiKey string, target interface{}) error {
	maxRetries := 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("    Retry attempt %d/%d", attempt, maxRetries-1)
		}

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("x-glacier-api-key", apiKey)

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode == 409 || resp.StatusCode == 429 {
			log.Printf("    Rate limited (%d), sleeping 10 seconds...", resp.StatusCode)
			time.Sleep(10 * time.Second)
			continue
		}

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}

		if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
			return fmt.Errorf("decoding JSON: %w", err)
		}

		return nil
	}

	return fmt.Errorf("max retries exceeded")
}

func saveJSON(filename string, data interface{}) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}
