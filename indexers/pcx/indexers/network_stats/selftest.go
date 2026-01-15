package network_stats

import (
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
)

func (m *NetworkMonitor) TestCases() []indexer.TestCase {
	return []indexer.TestCase{
		{
			Name: "Network Stats parity check",
			Path: "/v1/networks/testnet",
			// These values change frequently, so we use reasonable tolerances.
			// Counts can differ by 1-2 due to node gossip propagation delays.
			// Stake amounts should be very close (0.1% tolerance).
			ApproxFields: map[string]float64{
				"delegatorDetails.delegatorCount":               0.02,   // 2% (1-2 delegators difference is normal)
				"delegatorDetails.totalAmountStaked":            0.01,   // 1%
				"validatorDetails.validatorCount":               0.002,  // 2%
				"validatorDetails.totalAmountStaked":            0.001,  // 1%
				"validatorDetails.stakingRatio":                 0.001,  // 1%
				"validatorDetails.estimatedAnnualStakingReward": 0.0001, // 20% - different estimation methods
			},
			// Staking distribution is highly volatile and the array order/versions
			// can differ between Glacier's infra and our local node.
			// We skip it for exact match but the test still ensures the rest works.
			SkipFields: []string{
				"validatorDetails.stakingDistributionByVersion",
			},
		},
	}
}
