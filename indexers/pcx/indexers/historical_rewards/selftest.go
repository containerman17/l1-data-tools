package historical_rewards

import "github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"

// TestCases returns self-tests for this indexer.
func (h *HistoricalRewards) TestCases() []indexer.TestCase {
	addresses := []string{
		"fuji1jun9yd92nm4nwlsfkq87pn7gxvel9mqqx28qwp",
		"fuji1rjjy7tlry4yh26cymcx2zuu9zlg7js36vv259l",
		"fuji166qqctxjwcm39jk8dmwqsx8zvhnry3wpp8dqqm",
		"fuji1udpqdsrf5hydtl96d7qexgvkvc8tgn0d3fgrym",
		"fuji1ur873jhz9qnaqv5qthk5sn3e8nj3e0kmafyxut",
		"fuji1deswvjzkwst3uehwf2cpvxf6g8vsrvyk76a43g",
		"fuji16cvmzlnfysmu204kfp23994skw9ykre88eu7fk",
		"fuji1p3u4k99q3qyml7nez59rqtu49777agdgkq7azh",
		"fuji1e3agq9d8m6887nvvegz8qvvyklnvlphx0kgfgd",
		"fuji16ac3zfvuav40t5xxyluh3jsh8rgdf2zjrsg7um",
	}

	testCases := []indexer.TestCase{}

	for _, address := range addresses {
		testCases = append(testCases, indexer.TestCase{
			Name: "historical-by-address-" + address,
			Path: "/v1/networks/testnet/rewards",
			Params: map[string]string{
				"addresses": address,
				"pageSize":  "10",
			},
			SkipFields: []string{"nextPageToken"}, // Format differs from Glacier
			ApproxFields: map[string]float64{
				"startTimestamp": 0.0001,
				"endTimestamp":   0.0001,
			},
		})
	}

	return testCases
}
