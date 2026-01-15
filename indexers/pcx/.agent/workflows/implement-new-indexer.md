---
description: Implement a new indexer feature from a requirements MD file
---

This workflow guides you through implementing a new API/Indexer feature based on a provided Markdown specification (e.g., `notes/23_asset_by_id.md`).

## 1. Analyze Requirements
Read the provided MD file to understand:
- **API Endpoints**: What is the URL path? (e.g., `/v1/networks/{network}/blockchains/{blockchain}/assets/{assetID}`)
- **Data Source**: Which chain(s) does the data come from? (P, X, or C)
- **Output Schema**: What does the JSON response look like?
- **Glacier Alignment**: Are the field names and types exactly matching the provided `curl` examples?

## 2. Create Indexer Package
Create a new directory (e.g., `indexers/assets/`).

### A. Define Storage (`store.go`)
Define the data model and Pebble storage logic:
- `AssetMetadata` (or similar) struct with `json` tags matching the requirements.
- Unique DB prefix (e.g., `const prefixAsset = "asset:"`).
- Helper functions: `saveAsset(batch, meta)` and `getAsset(id)`.

### B. Define Indexer & API (`assets.go` & `api.go`)
- **Indexer Struct**: Keep track of the Pebble DB and `networkID`.
- **Initialization**: Open a Pebble DB in a dedicated subdirectory (e.g., `dataDir + "/assets"`).
- **Hardcoded Data**: Seed any mandatory data (like AVAX metadata) during `Init`.
- **Route Registration**: Implement `RegisterRoutes(mux *http.ServeMux)` using `mux.HandleFunc`.

## 3. Implement Chain Indexing
Implement ONLY the interfaces required by the data source.

### X-Chain Indexing (`x_indexing.go`)
If indexing X-Chain (BaseTx, CreateAssetTx, etc.):
- Implement `ProcessXChainPreCortinaTxs` and `ProcessXChainBlocks`.
- **Crucial**: For pre-Cortina transactions, use `github.com/.../xchain/pre_cortina_timestamps` to fix bogus Index API timestamps.
- Use `db.GetWatermark` / `db.SaveWatermark` for persistence.

### P-Chain Indexing (`p_indexing.go`)
If indexing P-Chain (Staking, Rewards, etc.):
- Implement `ProcessPChainBatch`.
- Use `db.GetWatermark` / `db.SaveWatermark`.

## 4. Define Self-Tests (`selftest.go`)
Implement the `Testable` interface:
- Use the `curl` examples from the requirement MD to create `indexer.TestCase` objects.
- Ensure `Path` and `Params` match the expected usage.
- Tests will automatically compare Local vs Glacier unless `LocalOnly: true` is set.

## 5. Registration
You **must** register the indexer in two places.

### A. Production Server (`cmd/server/main.go`)
1. Import the package.
2. Initialize: `assetsIndexer := assets.New()`.
3. Add to the relevant chain slices (e.g., `xIndexers`).
4. Add to the `uniqueIndexers` list for route registration.

### B. Test Runner (`cmd/test/main.go`)
Add the indexer to the `indexers` map. **Set the `needs` flags accurately** to save time during sync:
```go
"assets": {
    indexer:  assets.New(),
    testable: assets.New(),
    needsX:   true, // Only sync X-chain!
},
```

## 6. Verification
Run the self-test.

### 🚀 Fast Iteration (API/Test changes only)
If you only changed the API logic or test case definitions:
```bash
go run ./cmd/test/ <indexer_name>
```
*Tip: This takes ~1 second if the index is already built.*

### 🛠️ Re-indexing (Indexing logic changes)
If you changed how data is written to the DB:
```bash
go run ./cmd/test/ <indexer_name> --fresh
```
*Warning: Re-indexing can take ~200 seconds.*

### 💡 Debugging
If tests fail, check the `/tmp/dev/diffs/` directory for detailed YAML and diff comparisons between Local and Glacier results.