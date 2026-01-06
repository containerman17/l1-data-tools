# l1-data-tools

EVM blockchain data tools for Avalanche L1s.

## Packages

| Package | Description |
|---------|-------------|
| [evm-ingestion](./evm-ingestion/) | High-throughput block ingestion server with WebSocket streaming |
| [evm-ingestion-client](./evm-ingestion-client/) | Go client library for streaming blocks |
| [indexing-subnet-evm](./indexing-subnet-evm/) | Subnet-EVM VM plugin with built-in indexing |

## Build

```bash
# Server
go build -o evm-ingestion ./evm-ingestion

# Client example
go build -o example-client ./evm-ingestion-client/example

# VM plugin
go build -buildmode=plugin -o indexing-subnet-evm.so ./indexing-subnet-evm
```
