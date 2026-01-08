package storage

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"github.com/cockroachdb/pebble/v2"
	"github.com/containerman17/l1-data-tools/evm-ingestion/consts"
)

// BatchSize is blocks per compressed batch
const BatchSize = consts.StorageBatchSize

const (
	blockKeyFormat = "block:%020d"       // block:{blockNum}
	batchKeyFormat = "batch:%020d-%020d" // batch:{start}-{end}
	metaKey        = "meta"
)

// PebbleStorage implements Storage using a standalone pebble database
type PebbleStorage struct {
	db *pebble.DB
}

// Ensure PebbleStorage implements Storage interface
var _ Storage = (*PebbleStorage)(nil)

func NewPebbleStorage(path string) (*PebbleStorage, error) {
	db, err := pebble.Open(path, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to open pebble db: %w", err)
	}
	return &PebbleStorage{db: db}, nil
}

func (s *PebbleStorage) Close() error {
	return s.db.Close()
}

func blockKey(blockNum uint64) []byte {
	return []byte(fmt.Sprintf(blockKeyFormat, blockNum))
}

func batchKey(start, end uint64) []byte {
	return []byte(fmt.Sprintf(batchKeyFormat, start, end))
}

func parseBlockKey(key []byte) (blockNum uint64, ok bool) {
	var num uint64
	_, err := fmt.Sscanf(string(key), blockKeyFormat, &num)
	return num, err == nil
}

func parseBatchKey(key []byte) (start, end uint64, ok bool) {
	parts := strings.Split(string(key), ":")
	if len(parts) != 2 || parts[0] != "batch" {
		return 0, 0, false
	}
	rangeParts := strings.Split(parts[1], "-")
	if len(rangeParts) != 2 {
		return 0, 0, false
	}
	var err error
	start, err = strconv.ParseUint(rangeParts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	end, err = strconv.ParseUint(rangeParts[1], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return start, end, true
}

// SaveBlock stores a block's JSON data
func (s *PebbleStorage) SaveBlock(blockNum uint64, data []byte) error {
	return s.db.Set(blockKey(blockNum), data, pebble.Sync)
}

// GetBlock retrieves a single block's data
func (s *PebbleStorage) GetBlock(blockNum uint64) ([]byte, error) {
	data, closer, err := s.db.Get(blockKey(blockNum))
	if err != nil {
		return nil, err
	}
	result := make([]byte, len(data))
	copy(result, data)
	closer.Close()
	return result, nil
}

// FirstBlock returns the lowest block number stored
func (s *PebbleStorage) FirstBlock() (uint64, bool) {
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte("block:"),
		UpperBound: []byte("block;"), // ; is after : in ASCII
	})
	if err != nil {
		return 0, false
	}
	defer iter.Close()

	if !iter.First() {
		return 0, false
	}

	blockNum, ok := parseBlockKey(iter.Key())
	return blockNum, ok
}

// LatestBlock returns the highest block number stored
func (s *PebbleStorage) LatestBlock() (uint64, bool) {
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte("block:"),
		UpperBound: []byte("block;"),
	})
	if err != nil {
		return 0, false
	}
	defer iter.Close()

	if !iter.Last() {
		return 0, false
	}

	blockNum, ok := parseBlockKey(iter.Key())
	return blockNum, ok
}

// DeleteBlockRange deletes all blocks from start to end (inclusive)
func (s *PebbleStorage) DeleteBlockRange(start, end uint64) error {
	startKey := blockKey(start)
	endKey := blockKey(end + 1)
	return s.db.DeleteRange(startKey, endKey, pebble.Sync)
}

// SaveBatch stores a compressed batch of blocks
func (s *PebbleStorage) SaveBatch(start, end uint64, data []byte) error {
	return s.db.Set(batchKey(start, end), data, pebble.Sync)
}

// GetBatchCompressed retrieves a compressed batch by its start block
func (s *PebbleStorage) GetBatchCompressed(start uint64) ([]byte, error) {
	// Calculate expected end block (batches are 100 blocks)
	end := start + BatchSize - 1
	data, closer, err := s.db.Get(batchKey(start, end))
	if err != nil {
		return nil, err
	}
	result := make([]byte, len(data))
	copy(result, data)
	closer.Close()
	return result, nil
}

// FirstBatch returns the start block of the first compressed batch
func (s *PebbleStorage) FirstBatch() (uint64, bool) {
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte("batch:"),
		UpperBound: []byte("batch;"),
	})
	if err != nil {
		return 0, false
	}
	defer iter.Close()

	if !iter.First() {
		return 0, false
	}

	start, _, ok := parseBatchKey(iter.Key())
	return start, ok
}

// LatestBatch returns the end block of the last compressed batch
func (s *PebbleStorage) LatestBatch() (uint64, bool) {
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte("batch:"),
		UpperBound: []byte("batch;"),
	})
	if err != nil {
		return 0, false
	}
	defer iter.Close()

	if !iter.Last() {
		return 0, false
	}

	_, end, ok := parseBatchKey(iter.Key())
	return end, ok
}

// GetMeta returns the last compacted block number (0 if not set)
func (s *PebbleStorage) GetMeta() uint64 {
	data, closer, err := s.db.Get([]byte(metaKey))
	if err != nil {
		return 0
	}
	defer closer.Close()
	if len(data) < 8 {
		return 0
	}
	return binary.BigEndian.Uint64(data)
}

// SaveMeta stores the last compacted block number
func (s *PebbleStorage) SaveMeta(lastCompacted uint64) error {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, lastCompacted)
	return s.db.Set([]byte(metaKey), data, pebble.Sync)
}

// BlockCount returns approximate count of individual blocks stored
func (s *PebbleStorage) BlockCount() int {
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
