# Strategy and Plan: Extra Filters for UTXOs

To achieve parity with Glacier API for UTXO listing, we need to implement `assetId` and `minUtxoAmount` filters, and ensure `includeSpent`, `sortBy`, and `sortOrder` work correctly.

## 1. Research Findings

### Glacier API Behavior
- **P-Chain**: `assetId` is ignored (UTXOs are always AVAX). `minUtxoAmount` is supported.
- **X-Chain**: `assetId` is supported (filters by specific asset). `minUtxoAmount` is supported.
- **C-Chain**: Atomic memory UTXOs (mostly AVAX).
- **Sorting**: Supports `timestamp` and `amount` as `sortBy` options.
- **tie-breaker**: Glacier uses `utxoId` as a tie-breaker in the same direction as the primary sort.

### Current Implementation
- `handleUTXOs` in `indexers/utxos/api.go` already parses `includeSpent`, `sortBy`, and `sortOrder`.
- `sortBy` currently supports `timestamp` and `amount`.
- Tie-breaker logic in `sortResults` already uses `utxoId`.

## 2. Implementation Plan

### Parameter Parsing
- Update `handleUTXOs` (api.go) to parse `assetId` and `minUtxoAmount`.
- `minUtxoAmount` should default to 0 and be parsed as `uint64`.

### Filtering Logic
- Update `getUTXOsForAddresses` (api.go) to accept new filter parameters.
- Apply `minUtxoAmount` filter: `amount >= minUtxoAmount`.
- Apply `assetId` filter for X-Chain: `assetId == "" || stored.AssetID == assetId`.

## 3. Verification Strategy
- Add specific test cases to `indexers/utxos/selftest.go` for:
    - P-Chain with `minUtxoAmount`.
    - X-Chain with `assetId`.
    - X-Chain with `minUtxoAmount` and `assetId`.
- Compare local results with Glacier results using the self-test tool.

---

Implement each param from utxos list method. I guess minUtxoAmount, what else? Is everything else covered?

"/v1/networks/{network}/blockchains/{blockchainId}/utxos": {
                "get": {
                    "operationId": "getUtxosByAddresses",
                    "x-speakeasy-group": "data.primaryNetwork.utxos",
                    "x-speakeasy-name-override": "listByAddresses",
                    "x-speakeasy-pagination": {
                        "type": "cursor",
                        "inputs": [
                            {
                                "name": "pageToken",
                                "in": "parameters",
                                "type": "cursor"
                            }
                        ],
                        "outputs": {
                            "nextCursor": "$.nextPageToken"
                        }
                    },
                    "x-execution-weight": "xl",
                    "summary": "List UTXOs",
                    "description": "Lists UTXOs on one of the Primary Network chains for the supplied addresses.",
                    "parameters": [
                        {
                            "name": "addresses",
                            "required": false,
                            "in": "query",
                            "description": "A comma separated list of X-Chain or P-Chain wallet addresses, starting with \"avax\"/\"fuji\", \"P-avax\"/\"P-fuji\" or \"X-avax\"/\"X-fuji\".",
                            "example": "avax1h2ccj9f5ay5acl6tyn9mwmw32p8wref8vl8ctg",
                            "schema": {
                                "type": "string"
                            }
                        },
                        {
                            "name": "pageToken",
                            "required": false,
                            "in": "query",
                            "description": "A page token, received from a previous list call. Provide this to retrieve the subsequent page.",
                            "schema": {
                                "type": "string"
                            }
                        },
                        {
                            "name": "pageSize",
                            "required": false,
                            "in": "query",
                            "description": "The maximum number of items to return. The minimum page size is 1. The maximum pageSize is 100.",
                            "schema": {
                                "type": "integer",
                                "default": 10,
                                "minimum": 1,
                                "maximum": 100
                            },
                            "example": "10"
                        },
                        {
                            "name": "blockchainId",
                            "required": true,
                            "in": "path",
                            "description": "A primary network blockchain id or alias.",
                            "example": "p-chain",
                            "schema": {
                                "$ref": "#/components/schemas/BlockchainId"
                            }
                        },
                        {
                            "name": "network",
                            "required": true,
                            "in": "path",
                            "description": "Either mainnet or testnet/fuji.",
                            "example": "mainnet",
                            "schema": {
                                "$ref": "#/components/schemas/Network"
                            }
                        },
                        {
                            "name": "assetId",
                            "required": false,
                            "in": "query",
                            "description": "Asset ID for any asset (only applicable X-Chain)",
                            "schema": {
                                "type": "string"
                            }
                        },
                        {
                            "name": "minUtxoAmount",
                            "required": false,
                            "in": "query",
                            "description": "The minimum UTXO amount in nAVAX (inclusive), used to filter the set of UTXOs being returned. Default is 0.",
                            "example": 1000,
                            "schema": {
                                "type": "number",
                                "minimum": 0
                            }
                        },
                        {
                            "name": "includeSpent",
                            "required": false,
                            "in": "query",
                            "description": "Boolean filter to include spent UTXOs.",
                            "schema": {
                                "type": "boolean"
                            }
                        },
                        {
                            "name": "sortBy",
                            "required": false,
                            "in": "query",
                            "description": "Which property to sort by, in conjunction with sortOrder.",
                            "schema": {
                                "$ref": "#/components/schemas/UtxosSortByOption"
                            }
                        },
                        {
                            "name": "sortOrder",
                            "required": false,
                            "in": "query",
                            "example": "asc",
                            "description": "The order by which to sort results. Use \"asc\" for ascending order, \"desc\" for descending order. Sorted by timestamp or the `sortBy` query parameter, if provided.",
                            "schema": {
                                "$ref": "#/components/schemas/SortOrder"
                            }
                        }
                    ],
                    "responses": {
                        "200": {
                            "description": "Successful response",
                            "content": {
                                "application/json": {
                                    "schema": {
                                        "oneOf": [
                                            {
                                                "$ref": "#/components/schemas/ListPChainUtxosResponse"
                                            },
                                            {
                                                "$ref": "#/components/schemas/ListUtxosResponse"
                                            }
                                        ]
                                    }
                                }
                            }
                        },