# l1-data-tools

EVM blockchain data tools for Avalanche L1s.

## Packages

| Package | Description |
|---------|-------------|
| [ingestion/evm/rpc](./ingestion/evm/rpc/) | High-throughput block ingestion server with WebSocket streaming |
| [ingestion/evm/client](./ingestion/evm/client/) | Go client library for streaming blocks |
| [ingestion/evm/plugin](./ingestion/evm/plugin/) | Subnet-EVM VM plugin with built-in indexing |

## Build

```bash
# Server
go build -o ingestion/evm/rpc ./ingestion/evm/rpc

# Client example
go build -o example-client ./ingestion/evm/client/example

# VM plugin
go build -buildmode=plugin -o ingestion/evm/plugin.so ./ingestion/evm/plugin
```
