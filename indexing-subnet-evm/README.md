# Indexing Subnet-EVM Plugin

Drop-in replacement for subnet-evm that indexes blocks with receipts and traces locally. Exposes a WebSocket firehose on port 9090.

## Docker

```bash
docker run \
  -e GRPC_INDEXER_CHAIN_ID=2pGcTLh9Z5YCjmU871k2Z5waC8cCXYJnafYz1h7RJjz7u9Nmg2 \
  -e VM_ID=YourVmId \
  containerman17/indexing-subnet-evm:v0.8.0_v1.14.1 \
  --network-id=fuji --track-subnets=YourSubnetId
```

## Environment

| Variable | Default | Description |
|----------|---------|-------------|
| `GRPC_INDEXER_CHAIN_ID` | **required** | Chain ID to run (32-byte Avalanche chain ID). Other chains fail to initialize. `GRPC_` prefix required for avalanchego to pass it to the plugin. |
| `VM_ID` | - | VM ID to register the plugin as (entrypoint copies binary from canonical ID) |

## API

Firehose server listens on port 9090.

### `GET /info`

Returns chain identity and sync status.

```json
{"chainID":"2pGcTLh9Z5YCjmU871k2Z5waC8cCXYJnafYz1h7RJjz7u9Nmg2","latestBlock":12345678}
```

Use this to discover which chain is on which port when running multiple L1s.

### `GET /ws?from=N`

WebSocket endpoint. Streams blocks starting from height `N` (default: 1).

**Protocol:**
- Binary frames only
- Each frame is zstd-compressed JSONL (1-100 blocks per frame)
- Blocks are sent in order, no gaps
- At tip: server polls and sends new blocks as they arrive

**Frame format after decompression:**
```
{"number":123,"hash":"0x...","receipts":[...],"traces":[...]}
{"number":124,"hash":"0x...","receipts":[...],"traces":[...]}
```

**Client pseudocode:**
```python
ws = connect("ws://localhost:9090/ws?from=1")
decoder = zstd.decompressor()
while frame := ws.recv():
    jsonl = decoder.decompress(frame)
    for line in jsonl.split('\n'):
        block = json.loads(line)
        process(block)
```

## How it works

1. Wraps subnet-evm, intercepts `Accept()` calls
2. Fetches block + receipts + traces via direct blockchain access (no RPC)
3. Stores JSON in the node's local database under `chainData/{chainID}/indexer/`
4. Compacts old blocks into zstd-compressed batches (100 blocks each)
5. Serves data over WebSocket

Data lives with the node. If you wipe `chainData`, the index goes with it.

