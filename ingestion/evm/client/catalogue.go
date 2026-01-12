package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ChainInfo from catalogue endpoint
type ChainInfo struct {
	Name       string `json:"name"`
	EvmChainId int    `json:"evmChainId"`
	SubnetId   string `json:"subnetId"`
	Indexer    string `json:"indexer"`
}

// NewFromCatalogue fetches /chains and returns a client for each chain.
// catalogueURL should be the base URL (e.g., "http://node:80").
// Returns map[blockchainId]*Client.
func NewFromCatalogue(catalogueURL string, opts ...Option) (map[string]*Client, map[string]ChainInfo, error) {
	url := strings.TrimSuffix(catalogueURL, "/") + "/chains"
	resp, err := http.Get(url)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch catalogue: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("catalogue returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read catalogue: %w", err)
	}

	var catalogue map[string]ChainInfo
	if err := json.Unmarshal(body, &catalogue); err != nil {
		return nil, nil, fmt.Errorf("parse catalogue: %w", err)
	}

	clients := make(map[string]*Client)
	baseURL := strings.TrimSuffix(catalogueURL, "/")

	for blockchainId, info := range catalogue {
		// Build WebSocket URL from indexer path
		wsURL := strings.Replace(baseURL, "http://", "", 1)
		wsURL = strings.Replace(wsURL, "https://", "", 1)
		wsURL = wsURL + info.Indexer
		wsURL = strings.TrimSuffix(wsURL, "/ws") // NewClient adds /ws in connect()

		clients[blockchainId] = NewClient(wsURL, opts...)
	}

	return clients, catalogue, nil
}
