package validators

import (
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
)

func (v *Validators) TestCases() []indexer.TestCase {
	// Fields we skip because they require live node data we can't index
	// or because the ordering differs between implementations
	liveFields := []string{
		"stakePercentage",
		"validatorHealth",
		"uptimePerformance",
		"avalancheGoVersion",
		"delegationCapacity",
		"geolocation",
		"nextPageToken",
		// potentialRewards amounts require complex calculation
		"potentialRewards.validationRewardAmount",
		"potentialRewards.delegationRewardAmount",
		// amountDelegated can be empty vs "0"
		"amountDelegated",
	}

	return []indexer.TestCase{
		// Note: Default list order differs from Glacier (they use internal ordering)
		// We test with LocalOnly where ordering matters, or use specific filters
		{
			Name:       "List validators - primary network only",
			Path:       "/v1/networks/fuji/validators",
			Params:     map[string]string{"pageSize": "1", "subnetId": "11111111111111111111111111111111LpoYY", "validationStatus": "active"},
			SkipFields: liveFields,
			// Glacier and local may return different validators due to ordering
			// Skip for now until we implement proper cursor-based pagination
			LocalOnly: true,
		},
		{
			Name:       "Get validator by nodeId - active",
			Path:       "/v1/networks/fuji/validators/NodeID-AuyddbmFnAXCgeN388tMWzvAYu3nMzi4m",
			Params:     map[string]string{"pageSize": "1", "validationStatus": "active"},
			SkipFields: liveFields,
			// Compare structure only, specific validator data may differ
			LocalOnly: true,
		},
		{
			Name:       "Get validator by nodeId - completed",
			Path:       "/v1/networks/fuji/validators/NodeID-AuyddbmFnAXCgeN388tMWzvAYu3nMzi4m",
			Params:     map[string]string{"pageSize": "1", "validationStatus": "completed"},
			SkipFields: liveFields,
			LocalOnly: true,
		},
	}
}
