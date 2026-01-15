package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type GlacierUTXO struct {
	Addresses               []string     `json:"addresses"`
	UTXOId                  string       `json:"utxoId"`
	TxHash                  string       `json:"txHash"`
	OutputIndex             int          `json:"outputIndex"`
	BlockTimestamp          int64        `json:"blockTimestamp,omitempty"`
	BlockNumber             string       `json:"blockNumber,omitempty"`
	ConsumingTxHash         string       `json:"consumingTxHash,omitempty"`
	ConsumingBlockTimestamp int64        `json:"consumingBlockTimestamp,omitempty"`
	ConsumingBlockNumber    string       `json:"consumingBlockNumber,omitempty"`
	AssetId                 string       `json:"assetId"`
	Asset                   GlacierAsset `json:"asset"`
	UTXOType                string       `json:"utxoType"`
	Amount                  string       `json:"amount"`
	PlatformLocktime        int64        `json:"platformLocktime"`
	Threshold               int          `json:"threshold"`
	CreatedOnChainId        string       `json:"createdOnChainId"`
	ConsumedOnChainId       string       `json:"consumedOnChainId"`
	Staked                  bool         `json:"staked"`
	UTXOBytes               string       `json:"utxoBytes"`
	// Staked UTXO fields
	UTXOStartTimestamp int64 `json:"utxoStartTimestamp,omitempty"`
	UTXOEndTimestamp   int64 `json:"utxoEndTimestamp,omitempty"`
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
	baseURL := "https://data-api.avax.network/v1/networks/fuji/blockchains/p-chain/utxos"

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

		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		if apiKey := os.Getenv("DATA_API_KEY"); apiKey != "" {
			req.Header.Set("x-glacier-api-key", apiKey)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("bad status: %d", resp.StatusCode)
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

// getStakedUTXOs fetches only staked UTXOs for an address
func getStakedUTXOs(addresses []string) ([]GlacierUTXO, error) {
	utxos, err := getGlacierUTXOs(addresses, false)
	if err != nil {
		return nil, err
	}

	var staked []GlacierUTXO
	for _, u := range utxos {
		if u.Staked {
			staked = append(staked, u)
		}
	}
	return staked, nil
}
