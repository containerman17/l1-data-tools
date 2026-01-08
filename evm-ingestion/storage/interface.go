package storage

// Storage defines the interface for block storage operations
// Two implementations: PebbleStorage (standalone) and VersionDBStorage (plugin)
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
	FirstBatch() (uint64, bool)
	LatestBatch() (uint64, bool)

	// Meta operations
	GetMeta() uint64
	SaveMeta(lastCompacted uint64) error

	// Stats
	BlockCount() int

	// Lifecycle
	Close() error
}
