# Split Files Refactor

## Current: `client.go` (406 lines)
Too large. Split into logical units.

## Target Structure

| File | Contents |
|------|----------|
| `client.go` | `Client`, `Block`, `NewClient`, `Stream`, `Close`, `connect` |
| `buffer.go` | `receiveBuffer`, `bufferedItem`, `BufferConfig`, `DefaultBufferConfig` |
| `catalogue.go` | `ChainInfo`, `NewFromCatalogue` |
| `options.go` | `Option`, `WithReconnect`, `WithBufferConfig` |

## Steps
1. Create `buffer.go` - move buffer types and methods
2. Create `catalogue.go` - move `ChainInfo`, `NewFromCatalogue`
3. Create `options.go` - move `Option` type and option funcs
4. Keep `client.go` lean - just core `Client` logic
5. `go build ./...` to verify
