package blockchains

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type ListBlockchainsResponse struct {
	Blockchains   []*BlockchainMetadata `json:"blockchains"`
	NextPageToken string                `json:"nextPageToken,omitempty"`
}

func (b *Blockchains) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/networks/{network}/blockchains", b.handleListBlockchains)
	mux.HandleFunc("/v1/networks/{network}/blockchains/{blockchainId}", b.handleGetBlockchain)
}

func (b *Blockchains) handleListBlockchains(w http.ResponseWriter, r *http.Request) {
	pageSize := 10
	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil {
			pageSize = v
		}
	}
	pageToken := r.URL.Query().Get("pageToken")

	blockchains, nextToken, err := b.listBlockchains(pageSize, pageToken)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := ListBlockchainsResponse{
		Blockchains:   blockchains,
		NextPageToken: nextToken,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (b *Blockchains) handleGetBlockchain(w http.ResponseWriter, r *http.Request) {
	blockchainID := r.PathValue("blockchainId")
	if blockchainID == "" {
		http.Error(w, "blockchainId is required", http.StatusBadRequest)
		return
	}

	blockchain, err := b.getBlockchain(blockchainID)
	if err != nil {
		http.Error(w, "blockchain not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(blockchain)
}
