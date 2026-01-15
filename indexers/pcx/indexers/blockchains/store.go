package blockchains

import (
	"encoding/json"

	"github.com/cockroachdb/pebble/v2"
)

const (
	prefixBlockchain = "bc:" // blockchainId -> JSON (BlockchainMetadata)
	prefixList       = "bl:" // bl:{uint64 blockNumber}:{blockchainId} -> nil
)

type BlockchainMetadata struct {
	CreateBlockTimestamp int64  `json:"createBlockTimestamp"`
	CreateBlockNumber    string `json:"createBlockNumber"`
	BlockchainID         string `json:"blockchainId"`
	VMID                 string `json:"vmId"`
	SubnetID             string `json:"subnetId"`
	BlockchainName       string `json:"blockchainName"`
	EVMChainID           *int   `json:"evmChainId,omitempty"`
	GenesisData          any    `json:"genesisData,omitempty"`
}

func (b *Blockchains) saveBlockchain(batch *pebble.Batch, meta *BlockchainMetadata, blockNumber uint64) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if err := batch.Set([]byte(prefixBlockchain+meta.BlockchainID), data, pebble.NoSync); err != nil {
		return err
	}

	// Secondary index: bl:{blockNumber}:{blockchainId}
	listKey := make([]byte, len(prefixList)+8+len(meta.BlockchainID))
	copy(listKey, prefixList)
	// Use BigEndian so that it's sortable. We'll iterate in reverse.
	uint64ToBytes(blockNumber, listKey[len(prefixList):len(prefixList)+8])
	copy(listKey[len(prefixList)+8:], meta.BlockchainID)

	return batch.Set(listKey, nil, pebble.NoSync)
}

func uint64ToBytes(v uint64, b []byte) {
	b[0] = byte(v >> 56)
	b[1] = byte(v >> 48)
	b[2] = byte(v >> 40)
	b[3] = byte(v >> 32)
	b[4] = byte(v >> 24)
	b[5] = byte(v >> 16)
	b[6] = byte(v >> 8)
	b[7] = byte(v)
}

func bytesToUint64(b []byte) uint64 {
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
}

func (b *Blockchains) listBlockchains(pageSize int, pageToken string) ([]*BlockchainMetadata, string, error) {
	if pageSize <= 0 {
		pageSize = 10
	}

	lower := []byte(prefixList)
	upper := []byte("bl;")

	iter, _ := b.db.NewIter(&pebble.IterOptions{
		LowerBound: lower,
		UpperBound: upper,
	})
	defer iter.Close()

	var results []*BlockchainMetadata
	count := 0

	// Move to start position (descending order)
	valid := iter.Last()
	if pageToken != "" {
		// pageToken should be the full key to start from (non-inclusive)
		// but Glacier uses a UUID. We might need a proper token encoding.
		// For now, let's just support the basic list.
	}

	for ; valid && count < pageSize; valid = iter.Prev() {
		key := iter.Key()
		blockchainID := string(key[len(prefixList)+8:])

		meta, err := b.getBlockchain(blockchainID)
		if err != nil {
			continue
		}
		results = append(results, meta)
		count++
	}

	// NEXT TOKEN logic (too complex for now, will return empty for now)
	return results, "", nil
}

func (b *Blockchains) getBlockchain(blockchainID string) (*BlockchainMetadata, error) {
	data, closer, err := b.db.Get([]byte(prefixBlockchain + blockchainID))
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	var meta BlockchainMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}
