package utxos

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/pebble/v2"
)

// Asset IDs for AVAX on different networks
const (
	mainnetAvaxAssetID = "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z"
	fujiAvaxAssetID    = "U8iRqJoiJm8xZHAacmvYyZVwqQx6uDNtQeP3CQ6fcgQk3JqnK"
)

// X-Chain IDs
// Removed local constants, using them from utxos.go

func getAvaxAssetID(networkID uint32) string {
	if networkID == 5 {
		return fujiAvaxAssetID
	}
	return mainnetAvaxAssetID
}

// RegisterRoutes adds HTTP handlers for UTXOs and balances.
func (u *UTXOs) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/networks/{network}/blockchains/{blockchainId}/utxos", u.handleUTXOs)
	mux.HandleFunc("GET /v1/networks/{network}/blockchains/{blockchainId}/balances", u.handleBalances)
}

func (u *UTXOs) handleUTXOs(w http.ResponseWriter, r *http.Request) {
	addrs := parseAddrs(r.URL.Query().Get("addresses"))
	if len(addrs) == 0 {
		http.Error(w, "addresses required", http.StatusBadRequest)
		return
	}

	pageSize, startIndex := parsePagination(r)
	includeSpent := r.URL.Query().Get("includeSpent") == "true"
	sortBy := r.URL.Query().Get("sortBy")
	sortOrder := r.URL.Query().Get("sortOrder")

	assetIDFilter := r.URL.Query().Get("assetId")
	minAmountStr := r.URL.Query().Get("minUtxoAmount")
	var minAmount uint64
	if minAmountStr != "" {
		minAmount, _ = strconv.ParseUint(minAmountStr, 10, 64)
	}

	if sortOrder == "" {
		sortOrder = "desc"
	}

	// Determine chain from path
	chain := r.PathValue("blockchainId")
	var utxoPrefix, addrPrefix, chainName, currentChainID string

	switch chain {
	case "x-chain", xChainIDFuji, xChainIDMainnet:
		utxoPrefix = prefixXChainUTXO
		addrPrefix = prefixXChainAddr
		chainName = "x-chain"
		currentChainID = xChainIDFuji
		if u.networkID == 1 {
			currentChainID = xChainIDMainnet
		}
	case "c-chain", cChainIDFuji, cChainIDMainnet:
		utxoPrefix = prefixCChainUTXO
		addrPrefix = prefixCChainAddr
		chainName = "c-chain"
		currentChainID = cChainIDFuji
		if u.networkID == 1 {
			currentChainID = cChainIDMainnet
		}
	case "p-chain", pChainID:
		utxoPrefix = prefixPChainUTXO
		addrPrefix = prefixPChainAddr
		chainName = "p-chain"
		currentChainID = pChainID
	default:
		http.Error(w, fmt.Sprintf("unknown blockchainId: %s", chain), http.StatusNotFound)
		return
	}

	if sortBy == "" {
		sortBy = "timestamp"
	}
	if sortOrder == "" {
		sortOrder = "desc"
	}

	utxos := u.getUTXOsForAddresses(addrs, addrPrefix, utxoPrefix, currentChainID, includeSpent, true, sortBy, sortOrder, 0, assetIDFilter, minAmount)

	// Paginate
	total := len(utxos)
	endIndex := startIndex + pageSize
	if endIndex > total {
		endIndex = total
	}
	if startIndex > total {
		startIndex = total
	}

	// Convert StoredUTXO to API response format based on chain
	apiUTXOs := make([]map[string]any, 0, endIndex-startIndex)
	for _, utxo := range utxos[startIndex:endIndex] {
		if chainName == "p-chain" {
			apiUTXOs = append(apiUTXOs, u.toPChainResponse(utxo))
		} else {
			apiUTXOs = append(apiUTXOs, u.toXorCChainResponse(utxo))
		}
	}

	network := r.PathValue("network")
	if network == "testnet" {
		network = "fuji"
	}
	resp := map[string]any{
		"utxos": apiUTXOs,
		"chainInfo": map[string]any{
			"chainName": chainName,
			"network":   network,
		},
	}
	if endIndex < total {
		resp["nextPageToken"] = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d", endIndex)))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (u *UTXOs) handleBalances(w http.ResponseWriter, r *http.Request) {
	addrs := parseAddrs(r.URL.Query().Get("addresses"))
	if len(addrs) == 0 {
		http.Error(w, "addresses required", http.StatusBadRequest)
		return
	}

	blockTimestampStr := r.URL.Query().Get("blockTimestamp")
	var blockTimestamp int64
	if blockTimestampStr != "" {
		blockTimestamp, _ = strconv.ParseInt(blockTimestampStr, 10, 64)
	}

	// Determine chain from path
	chain := r.PathValue("blockchainId")
	var utxoPrefix, addrPrefix, chainName, currentChainID string

	switch chain {
	case "x-chain", xChainIDFuji, xChainIDMainnet:
		utxoPrefix = prefixXChainUTXO
		addrPrefix = prefixXChainAddr
		chainName = "x-chain"
		currentChainID = xChainIDFuji
		if u.networkID == 1 {
			currentChainID = xChainIDMainnet
		}
	case "c-chain", cChainIDFuji, cChainIDMainnet:
		utxoPrefix = prefixCChainUTXO
		addrPrefix = prefixCChainAddr
		chainName = "c-chain"
		currentChainID = cChainIDFuji
		if u.networkID == 1 {
			currentChainID = cChainIDMainnet
		}
	case "p-chain", pChainID:
		utxoPrefix = prefixPChainUTXO
		addrPrefix = prefixPChainAddr
		chainName = "p-chain"
		currentChainID = pChainID
	default:
		http.Error(w, fmt.Sprintf("unknown blockchainId: %s", chain), http.StatusNotFound)
		return
	}

	utxos := u.getUTXOsForAddresses(addrs, addrPrefix, utxoPrefix, currentChainID, false, false, "", "asc", blockTimestamp, "", 0)

	addrSet := make(map[string]bool)
	for _, a := range addrs {
		addrSet[a] = true
	}

	// Use chain-specific aggregation
	var balances any
	switch chainName {
	case "p-chain":
		balances = u.aggregatePChainBalances(utxos, addrSet, currentChainID, blockTimestamp)
	case "x-chain":
		balances = u.aggregateXChainBalances(utxos, addrSet, currentChainID, blockTimestamp)
	case "c-chain":
		balances = u.aggregateCChainBalances(utxos, addrSet, currentChainID, blockTimestamp)
	default:
		balances = u.aggregatePChainBalances(utxos, addrSet, currentChainID, blockTimestamp)
	}

	network := r.PathValue("network")
	if network == "testnet" {
		network = "fuji"
	}
	resp := map[string]any{
		"balances": balances,
		"chainInfo": map[string]any{
			"chainName": chainName,
			"network":   network,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// getUTXOsForAddresses returns UTXOs for given addresses using specified prefixes.
// includeConsumedStaked: if true, includes staked UTXOs even if consumed (for Glacier UTXO listing compat)
func (u *UTXOs) getUTXOsForAddresses(addrs []string, addrPrefix, utxoPrefix, currentChainID string, includeSpent, includeConsumedStaked bool, sortBy, sortOrder string, blockTimestamp int64, assetIDFilter string, minAmount uint64) []*StoredUTXO {
	u.mu.RLock()
	defer u.mu.RUnlock()

	// Map utxoPrefix to spend chain prefix
	var spendChainPrefix string
	switch utxoPrefix {
	case prefixXChainUTXO:
		spendChainPrefix = "x:"
	case prefixPChainUTXO:
		spendChainPrefix = "p:"
	case prefixCChainUTXO:
		spendChainPrefix = "c:"
	}

	addrSet := make(map[string]bool)
	for _, a := range addrs {
		addrSet[a] = true
	}

	seen := make(map[string]bool)
	var result []*StoredUTXO

	for _, addr := range addrs {
		// Scan address index: {addrPrefix}{address}:*
		prefix := []byte(addrPrefix + addr + ":")
		upperBound := make([]byte, len(prefix))
		copy(upperBound, prefix)
		upperBound[len(upperBound)-1]++

		iter, err := u.db.NewIter(&pebble.IterOptions{
			LowerBound: prefix,
			UpperBound: upperBound,
		})
		if err != nil {
			continue
		}

		for iter.First(); iter.Valid(); iter.Next() {
			// Extract utxoID from key: {addrPrefix}{address}:{utxoID}
			key := string(iter.Key())
			utxoID := key[len(addrPrefix)+len(addr)+1:]

			if seen[utxoID] {
				continue
			}
			seen[utxoID] = true

			// Load UTXO data
			stored := u.loadUTXO(utxoPrefix, utxoID)
			if stored == nil {
				continue
			}

			// Check spend index and merge if found
			if spendChainPrefix != "" {
				if spendInfo := u.getSpendInfo(spendChainPrefix, utxoID); spendInfo != nil {
					stored.ConsumingTxHash = &spendInfo.ConsumingTxHash
					stored.ConsumingBlockTimestamp = &spendInfo.ConsumingTime
					if spendInfo.ConsumingBlockNumber != "" {
						stored.ConsumingBlockNumber = &spendInfo.ConsumingBlockNumber
					}
					if len(spendInfo.Credentials) > 0 {
						stored.Credentials = spendInfo.Credentials
					}
					if spendInfo.ConsumedOnChainID != "" {
						stored.ConsumedOnChainID = spendInfo.ConsumedOnChainID
					}
					if spendInfo.CreatedOnChainID != "" {
						stored.CreatedOnChainID = spendInfo.CreatedOnChainID
					}
				}
			}

			// Skip spent UTXOs unless includeSpent is true
			// Exception: Include consumed staked UTXOs if includeConsumedStaked is true
			// AND the staking period hasn't ended yet (Glacier only shows active stakers)
			if blockTimestamp > 0 {
				// Historical Filtering:
				// 1. Must be created BEFORE blockTimestamp (Glacier is exclusive of creation at T)
				if stored.BlockTimestamp >= blockTimestamp {
					continue
				}
				// 2. Must NOT be consumed before or AT blockTimestamp (Glacier is inclusive of consumption at T)
				if stored.ConsumingBlockTimestamp != nil && *stored.ConsumingBlockTimestamp <= blockTimestamp {
					continue
				}
			} else if !includeSpent {
				if stored.ConsumingTxHash != nil {
					// For staked UTXOs, only show if:
					// 1. includeConsumedStaked is true, AND
					// 2. The staking period hasn't ended (utxoEndTimestamp > now)
					if stored.Staked && includeConsumedStaked {
						now := time.Now().Unix()
						if blockTimestamp > 0 {
							now = blockTimestamp
						}
						if stored.UTXOEndTimestamp != nil && *stored.UTXOEndTimestamp > now {
							// Active staker - show it
						} else {
							// Staking ended - don't show
							continue
						}
					} else {
						continue
					}
				}
			}

			// Check threshold: query addresses must be able to spend
			overlap := 0
			for _, a := range stored.Addresses {
				if addrSet[a] {
					overlap++
				}
			}
			if uint32(overlap) < stored.Threshold {
				continue
			}

			// Asset Filter
			if assetIDFilter != "" && stored.AssetID != assetIDFilter {
				continue
			}

			// Min Amount Filter
			if minAmount > 0 {
				amt, _ := strconv.ParseUint(stored.Amount, 10, 64)
				if amt < minAmount {
					continue
				}
			}

			result = append(result, stored)
		}
		iter.Close()
	}

	// Sort results
	sortResults(result, sortBy, sortOrder)

	return result
}

// sortResults sorts UTXOs by the specified field and order.
func sortResults(utxos []*StoredUTXO, sortBy, sortOrder string) {
	desc := sortOrder == "desc"

	switch sortBy {
	case "timestamp":
		sort.Slice(utxos, func(i, j int) bool {
			// Primary: timestamp
			ti, tj := utxos[i].BlockTimestamp, utxos[j].BlockTimestamp
			if ti != tj {
				if desc {
					return ti > tj
				}
				return ti < tj
			}

			// Secondary: utxo_id (Glacier uses this as tie-breaker in the same direction)
			if desc {
				return utxos[i].UTXOId > utxos[j].UTXOId
			}
			return utxos[i].UTXOId < utxos[j].UTXOId
		})
	case "amount":
		sort.SliceStable(utxos, func(i, j int) bool {
			ai, _ := strconv.ParseUint(utxos[i].Amount, 10, 64)
			aj, _ := strconv.ParseUint(utxos[j].Amount, 10, 64)
			if ai != aj {
				if desc {
					return ai > aj
				}
				return ai < aj
			}
			// Tie-breaker: utxoId in same direction as primary sort
			if desc {
				return utxos[i].UTXOId > utxos[j].UTXOId
			}
			return utxos[i].UTXOId < utxos[j].UTXOId
		})
	default:
		// Default: sort by utxoId for consistent pagination
		sort.SliceStable(utxos, func(i, j int) bool {
			if desc {
				return utxos[i].UTXOId > utxos[j].UTXOId
			}
			return utxos[i].UTXOId < utxos[j].UTXOId
		})
	}
}

// toPChainResponse converts a StoredUTXO to P-Chain API response format.
func (u *UTXOs) toPChainResponse(stored *StoredUTXO) map[string]any {
	assetID := stored.AssetID

	result := map[string]any{
		"utxoId":            stored.UTXOId,
		"txHash":            stored.TxHash,
		"outputIndex":       stored.OutputIndex,
		"amount":            stored.Amount,
		"assetId":           assetID,
		"addresses":         stored.Addresses,
		"threshold":         stored.Threshold,
		"utxoType":          stored.UTXOType,
		"staked":            stored.Staked,
		"blockNumber":       stored.BlockNumber,
		"createdOnChainId":  stored.CreatedOnChainID,
		"consumedOnChainId": stored.ConsumedOnChainID,
		"asset": map[string]any{
			"assetId":      assetID,
			"name":         u.assetName(assetID),
			"symbol":       u.assetSymbol(assetID),
			"denomination": u.assetDenom(assetID),
			"type":         "secp256k1",
			"amount":       stored.Amount,
		},
	}

	// Only add platformLocktime if not nil AND UTXO is native P-Chain (not cross-chain)
	// Cross-chain UTXOs (created or consumed on different chain) don't have platformLocktime
	isNativePChain := stored.CreatedOnChainID == pChainID && stored.ConsumedOnChainID == pChainID
	if stored.PlatformLocktime != nil && isNativePChain {
		result["platformLocktime"] = *stored.PlatformLocktime
	}

	// Add optional fields only if they have values
	if stored.BlockTimestamp > 0 {
		result["blockTimestamp"] = stored.BlockTimestamp
	}
	if stored.UTXOBytes != "" {
		checksummed, err := appendChecksum(stored.UTXOBytes)
		if err != nil {
			// Log error but include original bytes for debugging
			result["utxoBytes"] = stored.UTXOBytes
			result["_utxoBytesError"] = err.Error()
		} else {
			result["utxoBytes"] = checksummed
		}
	}
	if stored.UTXOStartTimestamp != nil {
		result["utxoStartTimestamp"] = *stored.UTXOStartTimestamp
	}
	if stored.UTXOEndTimestamp != nil {
		result["utxoEndTimestamp"] = *stored.UTXOEndTimestamp
	}
	// Add consumption fields only if they have values
	// Exception: Don't include consumption fields for staked UTXOs (Glacier compatibility)
	if !stored.Staked {
		if stored.ConsumingTxHash != nil {
			result["consumingTxHash"] = *stored.ConsumingTxHash
		}
		if stored.ConsumingBlockNumber != nil {
			result["consumingBlockNumber"] = *stored.ConsumingBlockNumber
		}
		if stored.ConsumingBlockTimestamp != nil {
			result["consumingBlockTimestamp"] = *stored.ConsumingBlockTimestamp
		}
	}

	return result
}

// toXorCChainResponse converts a StoredUTXO to X-Chain or C-Chain API response format.
// Different field names and types than P-Chain!
func (u *UTXOs) toXorCChainResponse(stored *StoredUTXO) map[string]any {
	assetID := stored.AssetID

	result := map[string]any{
		"utxoId":            stored.UTXOId,
		"creationTxHash":    stored.TxHash,                         // Different name!
		"outputIndex":       fmt.Sprintf("%d", stored.OutputIndex), // STRING!
		"timestamp":         stored.BlockTimestamp,                 // Different name!
		"locktime":          u.getLocktime(stored),                 // Not platformLocktime
		"utxoType":          strings.ToLower(stored.UTXOType),      // lowercase
		"addresses":         stored.Addresses,
		"threshold":         stored.Threshold,
		"createdOnChainId":  stored.CreatedOnChainID,
		"consumedOnChainId": stored.ConsumedOnChainID,
		"asset": map[string]any{
			"assetId":      assetID,
			"name":         u.assetName(assetID),
			"symbol":       u.assetSymbol(assetID),
			"denomination": u.assetDenom(assetID),
			"type":         u.assetType(stored), // NEW: dynamic type
			"amount":       stored.Amount,
		},
	}

	// Add payload and groupId if present (NFTs)
	if stored.Payload != "" {
		result["payload"] = stored.Payload
	}
	if stored.GroupID != nil {
		result["groupId"] = *stored.GroupID
	}

	// Add utxoBytes if present
	if stored.UTXOBytes != "" {
		checksummed, err := appendChecksum(stored.UTXOBytes)
		if err == nil {
			result["utxoBytes"] = checksummed
		}
	}

	// Consumption fields - different names for C-Chain
	if stored.ConsumingTxHash != nil {
		result["consumingTxHash"] = *stored.ConsumingTxHash
	}
	if stored.ConsumingBlockTimestamp != nil {
		result["consumingTxTimestamp"] = *stored.ConsumingBlockTimestamp // Different name!
	}

	// Credentials (C-Chain only)
	if len(stored.Credentials) > 0 {
		result["credentials"] = stored.Credentials
	}

	return result
}

// getLocktime returns the locktime value for C-Chain response.
func (u *UTXOs) getLocktime(stored *StoredUTXO) int64 {
	if stored.PlatformLocktime != nil {
		return int64(*stored.PlatformLocktime)
	}
	return 0
}

// ============ Balance Aggregation ============

type AssetBalance struct {
	AssetID      string `json:"assetId"`
	Name         string `json:"name"`
	Symbol       string `json:"symbol"`
	Denomination int    `json:"denomination"`
	Type         string `json:"type"`
	Amount       string `json:"amount"`
	UTXOCount    int    `json:"utxoCount"`
}

// ============ P-Chain Balance Aggregation ============
// Returns: unlockedUnstaked, unlockedStaked, lockedPlatform, lockedStakeable,
//          lockedStaked, pendingStaked, atomicMemoryUnlocked, atomicMemoryLocked

type PChainSharedAsset struct {
	AssetID           string `json:"assetId"`
	Name              string `json:"name"`
	Symbol            string `json:"symbol"`
	Denomination      int    `json:"denomination"`
	Type              string `json:"type"`
	Amount            string `json:"amount"`
	UTXOCount         int    `json:"utxoCount"`
	SharedWithChainID string `json:"sharedWithChainId"`
	Status            string `json:"status"`
}

func (u *UTXOs) aggregatePChainBalances(utxos []*StoredUTXO, queryAddrs map[string]bool, pChainIDVal string, blockTimestamp int64) map[string]any {
	now := time.Now().Unix()
	if blockTimestamp > 0 {
		now = blockTimestamp
	}

	buckets := map[string]map[string]*balanceAccum{
		"unlockedUnstaked": {},
		"lockedPlatform":   {},
		"lockedStakeable":  {},
		"unlockedStaked":   {},
		"lockedStaked":     {},
		"pendingStaked":    {},
	}

	// Separate buckets for atomic memory (they have different schema with sharedWithChainId)
	atomicUnlockedByAsset := make(map[string]*atomicBalanceAccum)
	atomicLockedByAsset := make(map[string]*atomicBalanceAccum)

	for _, utxo := range utxos {
		amount, _ := strconv.ParseUint(utxo.Amount, 10, 64)

		var locktime int64
		if utxo.PlatformLocktime != nil {
			locktime = int64(*utxo.PlatformLocktime)
		}
		isLocked := locktime > now

		// Check if UTXO is in atomic memory (exported to another chain)
		isAtomic := utxo.ConsumedOnChainID != pChainIDVal && utxo.ConsumedOnChainID != ""

		if isAtomic {
			sharedWith := utxo.ConsumedOnChainID
			if isLocked {
				if atomicLockedByAsset[utxo.AssetID] == nil {
					atomicLockedByAsset[utxo.AssetID] = &atomicBalanceAccum{
						assetID:       utxo.AssetID,
						name:          u.assetName(utxo.AssetID),
						symbol:        u.assetSymbol(utxo.AssetID),
						denom:         u.assetDenom(utxo.AssetID),
						sharedWithMap: make(map[string]bool),
					}
				}
				atomicLockedByAsset[utxo.AssetID].amount += amount
				atomicLockedByAsset[utxo.AssetID].count++
				atomicLockedByAsset[utxo.AssetID].sharedWithMap[sharedWith] = true
			} else {
				if atomicUnlockedByAsset[utxo.AssetID] == nil {
					atomicUnlockedByAsset[utxo.AssetID] = &atomicBalanceAccum{
						assetID:       utxo.AssetID,
						name:          u.assetName(utxo.AssetID),
						symbol:        u.assetSymbol(utxo.AssetID),
						denom:         u.assetDenom(utxo.AssetID),
						sharedWithMap: make(map[string]bool),
					}
				}
				atomicUnlockedByAsset[utxo.AssetID].amount += amount
				atomicUnlockedByAsset[utxo.AssetID].count++
				atomicUnlockedByAsset[utxo.AssetID].sharedWithMap[sharedWith] = true
			}
		} else if utxo.Staked {
			// Check if staking hasn't started yet (pending)
			isPending := false
			if utxo.UTXOStartTimestamp != nil && *utxo.UTXOStartTimestamp > now {
				isPending = true
			}

			// Check if staking has ended
			isEnded := false
			if utxo.UTXOEndTimestamp != nil && *utxo.UTXOEndTimestamp <= now {
				isEnded = true
			}

			var category string
			if isEnded {
				if isLocked {
					category = "lockedNotStakeable"
				} else {
					category = "unlockedUnstaked"
				}
			} else if isPending {
				category = "pendingStaked"
			} else if isLocked {
				category = "lockedStaked"
			} else {
				category = "unlockedStaked"
			}

			bucket := buckets[category]
			if bucket[utxo.AssetID] == nil {
				bucket[utxo.AssetID] = &balanceAccum{
					assetID: utxo.AssetID,
					name:    u.assetName(utxo.AssetID),
					symbol:  u.assetSymbol(utxo.AssetID),
					denom:   u.assetDenom(utxo.AssetID),
				}
			}
			bucket[utxo.AssetID].amount += amount
			bucket[utxo.AssetID].count++
		} else {
			// Non-staked, non-atomic
			var category string
			if isLocked {
				if utxo.UTXOType == "STAKEABLE_LOCK" {
					category = "lockedStakeable"
				} else {
					category = "lockedPlatform"
				}
			} else {
				category = "unlockedUnstaked"
			}

			bucket := buckets[category]
			if bucket[utxo.AssetID] == nil {
				bucket[utxo.AssetID] = &balanceAccum{
					assetID: utxo.AssetID,
					name:    u.assetName(utxo.AssetID),
					symbol:  u.assetSymbol(utxo.AssetID),
					denom:   u.assetDenom(utxo.AssetID),
				}
			}
			bucket[utxo.AssetID].amount += amount
			bucket[utxo.AssetID].count++
		}
	}

	result := make(map[string]any)
	for name, bucket := range buckets {
		result[name] = bucketToBalances(bucket)
	}

	// Convert atomic balances (with sharedWithChainId)
	result["atomicMemoryUnlocked"] = atomicBucketToPChainSharedAssets(atomicUnlockedByAsset)
	result["atomicMemoryLocked"] = atomicBucketToPChainSharedAssets(atomicLockedByAsset)

	return result
}

// ============ X-Chain Balance Aggregation ============
// Returns: locked, unlocked, atomicMemoryUnlocked, atomicMemoryLocked

func (u *UTXOs) aggregateXChainBalances(utxos []*StoredUTXO, queryAddrs map[string]bool, xChainIDVal string, blockTimestamp int64) map[string]any {
	now := time.Now().Unix()
	if blockTimestamp > 0 {
		now = blockTimestamp
	}

	lockedByAsset := make(map[string]*balanceAccum)
	unlockedByAsset := make(map[string]*balanceAccum)
	atomicUnlockedByAsset := make(map[string]*xChainAtomicAccum)
	atomicLockedByAsset := make(map[string]*xChainAtomicAccum)

	for _, utxo := range utxos {
		amount, _ := strconv.ParseUint(utxo.Amount, 10, 64)

		var locktime int64
		if utxo.PlatformLocktime != nil {
			locktime = int64(*utxo.PlatformLocktime)
		}
		isLocked := locktime > now

		// Check if UTXO is in atomic memory (exported to another chain)
		isAtomic := utxo.ConsumedOnChainID != xChainIDVal && utxo.ConsumedOnChainID != ""

		if isAtomic {
			sharedWith := utxo.ConsumedOnChainID
			if isLocked {
				if atomicLockedByAsset[utxo.AssetID] == nil {
					atomicLockedByAsset[utxo.AssetID] = &xChainAtomicAccum{
						assetID:       utxo.AssetID,
						name:          u.assetName(utxo.AssetID),
						symbol:        u.assetSymbol(utxo.AssetID),
						denom:         u.assetDenom(utxo.AssetID),
						sharedWithMap: make(map[string]bool),
					}
				}
				atomicLockedByAsset[utxo.AssetID].amount += amount
				atomicLockedByAsset[utxo.AssetID].count++
				atomicLockedByAsset[utxo.AssetID].sharedWithMap[sharedWith] = true
			} else {
				if atomicUnlockedByAsset[utxo.AssetID] == nil {
					atomicUnlockedByAsset[utxo.AssetID] = &xChainAtomicAccum{
						assetID:       utxo.AssetID,
						name:          u.assetName(utxo.AssetID),
						symbol:        u.assetSymbol(utxo.AssetID),
						denom:         u.assetDenom(utxo.AssetID),
						sharedWithMap: make(map[string]bool),
					}
				}
				atomicUnlockedByAsset[utxo.AssetID].amount += amount
				atomicUnlockedByAsset[utxo.AssetID].count++
				atomicUnlockedByAsset[utxo.AssetID].sharedWithMap[sharedWith] = true
			}
		} else {
			if isLocked {
				if lockedByAsset[utxo.AssetID] == nil {
					lockedByAsset[utxo.AssetID] = &balanceAccum{
						assetID: utxo.AssetID,
						name:    u.assetName(utxo.AssetID),
						symbol:  u.assetSymbol(utxo.AssetID),
						denom:   u.assetDenom(utxo.AssetID),
					}
				}
				lockedByAsset[utxo.AssetID].amount += amount
				lockedByAsset[utxo.AssetID].count++
			} else {
				if unlockedByAsset[utxo.AssetID] == nil {
					unlockedByAsset[utxo.AssetID] = &balanceAccum{
						assetID: utxo.AssetID,
						name:    u.assetName(utxo.AssetID),
						symbol:  u.assetSymbol(utxo.AssetID),
						denom:   u.assetDenom(utxo.AssetID),
					}
				}
				unlockedByAsset[utxo.AssetID].amount += amount
				unlockedByAsset[utxo.AssetID].count++
			}
		}
	}

	return map[string]any{
		"locked":               bucketToBalances(lockedByAsset),
		"unlocked":             bucketToBalances(unlockedByAsset),
		"atomicMemoryUnlocked": xChainAtomicBucketToSharedAssets(atomicUnlockedByAsset),
		"atomicMemoryLocked":   xChainAtomicBucketToSharedAssets(atomicLockedByAsset),
	}
}

// ============ C-Chain Balance Aggregation ============
// Returns only: atomicMemoryUnlocked, atomicMemoryLocked
// C-Chain native balance is EVM and handled by different endpoint

func (u *UTXOs) aggregateCChainBalances(utxos []*StoredUTXO, queryAddrs map[string]bool, cChainIDVal string, blockTimestamp int64) map[string]any {
	now := time.Now().Unix()
	if blockTimestamp > 0 {
		now = blockTimestamp
	}

	atomicUnlockedByAsset := make(map[string]*cChainAtomicAccum)
	atomicLockedByAsset := make(map[string]*cChainAtomicAccum)

	for _, utxo := range utxos {
		amount, _ := strconv.ParseUint(utxo.Amount, 10, 64)

		var locktime int64
		if utxo.PlatformLocktime != nil {
			locktime = int64(*utxo.PlatformLocktime)
		}
		isLocked := locktime > now

		// For C-Chain, ALL UTXOs are atomic memory (shared with P-Chain or X-Chain)
		// because C-Chain native is EVM, not UTXO
		sharedWith := ""
		if utxo.CreatedOnChainID != cChainIDVal && utxo.CreatedOnChainID != "" {
			sharedWith = utxo.CreatedOnChainID
		} else if utxo.ConsumedOnChainID != cChainIDVal && utxo.ConsumedOnChainID != "" {
			sharedWith = utxo.ConsumedOnChainID
		}
		if sharedWith == "" {
			// Default to P-Chain if unknown
			sharedWith = pChainID
		}

		if isLocked {
			if atomicLockedByAsset[utxo.AssetID] == nil {
				atomicLockedByAsset[utxo.AssetID] = &cChainAtomicAccum{
					assetID:       utxo.AssetID,
					name:          u.assetName(utxo.AssetID),
					symbol:        u.assetSymbol(utxo.AssetID),
					denom:         u.assetDenom(utxo.AssetID),
					sharedWithMap: make(map[string]bool),
				}
			}
			atomicLockedByAsset[utxo.AssetID].amount += amount
			atomicLockedByAsset[utxo.AssetID].count++
			atomicLockedByAsset[utxo.AssetID].sharedWithMap[sharedWith] = true
		} else {
			if atomicUnlockedByAsset[utxo.AssetID] == nil {
				atomicUnlockedByAsset[utxo.AssetID] = &cChainAtomicAccum{
					assetID:       utxo.AssetID,
					name:          u.assetName(utxo.AssetID),
					symbol:        u.assetSymbol(utxo.AssetID),
					denom:         u.assetDenom(utxo.AssetID),
					sharedWithMap: make(map[string]bool),
				}
			}
			atomicUnlockedByAsset[utxo.AssetID].amount += amount
			atomicUnlockedByAsset[utxo.AssetID].count++
			atomicUnlockedByAsset[utxo.AssetID].sharedWithMap[sharedWith] = true
		}
	}

	return map[string]any{
		"atomicMemoryUnlocked": cChainAtomicBucketToSharedAssets(atomicUnlockedByAsset),
		"atomicMemoryLocked":   cChainAtomicBucketToSharedAssets(atomicLockedByAsset),
	}
}

// ============ Accumulator Types ============

type balanceAccum struct {
	assetID string
	name    string
	symbol  string
	denom   int
	amount  uint64
	count   int
}

type atomicBalanceAccum struct {
	assetID       string
	name          string
	symbol        string
	denom         int
	amount        uint64
	count         int
	sharedWithMap map[string]bool // Track all chains this is shared with
}

type xChainAtomicAccum struct {
	assetID       string
	name          string
	symbol        string
	denom         int
	amount        uint64
	count         int
	sharedWithMap map[string]bool
}

type cChainAtomicAccum struct {
	assetID       string
	name          string
	symbol        string
	denom         int
	amount        uint64
	count         int
	sharedWithMap map[string]bool
}

func bucketToBalances(m map[string]*balanceAccum) []AssetBalance {
	result := make([]AssetBalance, 0)
	for _, v := range m {
		if v.amount == 0 {
			continue
		}
		result = append(result, AssetBalance{
			AssetID:      v.assetID,
			Name:         v.name,
			Symbol:       v.symbol,
			Denomination: v.denom,
			Type:         "secp256k1",
			Amount:       fmt.Sprintf("%d", v.amount),
			UTXOCount:    v.count,
		})
	}
	return result
}

func atomicBucketToPChainSharedAssets(m map[string]*atomicBalanceAccum) []PChainSharedAsset {
	result := make([]PChainSharedAsset, 0)
	for _, v := range m {
		if v.amount == 0 {
			continue
		}
		// Pick one sharedWithChainId (in practice there's usually just one)
		sharedWith := ""
		for chainID := range v.sharedWithMap {
			sharedWith = chainID
			break
		}
		result = append(result, PChainSharedAsset{
			AssetID:           v.assetID,
			Name:              v.name,
			Symbol:            v.symbol,
			Denomination:      v.denom,
			Type:              "secp256k1",
			Amount:            fmt.Sprintf("%d", v.amount),
			UTXOCount:         v.count,
			SharedWithChainID: sharedWith,
			Status:            "pendingExport", // UTXOs in atomic memory waiting to be imported
		})
	}
	return result
}

// XChainSharedAssetBalance for X-Chain (no status field, simpler than P-Chain)
type XChainSharedAssetBalance struct {
	AssetID           string `json:"assetId"`
	Name              string `json:"name"`
	Symbol            string `json:"symbol"`
	Denomination      int    `json:"denomination"`
	Type              string `json:"type"`
	Amount            string `json:"amount"`
	UTXOCount         int    `json:"utxoCount"`
	SharedWithChainID string `json:"sharedWithChainId"`
}

func xChainAtomicBucketToSharedAssets(m map[string]*xChainAtomicAccum) []XChainSharedAssetBalance {
	result := make([]XChainSharedAssetBalance, 0)
	for _, v := range m {
		if v.amount == 0 {
			continue
		}
		sharedWith := ""
		for chainID := range v.sharedWithMap {
			sharedWith = chainID
			break
		}
		result = append(result, XChainSharedAssetBalance{
			AssetID:           v.assetID,
			Name:              v.name,
			Symbol:            v.symbol,
			Denomination:      v.denom,
			Type:              "secp256k1",
			Amount:            fmt.Sprintf("%d", v.amount),
			UTXOCount:         v.count,
			SharedWithChainID: sharedWith,
		})
	}
	return result
}

// CChainSharedAssetBalance for C-Chain (same schema as X-Chain)
type CChainSharedAssetBalance struct {
	AssetID           string `json:"assetId"`
	Name              string `json:"name"`
	Symbol            string `json:"symbol"`
	Denomination      int    `json:"denomination"`
	Type              string `json:"type"`
	Amount            string `json:"amount"`
	UTXOCount         int    `json:"utxoCount"`
	SharedWithChainID string `json:"sharedWithChainId"`
}

func cChainAtomicBucketToSharedAssets(m map[string]*cChainAtomicAccum) []CChainSharedAssetBalance {
	result := make([]CChainSharedAssetBalance, 0, len(m))
	for _, v := range m {
		sharedWith := ""
		for chainID := range v.sharedWithMap {
			sharedWith = chainID
			break
		}
		result = append(result, CChainSharedAssetBalance{
			AssetID:           v.assetID,
			Name:              v.name,
			Symbol:            v.symbol,
			Denomination:      v.denom,
			Type:              "secp256k1",
			Amount:            fmt.Sprintf("%d", v.amount),
			UTXOCount:         v.count,
			SharedWithChainID: sharedWith,
		})
	}
	return result
}

// ============ Helpers ============

func (u *UTXOs) assetName(assetID string) string {
	if assetID == getAvaxAssetID(u.networkID) {
		return "Avalanche"
	}
	if meta := u.loadAssetMetadata(assetID); meta != nil {
		return meta.Name
	}
	return "Unknown"
}

func (u *UTXOs) assetSymbol(assetID string) string {
	if assetID == getAvaxAssetID(u.networkID) {
		return "AVAX"
	}
	if meta := u.loadAssetMetadata(assetID); meta != nil {
		return meta.Symbol
	}
	return "???"
}

func (u *UTXOs) assetDenom(assetID string) int {
	if assetID == getAvaxAssetID(u.networkID) {
		return 9
	}
	if meta := u.loadAssetMetadata(assetID); meta != nil {
		return meta.Denomination
	}
	return 0
}

func (u *UTXOs) assetType(stored *StoredUTXO) string {
	name := strings.ToLower(u.assetName(stored.AssetID))
	//FIXME: very questionable logic
	if stored.Payload != "" || strings.Contains(name, "nft") || strings.Contains(name, "family") {
		return "nft"
	}
	return "secp256k1"
}

func parseAddrs(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	seen := make(map[string]struct{})
	var addrs []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		addrs = append(addrs, p)
	}
	// Limit to 64 addresses like Glacier
	if len(addrs) > 64 {
		addrs = addrs[:64]
	}
	return addrs
}

func parsePagination(r *http.Request) (pageSize, startIndex int) {
	pageSize = 10
	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		if n, _ := strconv.Atoi(ps); n >= 1 && n <= 100 {
			pageSize = n
		}
	}
	if pt := r.URL.Query().Get("pageToken"); pt != "" {
		if decoded, err := base64.StdEncoding.DecodeString(pt); err == nil {
			startIndex, _ = strconv.Atoi(string(decoded))
		}
	}
	return
}

// appendChecksum appends a 4-byte SHA256 checksum to the hex-encoded UTXO bytes.
// This matches Glacier API's format which includes the last 4 bytes of SHA256(utxoBytes).
func appendChecksum(hexStr string) (string, error) {
	// Strip 0x prefix if present
	hexStr = strings.TrimPrefix(hexStr, "0x")

	// Decode hex to bytes
	bytes, err := hex.DecodeString(hexStr)
	if err != nil {
		return "", fmt.Errorf("failed to decode hex: %w", err)
	}

	// Compute SHA256 hash
	hash := sha256.Sum256(bytes)

	// Append last 4 bytes of hash
	bytesWithChecksum := append(bytes, hash[len(hash)-4:]...)

	// Return as hex with 0x prefix
	return "0x" + hex.EncodeToString(bytesWithChecksum), nil
}
