package validators

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func (v *Validators) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/networks/{network}/validators", v.handleListValidators)
	mux.HandleFunc("GET /v1/networks/{network}/validators/{nodeId}", v.handleGetValidator)
}

func (v *Validators) handleListValidators(w http.ResponseWriter, r *http.Request) {
	opts := ListOptions{
		PageSize: 10,
	}

	// Parse pageSize
	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		if val, err := strconv.Atoi(ps); err == nil && val > 0 {
			opts.PageSize = val
		}
	}

	// Parse filters
	opts.SubnetID = r.URL.Query().Get("subnetId")
	opts.Status = r.URL.Query().Get("validationStatus")

	// nodeIds filter (comma-separated, supports substring matching)
	// For now, we'll just take the first nodeId if multiple are provided
	if nodeIds := r.URL.Query().Get("nodeIds"); nodeIds != "" {
		// Simple: just use first nodeId for now
		for i := 0; i < len(nodeIds); i++ {
			if nodeIds[i] == ',' {
				opts.NodeID = nodeIds[:i]
				break
			}
		}
		if opts.NodeID == "" {
			opts.NodeID = nodeIds
		}
	}

	records, err := v.listValidators(opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to API response format
	validators := make([]map[string]any, 0, len(records))
	for _, rec := range records {
		// Enrich with delegator counts
		count, totalDelegated := v.countDelegators(rec.TxHash)
		rec.DelegatorCount = count
		if totalDelegated > 0 {
			rec.AmountDelegated = strconv.FormatUint(totalDelegated, 10)
		}
		validators = append(validators, rec.ToAPIResponse())
	}

	response := map[string]any{
		"validators": validators,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (v *Validators) handleGetValidator(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeId")
	if nodeID == "" {
		http.Error(w, "missing nodeId", http.StatusBadRequest)
		return
	}

	opts := ListOptions{
		PageSize: 100, // Return more validations for a single node
		NodeID:   nodeID,
	}

	// Parse pageSize
	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		if val, err := strconv.Atoi(ps); err == nil && val > 0 {
			opts.PageSize = val
		}
	}

	// Parse status filter
	opts.Status = r.URL.Query().Get("validationStatus")

	records, err := v.listValidatorsByNodeID(nodeID, opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to API response format
	validators := make([]map[string]any, 0, len(records))
	for _, rec := range records {
		// Enrich with delegator counts
		count, totalDelegated := v.countDelegators(rec.TxHash)
		rec.DelegatorCount = count
		if totalDelegated > 0 {
			rec.AmountDelegated = strconv.FormatUint(totalDelegated, 10)
		}
		validators = append(validators, rec.ToAPIResponse())
	}

	response := map[string]any{
		"validators": validators,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
