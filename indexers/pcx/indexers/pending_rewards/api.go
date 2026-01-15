package pending_rewards

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// Asset IDs for AVAX on different networks
const (
	mainnetAvaxAssetID = "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z"
	fujiAvaxAssetID    = "U8iRqJoiJm8xZHAacmvYyZVwqQx6uDNtQeP3CQ6fcgQk3JqnK"
)

// PercentDenominator matches avalanchego's reward.PercentDenominator
const PercentDenominator = 1_000_000

func getAvaxAssetID(networkID uint32) string {
	if networkID == 5 {
		return fujiAvaxAssetID
	}
	return mainnetAvaxAssetID
}

// Response types
type pendingRewardEntry struct {
	Addresses       []string        `json:"addresses"`
	TxHash          string          `json:"txHash"`
	AmountStaked    string          `json:"amountStaked"`
	NodeID          string          `json:"nodeId"`
	StartTimestamp  int64           `json:"startTimestamp"`
	EndTimestamp    int64           `json:"endTimestamp"`
	Progress        float64         `json:"progress"`
	EstimatedReward estimatedReward `json:"estimatedReward"`
	RewardType      string          `json:"rewardType"`
}

type estimatedReward struct {
	AssetID      string `json:"assetId"`
	Name         string `json:"name"`
	Symbol       string `json:"symbol"`
	Denomination int    `json:"denomination"`
	Type         string `json:"type"`
	Amount       string `json:"amount"`
}

type pendingRewardsResponse struct {
	PendingRewards []pendingRewardEntry `json:"pendingRewards"`
	NextPageToken  string               `json:"nextPageToken,omitempty"`
}

type cachedPendingRewards struct {
	PendingRewards []pendingRewardEntry `json:"pendingRewards"`
	CachedAt       int64                `json:"_cachedAt"`
}

// RegisterRoutes adds HTTP handlers for pending rewards.
func (p *PendingRewards) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/networks/{network}/rewards:listPending", p.handleListPending)
}

