# Blockchain Details

Get detailed information about a specific blockchain by its ID.

## Target Endpoint

`GET /v1/networks/{network}/blockchains/{blockchainId}`

## Examples

### P-Chain (Mainnet)
URL: `https://data-api.avax.network/v1/networks/mainnet/blockchains/11111111111111111111111111111111LpoYY`
Response:
```json
{
  "createBlockTimestamp": 1599696000,
  "createBlockNumber": "-1",
  "blockchainId": "11111111111111111111111111111111LpoYY",
  "vmId": "platformvm",
  "subnetId": "11111111111111111111111111111111LpoYY",
  "blockchainName": "P-Chain"
}
```

### X-Chain (Mainnet)
URL: `https://data-api.avax.network/v1/networks/mainnet/blockchains/2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM`
Response (truncated - includes very large genesis data):
```json
{
  "createBlockTimestamp": 1599696000,
  "createBlockNumber": "-1",
  "blockchainId": "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM",
  "vmId": "avm",
  "subnetId": "11111111111111111111111111111111LpoYY",
  "blockchainName": "X-Chain",
  "genesisData": {
    "encoding": "hex",
    "data": "0x000000000000000006000..."
  }
}
```

### C-Chain (Fuji)
URL: `https://data-api.avax.network/v1/networks/fuji/blockchains/yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp`
Response (truncated - includes large genesis config):
```json
{
  "createBlockTimestamp": 1599696000,
  "createBlockNumber": "-1",
  "blockchainId": "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp",
  "vmId": "mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6",
  "subnetId": "11111111111111111111111111111111LpoYY",
  "blockchainName": "C-Chain",
  "evmChainId": 43113,
  "genesisData": {
    "config": {
      "chainId": 43113,
      "homesteadBlock": 0,
      "daoForkBlock": 0,
      "eip150Block": 0,
      ...
    },
    "alloc": {
      "0100000000000000000000000000000000000000": {
        "code": "0x730000...",
        "balance": "0x0"
      },
      ...
    },
    "number": "0x0",
    "gasUsed": "0x0",
    "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000"
  }
}
```

### Subnet Blockchain (Mainnet)
URL: `https://data-api.avax.network/v1/networks/mainnet/blockchains/BxUT6DyUMnJiD3Bxnhe7dAEe2bLaeL6eCHsjAiP5wua9eNzyb`
Response (truncated):
```json
{
  "createBlockTimestamp": 1766080370,
  "createBlockNumber": "23797323",
  "blockchainId": "BxUT6DyUMnJiD3Bxnhe7dAEe2bLaeL6eCHsjAiP5wua9eNzyb",
  "vmId": "cpEmfPHfq9jwt92JW7sZMgaz9EL5fg1nJRwUKhYiUeE33CqEW",
  "subnetId": "wLUs2Y29LqC3vGWrjHaATw2fw5XxSjE63hxPeG7oJ6VcXSgTn",
  "blockchainName": "QRTMS",
  "evmChainId": 199664,
  "genesisData": {
    "config": {
      "chainId": 199664,
      ...
    },
    "alloc": {
      ...
    }
  }
}
```

## Requirements

### Core Fields (Always Present)
- `createBlockTimestamp`: Unix timestamp when the blockchain was created
- `createBlockNumber`: P-Chain block number (or "-1" for primary network chains)
- `blockchainId`: The blockchain's unique identifier
- `vmId`: Virtual machine ID (e.g., "platformvm", "avm", or a custom VM ID)
- `subnetId`: The subnet this blockchain belongs to
- `blockchainName`: Human-readable name

### Optional Fields
- `evmChainId`: Only present for EVM-compatible chains (parsed from genesis data)
- `genesisData`: Genesis configuration data (can be very large, especially for X-Chain and C-Chain)
  - For X-Chain: Hex-encoded data in `{"encoding": "hex", "data": "0x..."}`
  - For EVM chains: Full genesis JSON with config, alloc, etc.

## Implementation Notes

### Data Source
This is an extension of the `GET /v1/networks/{network}/blockchains` (list) endpoint:
- Same data indexing from `CreateChainTx` transactions
- Single blockchain lookup by ID instead of pagination

### Response Handling
- Return 404 if blockchain ID not found
- The `genesisData` field can be extremely large (X-Chain mainnet is ~24KB+)
- Genesis data is stored exactly as returned from the node's `platform.getBlockchains` RPC

### Special Cases
- **Primary Network Chains** (P, X, C): Have `createBlockNumber: "-1"` and `createBlockTimestamp: 1599696000`
- **P-Chain**: Uses special VM ID `"platformvm"` instead of a CB58-encoded ID
- **X-Chain**: Genesis data is hex-encoded bytes
- **EVM Chains**: Genesis data is a full JSON object with config and allocations

## Relationship to Other Endpoints
- Data should come from the same indexer as `GET /v1/networks/{network}/blockchains`
- This is essentially a "get by ID" version of the list endpoint
- Can reuse the same storage and parsing logic
