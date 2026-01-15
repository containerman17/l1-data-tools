package subnets

import (
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
)

func (s *Subnets) TestCases() []indexer.TestCase {
	return []indexer.TestCase{
		{
			Name:       "List subnets - 1 subnet",
			Path:       "/v1/networks/testnet/subnets",
			Params:     map[string]string{"pageSize": "1"},
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name:       "List subnets - 100 subnets",
			Path:       "/v1/networks/testnet/subnets",
			Params:     map[string]string{"pageSize": "100"},
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name: "Subnet details: Primary",
			Path: "/v1/networks/mainnet/subnets/11111111111111111111111111111111LpoYY",
		},
		{
			Name: "Subnet details: Fuji",
			Path: "/v1/networks/fuji/subnets/VAtJ5S7UKxgW1VAAN3FDGMx8SbRyhdBy4MSD5HxtW7nu8Ke61",
		},
		{
			Name:       "Subnet details: Beam (⚠️ Glacier has wrong ownerAddresses)",
			Path:       "/v1/networks/fuji/subnets/ie1wUBR2bQDPkGCRf2CBVzmP55eSiyJsFYqeGXnTYt2r33aKW",
			SkipFields: []string{"ownerAddresses", "threshold"}, // Glacier shows stale data, node confirms our values are correct
		},
		{
			Name: "Subnet details: Expired",
			Path: "/v1/networks/fuji/subnets/2jJ5VQnDJfCgQHP3bKu4ZCGZo2gPNJKUhbDb3NTaF4SKcQitWz",
		},
	}
}
