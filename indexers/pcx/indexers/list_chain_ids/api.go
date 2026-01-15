package list_chain_ids

import (
	"encoding/json"
	"net/http"
	"strings"
)

type ChainIDsResponse struct {
	Addresses []AddressChains `json:"addresses"`
}

type AddressChains struct {
	Address       string   `json:"address"`
	BlockchainIDs []string `json:"blockchainIds"`
}

func (c *Chains) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/networks/{network}/addresses:listChainIds", c.handleListChainIDs)
}

func (c *Chains) handleListChainIDs(w http.ResponseWriter, r *http.Request) {
	addressesParam := r.URL.Query().Get("addresses")
	if addressesParam == "" {
		http.Error(w, "missing addresses parameter", http.StatusBadRequest)
		return
	}

	addresses := strings.Split(addressesParam, ",")
	resp := ChainIDsResponse{
		Addresses: make([]AddressChains, 0, len(addresses)),
	}

	for _, addr := range addresses {
		blockchainIDs, err := c.getChainIDs(addr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if len(blockchainIDs) > 0 {
			resp.Addresses = append(resp.Addresses, AddressChains{
				Address:       addr,
				BlockchainIDs: blockchainIDs,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
