package utxos

import (
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
)

// TestCases returns self-tests for this indexer.
func (u *UTXOs) TestCases() []indexer.TestCase {
	smallAddr := "fuji1udpqdsrf5hydtl96d7qexgvkvc8tgn0d3fgrym"
	firstCortinaBlockAddress := "fuji1ur873jhz9qnaqv5qthk5sn3e8nj3e0kmafyxut"
	mediumAddr := "fuji1deswvjzkwst3uehwf2cpvxf6g8vsrvyk76a43g"

	return []indexer.TestCase{
		{
			Name: "p-chain utxos small with includeSpent",
			Path: "/v1/networks/testnet/blockchains/p-chain/utxos",
			Params: map[string]string{
				"addresses":    smallAddr,
				"pageSize":     "50",
				"includeSpent": "true",
				"sortBy":       "timestamp",
			},
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name: "p-chain utxos small without includeSpent",
			Path: "/v1/networks/testnet/blockchains/p-chain/utxos",
			Params: map[string]string{
				"addresses": smallAddr,
				"pageSize":  "50",
				"sortBy":    "timestamp",
			},
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name: "p-chain utxos using p-chain ID",
			Path: "/v1/networks/testnet/blockchains/11111111111111111111111111111111LpoYY/utxos",
			Params: map[string]string{
				"addresses": smallAddr,
				"pageSize":  "50",
				"sortBy":    "timestamp",
			},
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name: "p-chain utxos sort by amount",
			Path: "/v1/networks/testnet/blockchains/p-chain/utxos",
			Params: map[string]string{
				"addresses": smallAddr,
				"pageSize":  "50",
				"sortBy":    "amount",
			},
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name: "p-chain utxos sort by amount asc",
			Path: "/v1/networks/testnet/blockchains/p-chain/utxos",
			Params: map[string]string{
				"addresses": smallAddr,
				"pageSize":  "50",
				"sortBy":    "amount",
				"sortOrder": "asc",
			},
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name: "p-chain utxos medium address",
			Path: "/v1/networks/testnet/blockchains/p-chain/utxos",
			Params: map[string]string{
				"addresses": mediumAddr,
				"pageSize":  "50",
				"sortBy":    "amount",
				"sortOrder": "asc",
			},
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name: "p-chain utxos active staker",
			Path: "/v1/networks/testnet/blockchains/p-chain/utxos",
			Params: map[string]string{
				"addresses": "fuji16cvmzlnfysmu204kfp23994skw9ykre88eu7fk",
				"pageSize":  "100",
			},
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name: "p-chain utxos active delegator",
			Path: "/v1/networks/testnet/blockchains/p-chain/utxos",
			Params: map[string]string{
				"addresses": "fuji1p3u4k99q3qyml7nez59rqtu49777agdgkq7azh",
				"pageSize":  "100",
			},
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name: "c-chain utxos small address",
			Path: "/v1/networks/testnet/blockchains/c-chain/utxos",
			Params: map[string]string{
				"addresses":    mediumAddr,
				"pageSize":     "50",
				"includeSpent": "true",
			},
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name: "x-chain utxos pre and post-cortina",
			Path: "/v1/networks/testnet/blockchains/x-chain/utxos",
			Params: map[string]string{
				"addresses":    firstCortinaBlockAddress,
				"pageSize":     "10",
				"includeSpent": "false",
			},
			SkipFields: []string{"nextPageToken", "utxoBytes"}, // utxoBytes: Glacier rebuilds simplified FT format, we serialize actual output type

		},
		{
			Name: "x-chain utxos single utxo",
			Path: "/v1/networks/testnet/blockchains/x-chain/utxos",
			Params: map[string]string{
				"addresses":    "fuji1e3agq9d8m6887nvvegz8qvvyklnvlphx0kgfgd",
				"pageSize":     "10",
				"includeSpent": "false",
			},
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name: "x-chain utxos many",
			Path: "/v1/networks/testnet/blockchains/x-chain/utxos",
			Params: map[string]string{
				"addresses":    "fuji16ac3zfvuav40t5xxyluh3jsh8rgdf2zjrsg7um",
				"pageSize":     "100",
				"includeSpent": "true",
			},
			SkipFields: []string{"nextPageToken"},
			Only:       false,
		},
		{
			Name: "c-chain utxos medium address with spent",
			Path: "/v1/networks/testnet/blockchains/c-chain/utxos",
			Params: map[string]string{
				"addresses":    mediumAddr,
				"pageSize":     "100",
				"includeSpent": "true",
			},
			SkipFields: []string{"nextPageToken"},
			Only:       false,
		},
		{
			Name: "c-chain utxos medium address without spent",
			Path: "/v1/networks/testnet/blockchains/c-chain/utxos",
			Params: map[string]string{
				"addresses": mediumAddr,
				"pageSize":  "100",
			},
			SkipFields: []string{"nextPageToken"},
			Only:       false,
		},
		//TODO: an address that done all 3 chains swaps

		// ============ Balance Tests ============
		// P-Chain Balances
		{
			Name: "p-chain balances small address",
			Path: "/v1/networks/testnet/blockchains/p-chain/balances",
			Params: map[string]string{
				"addresses": smallAddr,
			},
			FilterGlacier: filterZeroBalances,
		},
		{
			Name: "p-chain balances medium address",
			Path: "/v1/networks/testnet/blockchains/p-chain/balances",
			Params: map[string]string{
				"addresses": mediumAddr,
			},
			FilterGlacier: filterZeroBalances,
		},
		{
			Name: "p-chain balances active staker",
			Path: "/v1/networks/testnet/blockchains/p-chain/balances",
			Params: map[string]string{
				"addresses": "fuji16cvmzlnfysmu204kfp23994skw9ykre88eu7fk",
			},
			FilterGlacier: filterZeroBalances,
		},
		{
			Name: "p-chain balances active delegator",
			Path: "/v1/networks/testnet/blockchains/p-chain/balances",
			Params: map[string]string{
				"addresses": "fuji1p3u4k99q3qyml7nez59rqtu49777agdgkq7azh",
			},
			FilterGlacier: filterZeroBalances,
		},

		// X-Chain Balances
		{
			Name: "x-chain balances pre-post cortina",
			Path: "/v1/networks/testnet/blockchains/x-chain/balances",
			Params: map[string]string{
				"addresses": firstCortinaBlockAddress,
			},
			FilterGlacier: filterZeroBalances,
		},
		{
			Name: "x-chain balances single utxo",
			Path: "/v1/networks/testnet/blockchains/x-chain/balances",
			Params: map[string]string{
				"addresses": "fuji1e3agq9d8m6887nvvegz8qvvyklnvlphx0kgfgd",
			},
			FilterGlacier: filterZeroBalances,
		},
		{
			Name: "x-chain balances many utxos",
			Path: "/v1/networks/testnet/blockchains/x-chain/balances",
			Params: map[string]string{
				"addresses": "fuji16ac3zfvuav40t5xxyluh3jsh8rgdf2zjrsg7um",
			},
			FilterGlacier: filterZeroBalances,
		},

		// C-Chain Balances (atomic memory only)
		{
			Name: "c-chain balances medium address",
			Path: "/v1/networks/testnet/blockchains/c-chain/balances",
			Params: map[string]string{
				"addresses": mediumAddr,
			},
			FilterGlacier: filterZeroBalances,
		},

		// ============ blockTimestamp Balance Tests ============
		{
			Name: "balances small address before creation",
			Path: "/v1/networks/testnet/blockchains/p-chain/balances",
			Params: map[string]string{
				"addresses":      smallAddr,
				"blockTimestamp": "1765343146",
			},
			FilterGlacier: filterZeroBalances,
		},
		{
			Name: "balances small address at creation",
			Path: "/v1/networks/testnet/blockchains/p-chain/balances",
			Params: map[string]string{
				"addresses":      smallAddr,
				"blockTimestamp": "1765343147",
			},
			FilterGlacier: filterZeroBalances,
		},
		{
			Name: "balances small address before consumption",
			Path: "/v1/networks/testnet/blockchains/p-chain/balances",
			Params: map[string]string{
				"addresses":      smallAddr,
				"blockTimestamp": "1765343208",
			},
			FilterGlacier: filterZeroBalances,
		},
		{
			Name: "balances small address at consumption",
			Path: "/v1/networks/testnet/blockchains/p-chain/balances",
			Params: map[string]string{
				"addresses":      smallAddr,
				"blockTimestamp": "1765343209",
			},
			FilterGlacier: filterZeroBalances,
		},
		{
			Name: "balances small address after consumption",
			Path: "/v1/networks/testnet/blockchains/p-chain/balances",
			Params: map[string]string{
				"addresses":      smallAddr,
				"blockTimestamp": "1765343210",
			},
			FilterGlacier: filterZeroBalances,
		},
		{
			Name: "balances active staker before staking",
			Path: "/v1/networks/testnet/blockchains/p-chain/balances",
			Params: map[string]string{
				"addresses":      "fuji16cvmzlnfysmu204kfp23994skw9ykre88eu7fk",
				"blockTimestamp": "1750839102",
			},
			FilterGlacier: filterZeroBalances,
		},
		{
			Name: "balances active staker during staking",
			Path: "/v1/networks/testnet/blockchains/p-chain/balances",
			Params: map[string]string{
				"addresses":      "fuji16cvmzlnfysmu204kfp23994skw9ykre88eu7fk",
				"blockTimestamp": "1750839103",
			},
			FilterGlacier: filterZeroBalances,
		},
		{
			Name: "balances active staker after staking",
			Path: "/v1/networks/testnet/blockchains/p-chain/balances",
			Params: map[string]string{
				"addresses":      "fuji16cvmzlnfysmu204kfp23994skw9ykre88eu7fk",
				"blockTimestamp": "1750925740",
			},
			FilterGlacier: filterZeroBalances,
		},

		// ============ UTXO Filter Tests ============
		{
			Name: "p-chain utxos minUtxoAmount",
			Path: "/v1/networks/testnet/blockchains/p-chain/utxos",
			Params: map[string]string{
				"addresses":     mediumAddr,
				"minUtxoAmount": "1000000000", // 1 AVAX
			},
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name: "x-chain utxos assetId filter",
			Path: "/v1/networks/testnet/blockchains/x-chain/utxos",
			Params: map[string]string{
				"addresses": "fuji16ac3zfvuav40t5xxyluh3jsh8rgdf2zjrsg7um",
				"assetId":   "U8iRqJoiJm8xZHAacmvYyZVwqQx6uDNtQeP3CQ6fcgQk3JqnK", // AVAX
			},
			SkipFields: []string{"nextPageToken"},
		},
		{
			Name: "x-chain utxos minUtxoAmount",
			Path: "/v1/networks/testnet/blockchains/x-chain/utxos",
			Params: map[string]string{
				"addresses":     "fuji16ac3zfvuav40t5xxyluh3jsh8rgdf2zjrsg7um",
				"minUtxoAmount": "10000000000", // 10 AVAX
			},
			SkipFields: []string{"nextPageToken"},
		},
	}
}

// filterZeroBalances removes any balance entries with amount "0" from the Glacier/Local response.
// This is needed because Glacier is inconsistent: it sometimes includes zeros on X/C-Chain
// (due to a bug in their mappers) but omits them on P-Chain. We choose a "clean" API that
// always omits zeros, so we filter the comparison to ensure tests pass.
func filterZeroBalances(resp map[string]any) map[string]any {
	balances, ok := resp["balances"].(map[string]any)
	if !ok {
		return resp
	}

	for category, val := range balances {
		slice, ok := val.([]any)
		if !ok {
			continue
		}

		newSlice := make([]any, 0)
		for _, item := range slice {
			itemMap, ok := item.(map[string]any)
			if !ok {
				newSlice = append(newSlice, item)
				continue
			}

			amount, ok := itemMap["amount"].(string)
			if ok && amount == "0" {
				continue
			}
			newSlice = append(newSlice, item)
		}
		balances[category] = newSlice
	}

	return resp
}
