// Experiment: Compare Node RPC vs Glacier for pending rewards
//
// KEY FINDINGS:
// 1. RPC without nodeIDs filter → returns validators but NO delegators
// 2. RPC with MULTIPLE nodeIDs → returns validators but NO delegators
// 3. RPC with SINGLE nodeID → returns validator WITH full delegator details
// 4. getCurrentValidators returns only ACTIVE stakers
// 5. REWARDS ARE AUTO-DISTRIBUTED! No manual claim needed.
//    - When staker's endTime arrives, RewardValidatorTx is auto-issued
//    - Staker is removed from current set, rewards distributed as UTXOs
//    - Use platform.getRewardUTXOs(txID) to check if rewards were distributed
// 6. GLACIER HAS STALE DATA - may show already-rewarded stakers as "pending"
//    - Verified: "missing" entries have reward UTXOs (already rewarded!)
//    - Our RPC-based approach is MORE CORRECT than Glacier
//
// ALGORITHM:
// 1. Query ALL validators (no nodeIDs) - find ones where address is reward owner
// 2. For each matching validator, query INDIVIDUALLY to get delegators
// 3. Build VALIDATOR entries from validationRewardOwner matches
// 4. Build VALIDATOR_FEE entries from delegators where address is delegationRewardOwner
// 5. Build DELEGATOR entries from delegators where address is rewardOwner

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

const (
	nodeURL    = "https://api.avax.network/ext/bc/P"
	glacierURL = "https://glacier-api.avax.network"
	testAddr   = "avax10f8305248c0wsfsdempdtpx7lpkc30vwzl9y9q"
)

type RPCResponse struct {
	Result struct {
		Validators []Validator `json:"validators"`
	} `json:"result"`
}

type Validator struct {
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
	Delegators []Delegator `json:"delegators"`
}

type Delegator struct {
	TxID            string `json:"txID"`
	StartTime       string `json:"startTime"`
	EndTime         string `json:"endTime"`
	Weight          string `json:"weight"`
	PotentialReward string `json:"potentialReward"`
	RewardOwner     *struct {
		Addresses []string `json:"addresses"`
	} `json:"rewardOwner"`
}

type GlacierReward struct {
	TxHash     string   `json:"txHash"`
	NodeID     string   `json:"nodeId"`
	RewardType string   `json:"rewardType"`
	Addresses  []string `json:"addresses"`
}

type GlacierResponse struct {
	PendingRewards []GlacierReward `json:"pendingRewards"`
	NextPageToken  string          `json:"nextPageToken"`
}

func main() {
	queryAddr := "P-" + testAddr

	fmt.Println("=== Step 1: Query ALL validators (no nodeIDs) ===")
	allValidators := queryNode(nil)
	fmt.Printf("Got %d validators\n", len(allValidators))

	// Find nodeIDs where our address is validation or delegation reward owner
	var matchingNodeIDs []string
	for _, v := range allValidators {
		matched := false
		if v.ValidationRewardOwner != nil {
			for _, addr := range v.ValidationRewardOwner.Addresses {
				if addr == queryAddr {
					matched = true
					break
				}
			}
		}
		if !matched && v.DelegationRewardOwner != nil {
			for _, addr := range v.DelegationRewardOwner.Addresses {
				if addr == queryAddr {
					matched = true
					break
				}
			}
		}
		if matched {
			matchingNodeIDs = append(matchingNodeIDs, v.NodeID)
		}
	}
	fmt.Printf("Found %d validators matching query address\n", len(matchingNodeIDs))

	fmt.Println("\n=== Step 2: Query EACH nodeID individually (gets delegator details) ===")
	var validators []Validator
	totalDelegators := 0
	for _, nodeID := range matchingNodeIDs {
		// Query ONE nodeID at a time - RPC only returns delegators for single nodeID!
		result := queryNode([]string{nodeID})
		if len(result) > 0 {
			validators = append(validators, result[0])
			totalDelegators += len(result[0].Delegators)
		}
	}
	fmt.Printf("Got %d validators with delegator details\n", len(validators))
	fmt.Printf("Total delegators across all validators: %d\n", totalDelegators)

	// Now build results
	var nodeResults []string
	for _, v := range validators {
		// Check validation reward owner -> VALIDATOR entry
		if v.ValidationRewardOwner != nil {
			for _, addr := range v.ValidationRewardOwner.Addresses {
				if addr == queryAddr {
					nodeResults = append(nodeResults, fmt.Sprintf("VALIDATOR %s %s", v.TxID, v.NodeID))
					break
				}
			}
		}

		// Check delegation reward owner -> VALIDATOR_FEE entries (one per delegator)
		if v.DelegationRewardOwner != nil {
			isDelegationOwner := false
			for _, addr := range v.DelegationRewardOwner.Addresses {
				if addr == queryAddr {
					isDelegationOwner = true
					break
				}
			}
			if isDelegationOwner {
				// Each delegator generates a VALIDATOR_FEE entry
				for _, del := range v.Delegators {
					nodeResults = append(nodeResults, fmt.Sprintf("VALIDATOR_FEE %s %s", del.TxID, v.NodeID))
				}
			}
		}

		// Check delegators -> DELEGATOR entry
		for _, del := range v.Delegators {
			if del.RewardOwner != nil {
				for _, addr := range del.RewardOwner.Addresses {
					if addr == queryAddr {
						nodeResults = append(nodeResults, fmt.Sprintf("DELEGATOR %s %s", del.TxID, v.NodeID))
						break
					}
				}
			}
		}
	}

	fmt.Printf("Node found %d matching entries:\n", len(nodeResults))
	sort.Strings(nodeResults)
	for _, r := range nodeResults {
		fmt.Println("  ", r)
	}

	fmt.Println("\n=== Querying Glacier ===")
	glacierRewards := queryGlacier(testAddr)

	var glacierResults []string
	for _, r := range glacierRewards {
		glacierResults = append(glacierResults, fmt.Sprintf("%s %s %s", r.RewardType, r.TxHash, r.NodeID))
	}

	fmt.Printf("Glacier found %d entries:\n", len(glacierResults))
	sort.Strings(glacierResults)
	for _, r := range glacierResults {
		fmt.Println("  ", r)
	}

	fmt.Println("\n=== Comparison ===")
	// Count by type
	nodeByType := countByType(nodeResults)
	glacierByType := countByTypeGlacier(glacierRewards)

	fmt.Println("Node counts:")
	for t, c := range nodeByType {
		fmt.Printf("  %s: %d\n", t, c)
	}
	fmt.Println("Glacier counts:")
	for t, c := range glacierByType {
		fmt.Printf("  %s: %d\n", t, c)
	}

	// Find missing entries
	nodeSet := make(map[string]bool)
	for _, r := range nodeResults {
		parts := splitResult(r)
		if len(parts) >= 2 {
			nodeSet[parts[1]] = true // txHash
		}
	}

	var missingTxIDs []string
	for _, r := range glacierRewards {
		if !nodeSet[r.TxHash] {
			missingTxIDs = append(missingTxIDs, r.TxHash)
		}
	}

	if len(missingTxIDs) > 0 {
		fmt.Printf("\n=== In Glacier but NOT in Node (%d entries) ===\n", len(missingTxIDs))
		fmt.Println("Checking if already rewarded (Glacier stale data?)...")
		now := time.Now().Unix()

		for _, txID := range missingTxIDs {
			txInfo := getTxInfo(txID)
			rewardCount := getRewardUTXOCount(txID)

			status := "ACTIVE"
			if txInfo.EndTime > 0 && txInfo.EndTime < now {
				if rewardCount > 0 {
					status = "ALREADY REWARDED (Glacier stale!)"
				} else {
					status = "ENDED (processing...)"
				}
			} else if txInfo.StartTime > now {
				status = "PENDING (not started)"
			}
			fmt.Printf("  %s: end=%s rewardUTXOs=%d status=%s\n",
				txID[:20]+"...",
				time.Unix(txInfo.EndTime, 0).Format("2006-01-02"),
				rewardCount,
				status)
		}
	}
}

