# Catalogue Client

## Goal
Add a second constructor that connects to the `/chains` catalogue endpoint and creates clients for all available chains.

## API

```go
// Existing - single chain
func NewClient(addr string, opts ...Option) *Client

// New - catalogue-aware, returns map of blockchainId -> Client
func NewFromCatalogue(catalogueURL string, opts ...Option) (map[string]*Client, error)
```

## Behavior
1. `GET {catalogueURL}/chains` → parse JSON object
2. For each `blockchainId`, construct WebSocket URL from `indexer` field
3. Return `map[string]*Client` keyed by `blockchainId`

## Notes
- Share `Option` type between constructors
- Caller decides which chains to `Stream()` from the returned map
