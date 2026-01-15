package pending_rewards

import (
	"time"

	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
)

// TestCases returns self-tests for this indexer.
func (p *PendingRewards) TestCases() []indexer.TestCase {
	addresses := []string{
		"fuji1udpqdsrf5hydtl96d7qexgvkvc8tgn0d3fgrym",
		"fuji1ur873jhz9qnaqv5qthk5sn3e8nj3e0kmafyxut",
		"fuji1deswvjzkwst3uehwf2cpvxf6g8vsrvyk76a43g",
		"fuji16cvmzlnfysmu204kfp23994skw9ykre88eu7fk",
		"fuji1p3u4k99q3qyml7nez59rqtu49777agdgkq7azh",
		"fuji1e3agq9d8m6887nvvegz8qvvyklnvlphx0kgfgd",
		"fuji16ac3zfvuav40t5xxyluh3jsh8rgdf2zjrsg7um",
	}

	testCases := []indexer.TestCase{}

	for _, addr := range addresses {
		testCases = append(testCases, indexer.TestCase{
			Name:       "pending-by-address-" + addr,
			Path:       "/v1/networks/testnet/rewards:listPending",
			Params:     map[string]string{"addresses": addr, "pageSize": "100"},
			SkipFields: []string{"nextPageToken"},
			ApproxFields: map[string]float64{
				"estimatedReward.amount": 0.001,
				"progress":               0.01,
			},
			FilterGlacier: filterExpiredPendingRewards,
		})
	}

	testCases = append(testCases, []indexer.TestCase{
		{
			Name:       "pending-by-nodeId",
			Path:       "/v1/networks/testnet/rewards:listPending",
			Params:     map[string]string{"nodeIds": "NodeID-7AVJUG2aBK7avJevwggjcmtDTte42yXuK", "pageSize": "100"},
			SkipFields: []string{"nextPageToken"},
			ApproxFields: map[string]float64{
				"estimatedReward.amount": 0.001,
				"progress":               0.01,
			},
			FilterGlacier: filterExpiredPendingRewards,
		},
		{
			Name:      "pending-cache-hit",
			Path:      "/v1/networks/testnet/rewards:listPending",
			Params:    map[string]string{"addresses": "fuji1udpqdsrf5hydtl96d7qexgvkvc8tgn0d3fgrym", "pageSize": "10", "pageToken": "MTA="},
			LocalOnly: true,
			MaxTimeMs: 10,
		},
	}...)

	return testCases
}

// this is a bug of glacier api, not a feature of this indexer
func filterExpiredPendingRewards(resp map[string]any) map[string]any {
	rewards, ok := resp["pendingRewards"].([]any)
	if !ok {
		return resp
	}

	now := time.Now().Unix()
	filtered := make([]any, 0, len(rewards))

	for _, item := range rewards {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		var endTime int64
		if v, ok := m["endTimestamp"].(float64); ok {
			endTime = int64(v)
		}

		if endTime > now {
			filtered = append(filtered, item)
		}
	}

	result := make(map[string]any)
	for k, v := range resp {
		result[k] = v
	}
	result["pendingRewards"] = filtered
	return result
}
