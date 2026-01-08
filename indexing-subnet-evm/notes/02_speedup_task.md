# Task: Eliminate Sync Overhead via versiondb Integration

## Problem

Current indexer uses separate pebble database with `pebble.Sync` on every write. This is:
1. **Slow** - fsync on every block
2. **Gap-prone** - two independent WALs can flush at different times on crash
3. **No rollback** - if Accept() fails after indexBlock(), orphan data persists

## Solution

Use the chain's versiondb (via reflection) for atomic commits. Writes go to `versiondb.mem` and commit atomically with `lastAcceptedID` via `versiondb.Commit()`.

## Architecture: Storage Interface

Create a `Storage` interface that both implementations satisfy:

```go
// storage/interface.go
package storage

type Storage interface {
    // Block operations
    SaveBlock(blockNum uint64, data []byte) error
    GetBlock(blockNum uint64) ([]byte, error)
    FirstBlock() (uint64, bool)
    LatestBlock() (uint64, bool)
    DeleteBlockRange(start, end uint64) error
    
    // Batch operations (compactor)
    SaveBatch(start, end uint64, data []byte) error
    GetBatchCompressed(start uint64) ([]byte, error)
    
    // Meta operations
    GetMeta() uint64
    SaveMeta(lastCompacted uint64) error
    
    // Lifecycle
    Close() error
}
```

Two implementations:
1. **`PebbleStorage`** - current implementation, for standalone ingestion tool
2. **`VersionDBStorage`** - wraps `database.Database`, for plugin (atomic with chain)

## Implementation Steps

### 1. Extract Storage Interface

Create `storage/interface.go` with the interface above.

### 2. Rename Current Implementation

```go
// storage/pebble.go
type PebbleStorage struct {
    db *pebble.DB
}

func NewPebbleStorage(path string) (*PebbleStorage, error) { ... }

// Implements Storage interface (no changes to method signatures)
```

### 3. Create VersionDB Implementation

**CRITICAL:** `database.Iterator` has no `Last()` method - must cache latest block.

```go
// storage/versiondb.go
package storage

import (
    "sync/atomic"
    "github.com/ava-labs/avalanchego/database"
)

type VersionDBStorage struct {
    db           database.Database
    latestBlock  atomic.Uint64  // Cached for O(1) reads
}

func NewVersionDBStorage(db database.Database) *VersionDBStorage {
    s := &VersionDBStorage{db: db}
    // Initialize cache by scanning once at startup
    s.initLatestBlockCache()
    return s
}

func (s *VersionDBStorage) initLatestBlockCache() {
    iter := s.db.NewIteratorWithPrefix([]byte("block:"))
    defer iter.Release()
    
    var latest uint64
    for iter.Next() {
        if num, ok := parseBlockKey(iter.Key()); ok {
            latest = num
        }
    }
    s.latestBlock.Store(latest)
}

func (s *VersionDBStorage) SaveBlock(blockNum uint64, data []byte) error {
    if err := s.db.Put(blockKey(blockNum), data); err != nil {
        return err
    }
    // Update cache atomically
    for {
        current := s.latestBlock.Load()
        if blockNum <= current {
            break
        }
        if s.latestBlock.CompareAndSwap(current, blockNum) {
            break
        }
    }
    return nil
}

func (s *VersionDBStorage) GetBlock(blockNum uint64) ([]byte, error) {
    return s.db.Get(blockKey(blockNum))
}

func (s *VersionDBStorage) FirstBlock() (uint64, bool) {
    iter := s.db.NewIteratorWithPrefix([]byte("block:"))
    defer iter.Release()
    
    if !iter.Next() {
        return 0, false
    }
    return parseBlockKey(iter.Key())
}

func (s *VersionDBStorage) LatestBlock() (uint64, bool) {
    latest := s.latestBlock.Load()
    return latest, latest > 0
}

// NOTE: O(n) delete, but compaction is background task - acceptable
func (s *VersionDBStorage) DeleteBlockRange(start, end uint64) error {
    batch := s.db.NewBatch()
    for i := start; i <= end; i++ {
        if err := batch.Delete(blockKey(i)); err != nil {
            return err
        }
    }
    return batch.Write()
}

func (s *VersionDBStorage) SaveBatch(start, end uint64, data []byte) error {
    return s.db.Put(batchKey(start, end), data)
}

func (s *VersionDBStorage) GetBatchCompressed(start uint64) ([]byte, error) {
    end := start + BatchSize - 1
    return s.db.Get(batchKey(start, end))
}

func (s *VersionDBStorage) GetMeta() uint64 {
    data, err := s.db.Get([]byte(metaKey))
    if err != nil || len(data) < 8 {
        return 0
    }
    return binary.BigEndian.Uint64(data)
}

func (s *VersionDBStorage) SaveMeta(lastCompacted uint64) error {
    data := make([]byte, 8)
    binary.BigEndian.PutUint64(data, lastCompacted)
    return s.db.Put([]byte(metaKey), data)
}

func (s *VersionDBStorage) Close() error {
    return nil  // Don't own the database
}
```