func queryNode(nodeIDs []string) []Validator {
	params := map[string]any{}
	if len(nodeIDs) > 0 {
		params["nodeIDs"] = nodeIDs
	}

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "platform.getCurrentValidators",
		"params":  params,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(nodeURL, "application/json", bytes.NewBuffer(bodyBytes))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var rpcResp RPCResponse
	json.Unmarshal(data, &rpcResp)
	return rpcResp.Result.Validators
}

func queryGlacier(addr string) []GlacierReward {
	var allRewards []GlacierReward
	pageToken := ""

	for {
		url := fmt.Sprintf("%s/v1/networks/mainnet/rewards:listPending?addresses=%s&pageSize=100", glacierURL, addr)
		if pageToken != "" {
			url += "&pageToken=" + pageToken
		}

		resp, err := http.Get(url)
		if err != nil {
			panic(err)
		}

		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var glacierResp GlacierResponse
		json.Unmarshal(data, &glacierResp)

		allRewards = append(allRewards, glacierResp.PendingRewards...)

		if glacierResp.NextPageToken == "" {
			break
		}
		pageToken = glacierResp.NextPageToken
	}

	return allRewards
}

func countByType(results []string) map[string]int {
	counts := make(map[string]int)
	for _, r := range results {
		if len(r) > 10 {
			t := r[:10]
			if t[:9] == "VALIDATOR" {
				if len(r) > 12 && r[:13] == "VALIDATOR_FEE" {
					counts["VALIDATOR_FEE"]++
				} else {
					counts["VALIDATOR"]++
				}
			} else if t[:9] == "DELEGATOR" {
				counts["DELEGATOR"]++
			}
		}
	}
	return counts
}

func countByTypeGlacier(rewards []GlacierReward) map[string]int {
	counts := make(map[string]int)
	for _, r := range rewards {
		counts[r.RewardType]++
	}
	return counts
}

func splitResult(r string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(r); i++ {
		if r[i] == ' ' {
			parts = append(parts, r[start:i])
			start = i + 1
		}
	}
	if start < len(r) {
		parts = append(parts, r[start:])
	}
	return parts
}

type TxInfo struct {
	StartTime int64
	EndTime   int64
}

func getTxInfo(txID string) TxInfo {
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "platform.getTx",
		"params": map[string]any{
			"txID":     txID,
			"encoding": "json",
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(nodeURL, "application/json", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return TxInfo{}
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result struct {
		Result struct {
			Tx struct {
				UnsignedTx struct {
					Validator struct {
						Start int64 `json:"start"`
						End   int64 `json:"end"`
					} `json:"validator"`
				} `json:"unsignedTx"`
			} `json:"tx"`
		} `json:"result"`
	}
	json.Unmarshal(data, &result)

	return TxInfo{
		StartTime: result.Result.Tx.UnsignedTx.Validator.Start,
		EndTime:   result.Result.Tx.UnsignedTx.Validator.End,
	}
}

func getRewardUTXOCount(txID string) int {
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "platform.getRewardUTXOs",
		"params": map[string]any{
			"txID": txID,
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(nodeURL, "application/json", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result struct {
		Result struct {
			UTXOs []any `json:"utxos"`
		} `json:"result"`
	}
	json.Unmarshal(data, &result)

	return len(result.Result.UTXOs)
}
