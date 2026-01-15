package subnets

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func (s *Subnets) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/networks/{network}/subnets", s.handleListSubnets)
	mux.HandleFunc("GET /v1/networks/{network}/subnets/{subnetId}", s.handleGetSubnet)
}

func (s *Subnets) handleListSubnets(w http.ResponseWriter, r *http.Request) {
	pageSize := 10
	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 {
			pageSize = v
		}
	}

	subnets, err := s.listSubnets(pageSize)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := struct {
		Subnets []*SubnetMetadata `json:"subnets"`
	}{
		Subnets: subnets,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Subnets) handleGetSubnet(w http.ResponseWriter, r *http.Request) {
	subnetID := r.PathValue("subnetId")
	if subnetID == "" {
		http.Error(w, "missing subnetId", http.StatusBadRequest)
		return
	}

	meta, err := s.getSubnet(subnetID, true)
	if err != nil {
		http.Error(w, "subnet not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(meta)
}