func (p *PendingRewards) handleListPending(w http.ResponseWriter, r *http.Request) {
	addresses := r.URL.Query().Get("addresses")
	nodeIds := r.URL.Query().Get("nodeIds")

	if addresses == "" && nodeIds == "" {
		http.Error(w, "addresses or nodeIds required", http.StatusBadRequest)
		return
	}

	// Check cache
	cacheKey := p.getCacheKey(addresses, nodeIds)
	if cached := p.getFromCache(cacheKey); cached != nil {
		p.updateProgress(cached.PendingRewards)
		p.serveWithPagination(w, r, cached.PendingRewards, true)
		return
	}

	// Cache miss - fetch from RPC
	var validators []validatorInfo
	var queryAddrSet map[string]bool
	queryByNodeId := nodeIds != ""

	if queryByNodeId {
		validatorsJSON, err := p.rpc.GetCurrentValidators(r.Context(), []string{nodeIds})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var resp struct {
			Validators []validatorInfo `json:"validators"`
		}
		if err := json.Unmarshal(validatorsJSON, &resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		validators = resp.Validators
	} else {
		allValidatorsJSON, err := p.rpc.GetCurrentValidators(r.Context(), nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var allValidators struct {
			Validators []validatorInfo `json:"validators"`
		}
		if err := json.Unmarshal(allValidatorsJSON, &allValidators); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		queryAddr := "P-" + addresses
		queryAddrSet = map[string]bool{
			addresses: true,
			queryAddr: true,
		}

		var matchingNodeIDs []string
		for _, v := range allValidators.Validators {
			if v.ValidationRewardOwner != nil && matchAddrs(v.ValidationRewardOwner.Addresses, queryAddrSet) {
				matchingNodeIDs = append(matchingNodeIDs, v.NodeID)
				continue
			}
			if v.DelegationRewardOwner != nil && matchAddrs(v.DelegationRewardOwner.Addresses, queryAddrSet) {
				matchingNodeIDs = append(matchingNodeIDs, v.NodeID)
			}
		}

		for _, nodeID := range matchingNodeIDs {
			validatorsJSON, err := p.rpc.GetCurrentValidators(r.Context(), []string{nodeID})
			if err != nil {
				continue
			}
			var resp struct {
				Validators []validatorInfo `json:"validators"`
			}
			if err := json.Unmarshal(validatorsJSON, &resp); err != nil {
				continue
			}
			validators = append(validators, resp.Validators...)
		}
	}

	// Build results
	now := time.Now().Unix()
	var rewards []pendingRewardEntry

	for _, val := range validators {
		startTime, _ := strconv.ParseInt(val.StartTime, 10, 64)
		endTime, _ := strconv.ParseInt(val.EndTime, 10, 64)
		potentialReward, _ := strconv.ParseUint(val.PotentialReward, 10, 64)

		// VALIDATOR entry
		if queryByNodeId || (val.ValidationRewardOwner != nil && matchAddrs(val.ValidationRewardOwner.Addresses, queryAddrSet)) {
			var addrs []string
			if val.ValidationRewardOwner != nil {
				addrs = formatAddresses(val.ValidationRewardOwner.Addresses)
			}
			rewards = append(rewards, pendingRewardEntry{
				Addresses:      addrs,
				TxHash:         val.TxID,
				AmountStaked:   val.Weight,
				NodeID:         val.NodeID,
				StartTimestamp: startTime,
				EndTimestamp:   endTime,
				Progress:       calcProgress(startTime, endTime, now),
				EstimatedReward: estimatedReward{
					AssetID:      getAvaxAssetID(p.networkID),
					Name:         "Avalanche",
					Symbol:       "AVAX",
					Denomination: 9,
					Type:         "secp256k1",
					Amount:       strconv.FormatUint(potentialReward, 10),
				},
				RewardType: "VALIDATOR",
			})
		}

		// Process delegators
		delegationFeePercent, _ := strconv.ParseFloat(val.DelegationFee, 64)
		delegationShares := uint32(delegationFeePercent * 10000)

		for _, del := range val.Delegators {
			delStartTime, _ := strconv.ParseInt(del.StartTime, 10, 64)
			delEndTime, _ := strconv.ParseInt(del.EndTime, 10, 64)
			delPotentialReward, _ := strconv.ParseUint(del.PotentialReward, 10, 64)

			validatorFee, delegatorReward := splitReward(delPotentialReward, delegationShares)

			// DELEGATOR entry
			if queryByNodeId || (del.RewardOwner != nil && matchAddrs(del.RewardOwner.Addresses, queryAddrSet)) {
				var addrs []string
				if del.RewardOwner != nil {
					addrs = formatAddresses(del.RewardOwner.Addresses)
				}
				rewards = append(rewards, pendingRewardEntry{
					Addresses:      addrs,
					TxHash:         del.TxID,
					AmountStaked:   del.Weight,
					NodeID:         val.NodeID,
					StartTimestamp: delStartTime,
					EndTimestamp:   delEndTime,
					Progress:       calcProgress(delStartTime, delEndTime, now),
					EstimatedReward: estimatedReward{
						AssetID:      getAvaxAssetID(p.networkID),
						Name:         "Avalanche",
						Symbol:       "AVAX",
						Denomination: 9,
						Type:         "secp256k1",
						Amount:       strconv.FormatUint(delegatorReward, 10),
					},
					RewardType: "DELEGATOR",
				})
			}

			// VALIDATOR_FEE entry
			if queryByNodeId || (val.DelegationRewardOwner != nil && matchAddrs(val.DelegationRewardOwner.Addresses, queryAddrSet)) {
				if validatorFee > 0 {
					var addrs []string
					if val.DelegationRewardOwner != nil {
						addrs = formatAddresses(val.DelegationRewardOwner.Addresses)
					}
					rewards = append(rewards, pendingRewardEntry{
						Addresses:      addrs,
						TxHash:         del.TxID,
						AmountStaked:   del.Weight,
						NodeID:         val.NodeID,
						StartTimestamp: delStartTime,
						EndTimestamp:   delEndTime,
						Progress:       calcProgress(delStartTime, delEndTime, now),
						EstimatedReward: estimatedReward{
							AssetID:      getAvaxAssetID(p.networkID),
							Name:         "Avalanche",
							Symbol:       "AVAX",
							Denomination: 9,
							Type:         "secp256k1",
							Amount:       strconv.FormatUint(validatorFee, 10),
						},
						RewardType: "VALIDATOR_FEE",
					})
				}
			}
		}
	}

	// Sort by startTimestamp
	sortOrder := r.URL.Query().Get("sortOrder")
	descending := sortOrder != "asc"

	sort.Slice(rewards, func(i, j int) bool {
		if rewards[i].StartTimestamp != rewards[j].StartTimestamp {
			if descending {
				return rewards[i].StartTimestamp > rewards[j].StartTimestamp
			}
			return rewards[i].StartTimestamp < rewards[j].StartTimestamp
		}
		return rewards[i].RewardType > rewards[j].RewardType
	})

	// Cache the full response
	cached := &cachedPendingRewards{
		PendingRewards: rewards,
		CachedAt:       time.Now().Unix(),
	}
	p.setCache(cacheKey, cached)

	p.serveWithPagination(w, r, rewards, false)
}

func (p *PendingRewards) serveWithPagination(w http.ResponseWriter, r *http.Request, rewards []pendingRewardEntry, isHit bool) {
	pageSize := 10
	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 {
			pageSize = parsed
			if pageSize > 100 {
				pageSize = 100
			}
		}
	}

	offset := 0
	if token := r.URL.Query().Get("pageToken"); token != "" {
		if decoded, err := base64.StdEncoding.DecodeString(token); err == nil {
			offset, _ = strconv.Atoi(string(decoded))
		}
	}

	total := len(rewards)
	end := offset + pageSize
	if end > total {
		end = total
	}
	if offset > total {
		offset = total
	}

	pagedRewards := rewards[offset:end]
	if pagedRewards == nil {
		pagedRewards = []pendingRewardEntry{}
	}

	response := pendingRewardsResponse{
		PendingRewards: pagedRewards,
	}
	if end < total {
		response.NextPageToken = base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(end)))
	}

	w.Header().Set("Content-Type", "application/json")
	if isHit {
		w.Header().Set("X-Cache", "HIT")
	} else {
		w.Header().Set("X-Cache", "MISS")
	}
	json.NewEncoder(w).Encode(response)
}

