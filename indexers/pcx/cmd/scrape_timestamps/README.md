# Timestamp Scraper

Scrapes pre-Cortina X-Chain timestamps from Glacier API for both vertices and transactions.

## Purpose

Builds static timestamp archives for historical X-Chain data. See `notes/20_timestamp_problem.md` for full context.

## Setup

1. Add your Glacier API key to `.env`:
   ```
   DATA_API_KEY=your_api_key_here
   ```

2. Build:
   ```bash
   go build -o /tmp/scrape_timestamps ./cmd/scrape_timestamps
   ```

## Usage

```bash
cd /home/ubuntu/devrel-experiments/03_data_api/p-chain-indexer

# Scrape testnet (Fuji)
go run ./cmd/scrape_timestamps testnet

# Scrape mainnet
go run ./cmd/scrape_timestamps mainnet
```

The scraper will:
1. Scrape **Fuji (testnet)** or **Mainnet** based on the argument
2. Save data to `xchain/pre_cortina_timestamps/`
3. Build binary archive immediately

Output files (in `xchain/pre_cortina_timestamps/`):
- `timestamps-fuji.json` - Original Fuji JSON data (kept as source of truth)
- `fuji.bin` - Optimized Fuji binary archive (for go:embed)
- `timestamps-mainnet.json` - Original Mainnet JSON data (kept as source of truth)
- `mainnet.bin` - Optimized Mainnet binary archive (for go:embed)

**Progress tracking:**
- Files are updated every 10,000 entries (vertices) or 1,000 entries (transactions)
- If the scraper crashes or is interrupted, it will automatically resume from the last saved `nextPageToken`.
- The JSON file stores the current `stage` ("vertices" or "transactions") and progress tokens.
- The final save marks data as `"sorted": true` and `"inProgress": false`.

## Features

- **Rate limiting**: Automatically retries on 409/429 errors with 10-second backoff
- **Progress logging**: Shows page numbers and counts as it scrapes
- **Automatic Resume**: Picks up exactly where it left off using saved page tokens
- **Incremental saves**: Saves progress to disk so you never lose more than 10k/1k entries
- **Sorted output**: Data sorted by timestamp for optimal compression
- **Fail-fast**: Stops on Fuji errors before attempting Mainnet

## Binary Archive Format

The `.bin` files use a simple, stable binary format (no gob, no external dependencies):

```
[MAGIC: 4 bytes "XCTS"]
[VERSION: 4 bytes, currently 1]
[VERTEX_COUNT: 4 bytes]
[TX_COUNT: 4 bytes]
[VERTICES: (32-byte hash + 8-byte timestamp) × vertex_count, sorted by hash]
[TXS: (32-byte hash + 8-byte timestamp) × tx_count, sorted by hash]
```

**Lookup**: Binary search by hash → O(log n), ~22 comparisons for 4M entries.

**Size**: ~40 bytes per entry. Fuji: ~18 MB, Mainnet: ~164 MB (vertices only).

This format is stable across Go versions and trivial to parse in any language.

## If Interrupted

If the scraper crashes or is killed (e.g., rate limits, network issues, Ctrl+C):

1. **Check progress**:
   ```bash
   # See current stage and counts
   jq '{network, stage, vertexCount: (.vertices | length), txCount: (.transactions | length), inProgress}' timestamps-fuji.json
   ```

2. **Resume**: Just run the scraper again. It will detect the `"inProgress": true` flag and pick up from the last page token.
   - It will log: `fuji: Resuming from stage vertices (vertices: 22300, txs: 0)`

3. **Verification**: When a network is finished, it automatically builds the `.bin` archive. If you delete only the `.bin` file but keep the `.json`, running the scraper again will detect the finished JSON and rebuild the missing binary archive immediately.

**Note**: Glacier API limits pageSize to max 100 entries per request.