### 4. Update Compactor to Use Interface

```go
// storage/compactor.go
type Compactor struct {
    store  Storage  // Changed from *Storage
    logger CompactorLogger
    ...
}

func NewCompactorWithLogger(store Storage, logger CompactorLogger) *Compactor {
    ...
}
```

### 5. Update Server to Use Interface

```go
// api/server.go
type Server struct {
    store Storage  // Changed from *storage.Storage
    ...
}

func NewServer(store Storage, chainID string) *Server {
    ...
}
```

### 6. Add versiondb Reflection to Plugin

```go
// indexing-subnet-evm/vm.go

import (
    "github.com/ava-labs/avalanchego/database/versiondb"
    "github.com/ava-labs/avalanchego/database/prefixdb"
)

func (vm *IndexingVM) getVersionDB() (*versiondb.Database, error) {
    vmVal := reflect.ValueOf(vm.VM).Elem()
    vdbField := vmVal.FieldByName("versiondb")
    if !vdbField.IsValid() {
        return nil, fmt.Errorf("versiondb field not found")
    }
    
    vdbPtr := reflect.NewAt(
        vdbField.Type(),
        unsafe.Pointer(vdbField.UnsafeAddr()),
    ).Elem()
    
    vdb, ok := vdbPtr.Interface().(*versiondb.Database)
    if !ok {
        return nil, fmt.Errorf("versiondb has unexpected type")
    }
    return vdb, nil
}
```

### 7. Update Plugin Initialize()

```go
// In Initialize(), AFTER vm.VM.Initialize():

// Get versiondb via reflection
vdb, err := vm.getVersionDB()
if err != nil {
    return fmt.Errorf("failed to access versiondb: %w", err)
}

// Create prefixdb on versiondb for indexer data
indexerDB := prefixdb.New([]byte("grpc_indexer"), vdb)

// Create storage using versiondb backend
vm.store = storage.NewVersionDBStorage(indexerDB)

// Compactor and server use same interface
vm.compactor = storage.NewCompactorWithLogger(vm.store, compactorLogger)
vm.server = api.NewServer(vm.store, chainCtx.ChainID.String())
```

### 8. Remove Separate Pebble

Delete the separate pebble initialization - start fresh with versiondb.

## Performance Notes

| Operation | PebbleStorage | VersionDBStorage | Note |
|-----------|---------------|------------------|------|
| SaveBlock | 5ms (fsync) | 0.001ms (memory) | **5000x faster** |
| LatestBlock | O(1) iter.Last() | O(1) cached | Both fast |
| FirstBlock | O(1) iter.First() | O(1) iter.Next() | Both fast |
| DeleteBlockRange | O(1) DeleteRange | O(n) batch delete | Acceptable (background) |
| Startup | instant | O(n) cache init | One-time scan |

## Benefits

| Aspect | Before (PebbleStorage) | After (VersionDBStorage) |
|--------|------------------------|--------------------------|
| Pebble instances | 2 | 1 |
| Sync per block | Yes (fsync) | No (memory write) |
| Gap on crash | Possible | Impossible (atomic) |
| Orphan on error | Possible | Impossible (rollback) |

## Testing

1. **Interface test**: Verify both implementations satisfy Storage interface
2. **Gap test**: Kill process during Accept(), verify no gap on restart
3. **Rollback test**: Inject error in Accept() after indexBlock(), verify no orphan data
4. **Compactor test**: Verify compaction works with VersionDBStorage
5. **Firehose test**: Verify server can stream blocks with VersionDBStorage
6. **LatestBlock cache test**: Verify cache stays in sync after many writes/restarts

## Files to Modify

- `evm-ingestion/storage/interface.go` - NEW: Storage interface
- `evm-ingestion/storage/pebble.go` - Rename struct to PebbleStorage
- `evm-ingestion/storage/versiondb.go` - NEW: VersionDBStorage implementation
- `evm-ingestion/storage/compactor.go` - Use Storage interface
- `evm-ingestion/api/server.go` - Use Storage interface
- `indexing-subnet-evm/vm.go` - Add getVersionDB(), use VersionDBStorage