func (p *PendingRewards) getCacheKey(addresses, nodeIds string) string {
	if nodeIds != "" {
		return cacheKeyPrefixNode + nodeIds
	}
	return cacheKeyPrefixAddr + normalizeAddr(addresses)
}

func (p *PendingRewards) getFromCache(key string) *cachedPendingRewards {
	p.mu.RLock()
	defer p.mu.RUnlock()

	data, closer, err := p.cacheDB.Get([]byte(key))
	if err != nil {
		return nil
	}
	defer closer.Close()

	var cached cachedPendingRewards
	if json.Unmarshal(data, &cached) != nil {
		return nil
	}

	return &cached
}

func (p *PendingRewards) setCache(key string, data *cachedPendingRewards) {
	p.mu.Lock()
	defer p.mu.Unlock()

	encoded, err := json.Marshal(data)
	if err != nil {
		log.Printf("[pending_rewards] cache encode error: %v", err)
		return
	}

	if err := p.cacheDB.Set([]byte(key), encoded, nil); err != nil {
		log.Printf("[pending_rewards] cache write error: %v", err)
	}
}

func (p *PendingRewards) updateProgress(rewards []pendingRewardEntry) {
	now := time.Now().Unix()
	for i := range rewards {
		rewards[i].Progress = calcProgress(rewards[i].StartTimestamp, rewards[i].EndTimestamp, now)
	}
}

// ============ Helper Types ============

type validatorInfo struct {
	TxID                  string `json:"txID"`
	NodeID                string `json:"nodeID"`
	StartTime             string `json:"startTime"`
	EndTime               string `json:"endTime"`
	Weight                string `json:"weight"`
	PotentialReward       string `json:"potentialReward"`
	DelegationFee         string `json:"delegationFee"`
	ValidationRewardOwner *struct {
		Addresses []string `json:"addresses"`
	} `json:"validationRewardOwner"`
	DelegationRewardOwner *struct {
		Addresses []string `json:"addresses"`
	} `json:"delegationRewardOwner"`
	Delegators []delegatorInfo `json:"delegators"`
}

type delegatorInfo struct {
	TxID            string `json:"txID"`
	StartTime       string `json:"startTime"`
	EndTime         string `json:"endTime"`
	Weight          string `json:"weight"`
	PotentialReward string `json:"potentialReward"`
	RewardOwner     *struct {
		Addresses []string `json:"addresses"`
	} `json:"rewardOwner"`
}

func matchAddrs(addrStrs []string, querySet map[string]bool) bool {
	for _, a := range addrStrs {
		if querySet[a] {
			return true
		}
	}
	return false
}

func formatAddresses(addrs []string) []string {
	result := make([]string, len(addrs))
	for i, a := range addrs {
		if len(a) > 2 && a[:2] == "P-" {
			result[i] = a[2:]
		} else {
			result[i] = a
		}
	}
	return result
}

func calcProgress(start, end, now int64) float64 {
	if now <= start {
		return 0
	}
	if now >= end {
		return 100
	}
	return float64(now-start) / float64(end-start) * 100
}

func splitReward(totalAmount uint64, shares uint32) (validatorShare, delegatorShare uint64) {
	remainderShares := PercentDenominator - uint64(shares)
	delegatorShare = remainderShares * totalAmount / PercentDenominator
	validatorShare = totalAmount - delegatorShare
	return
}
