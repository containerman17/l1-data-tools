# Indexer Storage Optimization: Research Findings

## Problem Statement

Current indexer uses a separate pebble database with `pebble.Sync` on every write. On crash after indexBlock() but before Accept() completes, if WALs flush independently, we get a gap requiring full resync.

Note: "Orphan data" (indexed data when Accept() fails) is NOT a problem - on retry, same height = same key = overwrite. Snowman consensus ensures the same block wins after Accept() is called.

## Key Findings

### 1. subnet-evm Uses `Sync=false` by Default

```go
// ~/subnet-evm/plugin/evm/vm_database.go:211-212
// Default to "no sync" for pebble db
cfg.Sync = false
```

The chain doesn't fsync on writes. Our indexer with `pebble.Sync` is MORE conservative than the chain itself.

### 2. Database Architecture: Two Pebble Instances, Not Three

```go
// ~/subnet-evm/plugin/evm/vm_database.go:82-86
vm.chaindb = rawdb.NewDatabase(database.New(prefixdb.NewNested(ethDBPrefix, db)))
vm.versiondb = versiondb.New(db)
vm.acceptedBlockDB = prefixdb.New(acceptedPrefix, vm.versiondb)
vm.metadataDB = prefixdb.New(metadataPrefix, vm.versiondb)
vm.db = db
```

- **Chain's pebble:** `chaindb` and `versiondb` both wrap the same underlying `db`
- **Indexer's pebble:** Separate instance at different path with independent WAL

### 3. The `versiondb` Field is Unexported

```go
// ~/subnet-evm/plugin/evm/vm.go:205
versiondb *versiondb.Database  // lowercase = unexported
```

Cannot access directly from plugin code. Requires reflection (same pattern already used for `eth`, `config`, `stateHistory`).

### 4. blockchain.Accept() is Asynchronous

```go
// ~/subnet-evm/core/blockchain.go:1136
bc.addAcceptorQueue(block)  // Just queues, returns immediately
```

State commits happen in background goroutine. The `versiondb.Commit()` commits metadata (`lastAcceptedID`), not blockchain state.

### 5. versiondb Provides Atomic Commits

```go
// ~/avalanchego/database/versiondb/db.go
// Commit() - writes all mem entries in single batch
func (db *Database) Commit() error {
    batch := commitBatch()     // Collect all mem entries
    batch.Write()              // Write to underlying pebble atomically
    abort()                    // Clear mem
}

// Abort() - discards all uncommitted writes
func (db *Database) Abort() {
    db.mem = make(map[string]valueDelete)
}
```

### 6. Accept() Uses defer Abort() Pattern

```go
// ~/subnet-evm/plugin/evm/wrapped_block.go:74-108
func (b *wrappedBlock) Accept(context.Context) error {
    defer vm.versiondb.Abort()  // ALWAYS runs - clears mem on failure
    
    // ... do stuff ...
    
    if err := vm.blockChain.Accept(b.ethBlock); err != nil {
        return err  // Abort() clears uncommitted writes
    }
    
    return b.vm.versiondb.Commit()  // Only on success
}
```

### 7. Gap Risk with Separate Pebbles + NoSync

```
indexBlock() NoSync → indexer WAL write (might not flush)
wrappedBlock.Accept() → versiondb.Commit() (flushes to disk)
CRASH between flushes → gap possible (chain ahead of indexer)
```

Two independent pebbles have independent WAL flush timing. "Crash together" semantics don't apply.

## Solution: Write to versiondb via Reflection

By writing indexer data to `versiondb` (via prefixdb wrapper), we get:

1. **Atomic commits** - indexer data and `lastAcceptedID` commit in same batch
2. **Proper rollback** - `versiondb.Abort()` clears both on failure
3. **Zero sync overhead** - writes go to `versiondb.mem` (memory), commit once
4. **Single pebble instance** - reduces resource usage

### Implementation Pattern

```go
// vm.go - access unexported versiondb via reflection
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
        return nil, fmt.Errorf("versiondb field has unexpected type")
    }
    return vdb, nil
}

// In Initialize, AFTER vm.VM.Initialize():
vdb, err := vm.getVersionDB()
if err != nil {
    return fmt.Errorf("failed to access versiondb: %w", err)
}
vm.indexerDB = prefixdb.New([]byte("grpc_indexer"), vdb)

// In indexBlock():
if err := vm.indexerDB.Put(blockKey(height), data); err != nil {
    return err
}
// No sync needed - wrappedBlock.Accept() commits via versiondb.Commit()
```

## Comparison

| Aspect | Current (separate pebble) | versiondb solution |
|--------|---------------------------|-------------------|
| Gap on crash | Possible (separate WALs) | Impossible (atomic) |
| Orphan data on error | Possible (sync'd before Accept) | Impossible (rollback) |
| Sync overhead | High (every block) | Zero (memory writes) |
| Resource usage | 2 pebble instances | 1 pebble instance |
| Rollback semantics | None | Free with Abort() |

## Source Code References

| File | Content |
|------|---------|
| `~/subnet-evm/plugin/evm/vm_database.go:82-86` | DB initialization, versiondb wraps same db |
| `~/subnet-evm/plugin/evm/vm_database.go:211-212` | `Sync=false` default |
| `~/subnet-evm/plugin/evm/vm.go:205` | versiondb field (unexported) |
| `~/subnet-evm/plugin/evm/wrapped_block.go:74-108` | Accept() with defer Abort() |
| `~/subnet-evm/core/blockchain.go:1136` | Async acceptor queue |
| `~/avalanchego/database/versiondb/db.go:65-130` | Commit/Abort implementation |
| `~/avalanchego/database/prefixdb/db.go` | prefixdb wrapper implementation |

## Risks

1. **Reflection fragility** - if subnet-evm renames `versiondb` field, breaks at runtime. Mitigated by: field name unchanged in years, already use reflection for other fields.

2. **Memory pressure** - indexed data sits in `versiondb.mem` until Commit(). Mitigated by: one block at a time (1-10MB), plenty of memory available.
