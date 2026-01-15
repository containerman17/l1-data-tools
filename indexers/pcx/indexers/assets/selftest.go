package assets

import (
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
)

func (a *Assets) TestCases() []indexer.TestCase {
	return []indexer.TestCase{
		{
			Name: "Avalanche (AVAX) asset",
			Path: "/v1/networks/fuji/blockchains/x-chain/assets/U8iRqJoiJm8xZHAacmvYyZVwqQx6uDNtQeP3CQ6fcgQk3JqnK",
		},
		{
			Name: "Schmeckles (SMK) asset",
			Path: "/v1/networks/fuji/blockchains/x-chain/assets/tWt78T4XYdCSfqXoyhf9WGgbjf9i4GzqTwB9stje2bd6G5kSC",
		},
		{
			Name: "Jovica Coin (JOV) asset",
			Path: "/v1/networks/fuji/blockchains/x-chain/assets/2g68XohNi7CnsGypGK2AWbMx4iz94BSkwKZ1UXReESnXC5WMkA",
		},
	}
}
