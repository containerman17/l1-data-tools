package blockchains

import (
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
)

func (b *Blockchains) TestCases() []indexer.TestCase {
	return []indexer.TestCase{
		{
			Name:       "List blockchains",
			Path:       "/v1/networks/testnet/blockchains",
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name:       "List blockchains page size 100",
			Path:       "/v1/networks/testnet/blockchains",
			Params:     map[string]string{"pageSize": "100"},
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name:       "List blockchains page sort order asc",
			Path:       "/v1/networks/testnet/blockchains",
			Params:     map[string]string{"pageSize": "100", "sortOrder": "asc"},
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name:       "List blockchains page sort order desc",
			Path:       "/v1/networks/testnet/blockchains",
			Params:     map[string]string{"pageSize": "100", "sortOrder": "desc"},
			SkipFields: []string{"nextPageToken"},
		},
		// Get blockchain by ID tests
		{
			Name: "Get P-Chain details",
			Path: "/v1/networks/mainnet/blockchains/11111111111111111111111111111111LpoYY",
		},
		{
			Name: "Get X-Chain details (Fuji)",
			Path: "/v1/networks/fuji/blockchains/2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm",
		},
		{
			Name: "Get C-Chain details (Fuji)",
			Path: "/v1/networks/fuji/blockchains/yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp",
		},
		{
			Name: "Get subnet blockchain - CoinCreate Chain",
			Path: "/v1/networks/fuji/blockchains/2WDXuhDwcqJA6NBdgGV5HArRuVjG8TYrmZ29dNWkdQkj3i4BsX",
		},
		{
			Name: "Get subnet blockchain - USDC Testing Chain",
			Path: "/v1/networks/fuji/blockchains/2vSGvmBo8ye1HgBhxFj6fQozPsuVVLcNrupjVetan9MNYzaZjH",
		},
		{
			Name: "Get subnet blockchain - Deadpan Carrot Chain",
			Path: "/v1/networks/fuji/blockchains/aBEBo34XZ8ZGwJpfCSmfoxgKYQsJpjqWRcqinbErTHYB2SE3H",
		},
	}
}
