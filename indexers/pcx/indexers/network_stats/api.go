package network_stats

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (m *NetworkMonitor) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/networks/{network}", m.handleGetNetwork)
}

func (m *NetworkMonitor) handleGetNetwork(w http.ResponseWriter, r *http.Request) {
	network := r.PathValue("network")
	if network != "mainnet" && network != "fuji" && network != "testnet" {
		http.Error(w, "invalid network", http.StatusBadRequest)
		return
	}

	stats := m.GetStats()
	if stats == nil {
		// If monitor hasn't finished first run yet, try to return empty but valid JSON
		// or wait a moment. For now, empty skeleton or 404/503.
		// Since we persist to DB, this should only happen on literally the first run of the app.
		http.Error(w, "network stats not yet available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// Map Glacier-style generic network names to our specific IDs if needed
func matchesNetwork(network string, networkID uint32) bool {
	network = strings.ToLower(network)
	switch networkID {
	case 1:
		return network == "mainnet"
	case 5:
		return network == "fuji" || network == "testnet"
	default:
		return false
	}
}
