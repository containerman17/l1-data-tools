package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type GlacierUTXO struct {
	Addresses         []string     `json:"addresses"`
	UTXOId            string       `json:"utxoId"`
	CreationTxHash    string       `json:"creationTxHash"`
	OutputIndex       string       `json:"outputIndex"`
	Timestamp         int64        `json:"timestamp,omitempty"`
	ConsumingTxHash   string       `json:"consumingTxHash,omitempty"`
	AssetId           string       `json:"assetId"`
	Asset             GlacierAsset `json:"asset"`
	UTXOType          string       `json:"utxoType"`
	Locktime          int64        `json:"locktime"`
	Threshold         int          `json:"threshold"`
	CreatedOnChainId  string       `json:"createdOnChainId"`
	ConsumedOnChainId string       `json:"consumedOnChainId"`
	UTXOBytes         string       `json:"utxoBytes"`
}

type GlacierAsset struct {
	AssetId      string `json:"assetId"`
	Name         string `json:"name"`
	Symbol       string `json:"symbol"`
	Denomination int    `json:"denomination"`
	Type         string `json:"type"`
	Amount       string `json:"amount"`
}

type GlacierUTXOResponse struct {
	UTXOs         []GlacierUTXO `json:"utxos"`
	NextPageToken string        `json:"nextPageToken,omitempty"`
}

func getGlacierUTXOs(addresses []string, includeSpent bool) ([]GlacierUTXO, error) {
	baseURL := "https://data-api.avax.network/v1/networks/fuji/blockchains/x-chain/utxos"

	params := url.Values{}
	params.Set("addresses", strings.Join(addresses, ","))
	params.Set("pageSize", "100")
	params.Set("sortBy", "timestamp")
	params.Set("sortOrder", "desc")
	if includeSpent {
		params.Set("includeSpent", "true")
	}

	var allUTXOs []GlacierUTXO
	nextPageToken := ""

	for {
		reqURL := baseURL + "?" + params.Encode()
		if nextPageToken != "" {
			reqURL += "&pageToken=" + nextPageToken
		}

		// Rate limit protection - API has strict limits
		time.Sleep(500 * time.Millisecond)

		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		if apiKey := os.Getenv("DATA_API_KEY"); apiKey != "" {
			req.Header.Set("x-glacier-api-key", apiKey)
		} else {
			log.Fatal("DATA_API_KEY is not set, you should set it into .env ")
		}

		var resp *http.Response
		for retries := 0; retries < 5; retries++ {
			resp, err = http.DefaultClient.Do(req)
			if err != nil {
				return nil, fmt.Errorf("request failed: %w", err)
			}
			if resp.StatusCode == 429 {
				resp.Body.Close()
				backoff := time.Duration(3<<retries) * time.Second // exponential backoff: 3, 6, 12, 24, 48 seconds
				fmt.Printf("Rate limited, waiting %v (retry %d)...\n", backoff, retries+1)
				time.Sleep(backoff)
				req, _ = http.NewRequest("GET", reqURL, nil)
				req.Header.Set("x-glacier-api-key", os.Getenv("DATA_API_KEY"))
				continue
			}
			break
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("bad status: %d, body: %s, url: %s", resp.StatusCode, string(body), reqURL)
		}

		var result GlacierUTXOResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode failed: %w", err)
		}

		allUTXOs = append(allUTXOs, result.UTXOs...)

		if result.NextPageToken == "" {
			break
		}
		nextPageToken = result.NextPageToken
	}

	return allUTXOs, nil
}
