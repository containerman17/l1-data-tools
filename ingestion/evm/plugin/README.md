# Indexing Subnet-EVM Plugin

A subnet-evm VM wrapper that indexes blocks with traces and receipts in real-time.

## Quick Start

```bash
cd ingestion/evm/plugin/scripts
./plugin-dev.sh
```

## Configuration

| Environment Variable | Description |
|---------------------|-------------|
| `GRPC_INDEXER_CHAIN_ID` | Required. Chain ID to index |

## Endpoints

- `GET /info` → `{"chainID": "...", "latestBlock": 12345}`
- `GET /ws?from=100` → WebSocket block stream

## Output Format

```json
{
  "block": { /* eth_getBlockByNumber */ },
  "receipts": [ /* eth_getTransactionReceipt */ ],
  "traces": [ /* debug_traceBlockByNumber */ ]
}
```

## Storage

Data stored in: `~/.avalanchego/chainData/{chainID}/indexer/`

To reset indexer (if needed): `rm -rf ~/.avalanchego/chainData/{chainID}/indexer/`
