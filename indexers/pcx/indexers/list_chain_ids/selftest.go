package list_chain_ids

import (
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
)

func (c *Chains) FilterGlacierResponse(path string, response []byte) []byte {
	return response
}

func (c *Chains) TestCases() []indexer.TestCase {
	addresses := []string{
		"fuji1udpqdsrf5hydtl96d7qexgvkvc8tgn0d3fgrym",
		"fuji1ur873jhz9qnaqv5qthk5sn3e8nj3e0kmafyxut",
		"fuji1deswvjzkwst3uehwf2cpvxf6g8vsrvyk76a43g",
		"fuji16cvmzlnfysmu204kfp23994skw9ykre88eu7fk",
		"fuji1p3u4k99q3qyml7nez59rqtu49777agdgkq7azh",
		"fuji1e3agq9d8m6887nvvegz8qvvyklnvlphx0kgfgd",
		"fuji16ac3zfvuav40t5xxyluh3jsh8rgdf2zjrsg7um",
		"fuji1jun9yd92nm4nwlsfkq87pn7gxvel9mqqx28qwp",
		"fuji1rjjy7tlry4yh26cymcx2zuu9zlg7js36vv259l",
		"fuji166qqctxjwcm39jk8dmwqsx8zvhnry3wpp8dqqm",
	}

	testCases := make([]indexer.TestCase, 0, len(addresses))
	for _, addr := range addresses {
		testCases = append(testCases, indexer.TestCase{
			Name:   "Chain interactions for " + addr,
			Path:   "/v1/networks/testnet/addresses:listChainIds",
			Params: map[string]string{"addresses": addr},
		})
	}

	// Also add a multi-address test case
	testCases = append(testCases, indexer.TestCase{
		Name:   "Multi-address interaction test",
		Path:   "/v1/networks/testnet/addresses:listChainIds",
		Params: map[string]string{"addresses": addresses[0] + "," + addresses[1]},
	})

	return testCases
}
