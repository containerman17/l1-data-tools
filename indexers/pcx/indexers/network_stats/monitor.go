package network_stats

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"sort"
	"sync/atomic"
	"time"

	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/pchain"
)

var (
	keyNetworkStats = []byte("meta:network_stats")
)

type NetworkMonitor struct {
	rpc *pchain.Client

	stats atomic.Pointer[NetworkStats]
}

func NewMonitor(rpc *pchain.Client) *NetworkMonitor {
	return &NetworkMonitor{
		rpc: rpc,
	}
}

func (m *NetworkMonitor) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := m.refresh(ctx)
		if err != nil {
			log.Printf("[network-monitor] refresh error: %v, retrying in 5s", err)
			time.Sleep(5 * time.Second)
			continue
		}

		time.Sleep(30 * time.Second)
	}
}

func (m *NetworkMonitor) GetStats() *NetworkStats {
	return m.stats.Load()
}

func (m *NetworkMonitor) refresh(ctx context.Context) error {
	// 1. Fetch Supply
	supply, err := m.rpc.GetCurrentSupply(ctx)
	if err != nil {
		return fmt.Errorf("fetch supply: %w", err)
	}

	// 2. Fetch Validators
	validatorsRaw, err := m.rpc.GetCurrentValidators(ctx, nil)
	if err != nil {
		return fmt.Errorf("fetch validators: %w", err)
	}

	var valResp struct {
		Validators []struct {
			NodeID          string      `json:"nodeID"`
			Weight          json.Number `json:"weight"`
			Connected       bool        `json:"connected"`
			DelegatorCount  json.Number `json:"delegatorCount"`
			DelegatorWeight json.Number `json:"delegatorWeight"`
			PotentialReward json.Number `json:"potentialReward"`
		} `json:"validators"`
	}
	if err := json.Unmarshal(validatorsRaw, &valResp); err != nil {
		return fmt.Errorf("parse validators: %w", err)
	}

	// 3. Fetch Peers
	peersRaw, err := m.rpc.GetPeers(ctx)
	if err != nil {
		return fmt.Errorf("fetch peers: %w", err)
	}

	var peerResp struct {
		Peers []struct {
			NodeID  string `json:"nodeID"`
			Version string `json:"version"`
		} `json:"peers"`
	}
	if err := json.Unmarshal(peersRaw, &peerResp); err != nil {
		return fmt.Errorf("parse peers: %w", err)
	}

	// Create peer version map
	peerVersions := make(map[string]string)
	for _, p := range peerResp.Peers {
		peerVersions[p.NodeID] = p.Version
	}

	// 4. Aggregation
	totalValStake := big.NewInt(0)
	totalDelStake := big.NewInt(0)
	totalDelCount := 0
	totalPotentialReward := big.NewInt(0)
	valCount := len(valResp.Validators)

	versionBuckets := make(map[string]*big.Int)
	versionCounts := make(map[string]int)

	for _, v := range valResp.Validators {
		weight, _ := big.NewInt(0).SetString(string(v.Weight), 10)
		delWeight, _ := big.NewInt(0).SetString(string(v.DelegatorWeight), 10)
		delCount, _ := v.DelegatorCount.Int64()
		potentialReward, _ := big.NewInt(0).SetString(string(v.PotentialReward), 10)

		totalValStake.Add(totalValStake, weight)
		totalDelStake.Add(totalDelStake, delWeight)
		totalDelCount += int(delCount)
		totalPotentialReward.Add(totalPotentialReward, potentialReward)

		// Version mapping
		version := "unknown"
		if !v.Connected {
			version = "offline"
		} else if v, ok := peerVersions[v.NodeID]; ok {
			version = v
		}

		if _, ok := versionBuckets[version]; !ok {
			versionBuckets[version] = big.NewInt(0)
		}
		versionBuckets[version].Add(versionBuckets[version], weight)
		versionCounts[version]++
	}

	totalStaked := big.NewInt(0).Add(totalValStake, totalDelStake)

	// Build distribution list
	var distribution []VersionStatistic
	for ver, weight := range versionBuckets {
		distribution = append(distribution, VersionStatistic{
			Version:        ver,
			AmountStaked:   weight.String(),
			ValidatorCount: versionCounts[ver],
		})
	}
	// Sort by stake descending
	sort.Slice(distribution, func(i, j int) bool {
		ai, _ := big.NewInt(0).SetString(distribution[i].AmountStaked, 10)
		aj, _ := big.NewInt(0).SetString(distribution[j].AmountStaked, 10)
		return ai.Cmp(aj) > 0
	})

	// Estimated Annual Staking Reward: sum of all potential rewards from validators
	// This matches Glacier's calculation from their SQL query:
	// COALESCE((SELECT SUM(potential_reward) FROM uptime_stats_fuji), 0)

	stakingRatio := big.NewFloat(0).SetInt(totalStaked)
	supplyFloat := big.NewFloat(0).SetUint64(supply)
	if supply > 0 {
		stakingRatio.Quo(stakingRatio, supplyFloat)
	}

	stats := &NetworkStats{
		DelegatorDetails: DelegatorDetails{
			DelegatorCount:    totalDelCount,
			TotalAmountStaked: totalDelStake.String(),
		},
		ValidatorDetails: ValidatorDetails{
			ValidatorCount:               valCount,
			TotalAmountStaked:            totalValStake.String(),
			EstimatedAnnualStakingReward: totalPotentialReward.String(),
			StakingDistributionByVersion: distribution,
			StakingRatio:                 fmt.Sprintf("%.20f", stakingRatio),
		},
	}

	m.stats.Store(stats)
	return nil
}
