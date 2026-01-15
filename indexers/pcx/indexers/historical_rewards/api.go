package historical_rewards

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Asset IDs for AVAX on different networks
const (
	mainnetAvaxAssetID = "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z"
	fujiAvaxAssetID    = "U8iRqJoiJm8xZHAacmvYyZVwqQx6uDNtQeP3CQ6fcgQk3JqnK"
)

func getAvaxAssetID(networkID uint32) string {
	if networkID == 5 {
		return fujiAvaxAssetID
	}
	return mainnetAvaxAssetID
}

// RegisterRoutes adds HTTP handlers for historical rewards.
func (h *HistoricalRewards) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/networks/{network}/rewards", h.handleListRewards)
}

func (h *HistoricalRewards) handleListRewards(w http.ResponseWriter, r *http.Request) {
	queryAddrs := r.URL.Query().Get("addresses")
	pageSize := 10
	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		if n, err := strconv.Atoi(ps); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}

	offset := 0
	if token := r.URL.Query().Get("pageToken"); token != "" {
		if decoded, err := base64.StdEncoding.DecodeString(token); err == nil {
			offset, _ = strconv.Atoi(string(decoded))
		}
	}

	// Query - exclude records already marked as having no rewards
	query := `
		SELECT tx_id, reward_addrs, node_id, stake_amount, start_time, end_time, 
		       reward_type, reward_tx_id, reward_amount, reward_utxo_id
		FROM staking_records
		WHERE completed = 1 AND reward_utxo_id != 'NONE'
	`
	var args []any

	if queryAddrs != "" {
		addrList := strings.Split(queryAddrs, ",")
		likes := []string{}
		for _, addr := range addrList {
			clean := normalizeAddr(addr)
			likes = append(likes, `reward_addrs LIKE ?`)
			args = append(args, `%"`+clean+`"%`)
		}
		query += " AND (" + strings.Join(likes, " OR ") + ")"
	}

	query += " ORDER BY end_time DESC LIMIT ? OFFSET ?"
	// Fetch extra rows to account for NONE-filtered results after lazy-load
	// Using 3x multiplier to be safe (worst case: 2/3 are NONE)
	fetchLimit := (pageSize + 1) * 3
	args = append(args, fetchLimit, offset)

	rows, err := h.db.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var historicalRewards []map[string]any
	validCount := 0 // Count of valid (non-NONE) results

	for rows.Next() {
		// Stop fetching once we have pageSize+1 to detect next page
		if validCount >= pageSize+1 {
			break
		}

		var txID, rewardAddrsJSON, nodeID, rewardType, rewardTxID, rewardUTXOID string
		var stakeAmount, startTime, endTime, rewardAmount int64

		err := rows.Scan(&txID, &rewardAddrsJSON, &nodeID, &stakeAmount, &startTime, &endTime,
			&rewardType, &rewardTxID, &rewardAmount, &rewardUTXOID)
		if err != nil {
			continue
		}

		// LAZY LOAD: If completed but no reward info, fetch it now
		if rewardUTXOID == "" {
			utxosBytes, err := h.rpc.GetRewardUTXOs(r.Context(), txID)
			if err == nil {
				// Parse the staking record's reward addresses into a lookup set
				var recordAddrs []string
				json.Unmarshal([]byte(rewardAddrsJSON), &recordAddrs)
				addrSet := make(map[string]bool)
				for _, a := range recordAddrs {
					addrSet[a] = true
				}

				var matchedAmount uint64
				var matchedUTXOID string
				var matchedOutputIndex uint32
				hrp := getHRP(h.networkID)

				// Find the UTXO that matches the staking record's owner addresses
				for _, b := range utxosBytes {
					var u avax.UTXO
					if _, err := txs.Codec.Unmarshal(b, &u); err != nil {
						continue
					}

					// Extract owner addresses from the UTXO output
					utxoAddrs := extractUTXOAddrs(u.Out, hrp)

					// Check if any UTXO address matches the staking record's addresses
					matched := false
					for _, addr := range utxoAddrs {
						if addrSet[addr] {
							matched = true
							break
						}
					}

					if matched {
						if amt, ok := u.Out.(avax.Amounter); ok {
							matchedAmount = amt.Amount()
							matchedUTXOID = u.InputID().String()
							matchedOutputIndex = u.OutputIndex
							break // Use the first matching UTXO
						}
					}
				}

				fetchedUTXOID := matchedUTXOID
				if fetchedUTXOID == "" {
					fetchedUTXOID = "NONE"
				}

				utxoIDWithIndex := fetchedUTXOID
				if fetchedUTXOID != "NONE" {
					utxoIDWithIndex = fetchedUTXOID + ":" + strconv.FormatUint(uint64(matchedOutputIndex), 10)
				}
				h.db.Exec(`UPDATE staking_records SET reward_amount=?, reward_utxo_id=? WHERE tx_id=?`,
					matchedAmount, utxoIDWithIndex, txID)

				rewardAmount = int64(matchedAmount)
				rewardUTXOID = utxoIDWithIndex
			}
		}

		// Skip records with no rewards
		if rewardUTXOID == "NONE" || rewardUTXOID == "" {
			continue
		}

		// Parse utxoID:outputIndex from stored value
		utxoID := rewardUTXOID
		var outputIndex uint32
		if parts := strings.SplitN(rewardUTXOID, ":", 2); len(parts) == 2 {
			utxoID = parts[0]
			if idx, err := strconv.ParseUint(parts[1], 10, 32); err == nil {
				outputIndex = uint32(idx)
			}
		}

		var addrs []string
		json.Unmarshal([]byte(rewardAddrsJSON), &addrs)

		historicalRewards = append(historicalRewards, map[string]any{
			"addresses":      addrs,
			"txHash":         txID,
			"utxoId":         utxoID,
			"outputIndex":    outputIndex,
			"amountStaked":   strconv.FormatInt(stakeAmount, 10),
			"nodeId":         nodeID,
			"startTimestamp": startTime,
			"endTimestamp":   endTime,
			"reward": map[string]any{
				"assetId":      getAvaxAssetID(h.networkID),
				"name":         "Avalanche",
				"symbol":       "AVAX",
				"denomination": 9,
				"type":         "secp256k1",
				"amount":       strconv.FormatInt(rewardAmount, 10),
			},
			"rewardType":   rewardType,
			"rewardTxHash": rewardTxID,
		})
		validCount++
	}

	if historicalRewards == nil {
		historicalRewards = []map[string]any{}
	}

	// If we got more than pageSize, trim and indicate next page
	hasNextPage := len(historicalRewards) > pageSize
	if hasNextPage {
		historicalRewards = historicalRewards[:pageSize]
	}

	resp := map[string]any{
		"historicalRewards": historicalRewards,
	}

	if hasNextPage {
		nextToken := base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(offset + pageSize)))
		resp["nextPageToken"] = nextToken
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func normalizeAddr(addr string) string {
	if strings.HasPrefix(addr, "P-") {
		return addr[2:]
	}
	return addr
}

// extractUTXOAddrs extracts owner addresses from a UTXO output.
// Handles both direct secp256k1fx.TransferOutput and stakeable.LockOut wrapped outputs.
func extractUTXOAddrs(out any, hrp string) []string {
	var addrs []string

	switch o := out.(type) {
	case *secp256k1fx.TransferOutput:
		for _, a := range o.Addrs {
			if s, err := address.FormatBech32(hrp, a.Bytes()); err == nil {
				addrs = append(addrs, s)
			}
		}
	case *stakeable.LockOut:
		// Unwrap the inner output
		if inner, ok := o.TransferableOut.(*secp256k1fx.TransferOutput); ok {
			for _, a := range inner.Addrs {
				if s, err := address.FormatBech32(hrp, a.Bytes()); err == nil {
					addrs = append(addrs, s)
				}
			}
		}
	}

	return addrs
}
