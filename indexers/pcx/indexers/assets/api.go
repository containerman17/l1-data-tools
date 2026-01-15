package assets

import (
	"encoding/json"
	"net/http"
)

// RegisterRoutes adds HTTP handlers for assets.
func (a *Assets) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/networks/{network}/blockchains/{blockchainId}/assets/{assetID}", a.handleAssetByID)
}

func (a *Assets) handleAssetByID(w http.ResponseWriter, r *http.Request) {
	assetID := r.PathValue("assetID")
	if assetID == "" {
		http.Error(w, "assetID required", http.StatusBadRequest)
		return
	}

	meta, err := a.getAsset(assetID)
	if err != nil {
		http.Error(w, "asset not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(meta)
}
