# List Blockchains

Implement the endpoint to list all blockchains in a network.

## Target Endpoint

`GET /v1/networks/{network}/blockchains`

## Examples

### Mainnet
URL: `https://data-api.avax.network/v1/networks/mainnet/blockchains`
Response:
```json
{
  "blockchains": [
    {
      "createBlockTimestamp": 1766080370,
      "createBlockNumber": "23797323",
      "blockchainId": "BxUT6DyUMnJiD3Bxnhe7dAEe2bLaeL6eCHsjAiP5wua9eNzyb",
      "vmId": "cpEmfPHfq9jwt92JW7sZMgaz9EL5fg1nJRwUKhYiUeE33CqEW",
      "subnetId": "wLUs2Y29LqC3vGWrjHaATw2fw5XxSjE63hxPeG7oJ6VcXSgTn",
      "blockchainName": "QRTMS",
      "evmChainId": 199664
    },
    {
      "createBlockTimestamp": 1765811639,
      "createBlockNumber": "23736359",
      "blockchainId": "Z2cso3c3ZLAkrR8ASTKG2vVY47FcmqaWZaKxbX9grF6MHZVga",
      "vmId": "rWhpuQPF1kb72esV2momhMuTYGkEb1oL29pt2EBXWmSy4kxnT",
      "subnetId": "2spSQe7rNy8kYexeWesJEGRYtrKVUTTrkKaSKpda3HTJSkGyn3",
      "blockchainName": "testing"
    }
  ],
  "nextPageToken": "bb29d07a-2bcf-4cb4-a795-5fc6d25655d9"
}
```

## Requirements
- List all blockchains registered on the P-Chain.
- Support pagination.
- Include `createBlockTimestamp`, `createBlockNumber`, `blockchainId`, `vmId`, `subnetId`, `blockchainName`.
- If it's an EVM chain, include `evmChainId`.
- Primary Network blockchains (X, P, C) should likely be included or prioritized if they fit this format.
- Glacier usually filters this by the network specified in the path.
