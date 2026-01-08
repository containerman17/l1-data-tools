package storage

import (
	"encoding/binary"
	"sync/atomic"

	"github.com/ava-labs/avalanchego/database"
)

// VersionDBStorage implements Storage using avalanchego's database.Database interface.
// Used by the plugin to share the chain's versiondb - writes go to versiondb.mem
// and commit atomically with chain metadata via versiondb.Commit().
type VersionDBStorage struct {
	db          database.Database
	latestBlock atomic.Uint64 // Cached - database.Iterator has no Last()
}

// Ensure VersionDBStorage implements Storage interface
var _ Storage = (*VersionDBStorage)(nil)

func NewVersionDBStorage(db database.Database) *VersionDBStorage {
	s := &VersionDBStorage{db: db}
	s.initLatestBlockCache()
	return s
}

// initLatestBlockCache scans all blocks once at startup to find the highest.
// database.Iterator has no Last() method, only Next().
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
	// Update cache atomically (CAS loop for concurrent safety)
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

// DeleteBlockRange deletes all blocks from start to end (inclusive).
// O(n) delete because database.Database has no DeleteRange - acceptable for background compaction.
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

func (s *VersionDBStorage) FirstBatch() (uint64, bool) {
	iter := s.db.NewIteratorWithPrefix([]byte("batch:"))
	defer iter.Release()

	if !iter.Next() {
		return 0, false
	}
	start, _, ok := parseBatchKey(iter.Key())
	return start, ok
}

func (s *VersionDBStorage) LatestBatch() (uint64, bool) {
	iter := s.db.NewIteratorWithPrefix([]byte("batch:"))
	defer iter.Release()

	// Scan to find the last one (no Last() method)
	var lastStart, lastEnd uint64
	var found bool
	for iter.Next() {
		if start, end, ok := parseBatchKey(iter.Key()); ok {
			lastStart = start
			lastEnd = end
			found = true
		}
	}
	if !found {
		return 0, false
	}
	// Return end block of last batch (consistent with PebbleStorage)
	_ = lastStart
	return lastEnd, true
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

func (s *VersionDBStorage) BlockCount() int {
	first, hasFirst := s.FirstBlock()
	if !hasFirst {
		return 0
	}
	last, hasLast := s.LatestBlock()
	if !hasLast {
		return 0
	}
	return int(last - first + 1)
}

func (s *VersionDBStorage) Close() error {
	// Don't close - we don't own the database
	return nil
}
