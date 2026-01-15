# Network Details

Implement the high-level network health and staking overview endpoint.

## Target Endpoint

`GET /v1/networks/{network}`

## Examples

### Mainnet
URL: `https://data-api.avax.network/v1/networks/mainnet`
Response:
```json
{
  "delegatorDetails": {
    "delegatorCount": 89842,
    "totalAmountStaked": "41134158758070865"
  },
  "validatorDetails": {
    "validatorCount": 786,
    "totalAmountStaked": "168434941809049709",
    "estimatedAnnualStakingReward": "5774303751391910",
    "stakingDistributionByVersion": [
      {
        "version": "offline",
        "amountStaked": "645025783205143",
        "validatorCount": 56
      },
      {
        "version": "avalanchego/1.14.0",
        "amountStaked": "208924074783915431",
        "validatorCount": 730
      }
    ],
    "stakingRatio": "0.44961181662476747832"
  }
}
```

### Fuji Testnet
URL: `https://data-api.avax.network/v1/networks/fuji`
Response:
```json
{
  "delegatorDetails": {
    "delegatorCount": 163,
    "totalAmountStaked": "700091846777"
  },
  "validatorDetails": {
    "validatorCount": 282,
    "totalAmountStaked": "40001324840512390",
    "estimatedAnnualStakingReward": "4193552464960016",
    "stakingDistributionByVersion": [
      {
        "version": "avalanchego/1.14.0",
        "amountStaked": "40000622179358794",
        "validatorCount": 74
      },
      {
        "version": "offline",
        "amountStaked": "1402753000373",
        "validatorCount": 208
      }
    ],
    "stakingRatio": "0.10421001058485749995"
  }
}
```

## Requirements
- Derive counts and stake amounts from the current validator set.
- `stakingRatio` needs total supply (can be hardcoded or fetched).
- Node versions/distributions might require `platform.getCurrentValidators` or similar if not available in Indexed data.

## Implementation Strategy: Proactive Background Worker

To ensure sub-millisecond API responses and minimize Node RPC load, implement a dedicated background service (e.g., `NetworkMonitor`) that runs a continuous loop:

### The Loop Logic
- **Success Case**: If a refresh cycle succeeds, sleep for **30 seconds**.
- **Failure Case**: If any RPC fails (e.g., node is busy), retry after **5 seconds**.
- **Execution**: The service should run independently of the Block Fetcher but share the same `pchain.Client`.

### Storage & Serving
- **Primary Service**: The results should be stored in an `atomic.Pointer[NetworkStats]` for instant memory access.
- **Persistence**: On every successful update, persist the aggregated JSON to Pebble DB (key: `meta:network_stats`). This allows the API to serve data immediately after a server restart before the first loop cycle completes.

### Data Aggregation Workflow
1.  **Fetch Validators**: `platform.getCurrentValidators`
    - Sum `weight` for Total Validator Stake.
    - Sum `delegatorWeight` for Total Delegator Stake.
    - Group by `nodeID`.
2.  **Fetch Peers**: `info.peers`
    - Map `nodeID` to version.
3.  **Fetch Supply**: `platform.getCurrentSupply`
    - Use as denominator for `stakingRatio`.
4.  **Process Offline Nodes**:
    - If a validator has `connected: false`, move its stake into the `"offline"` version bucket.
5.  **Calculate Distribution**:
    - Cross-reference `validators` with `peers`.
    - Create a list of objects containing `version`, `amountStaked`, and `validatorCount`.
