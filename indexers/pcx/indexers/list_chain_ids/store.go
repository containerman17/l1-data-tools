package list_chain_ids

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/cockroachdb/pebble/v2"
)

const (
	prefixAddrChains = "ac:" // ac:{address}:{chainID}
)

// markTouched marks that an address has interacted with a specific blockchain.
func markTouched(batch *pebble.Batch, address string, blockchainID string) {
	key := []byte(fmt.Sprintf("%s%s:%s", prefixAddrChains, address, blockchainID))
	batch.Set(key, nil, nil)
}

// getChainIDs returns the list of blockchain IDs touched by an address.
func (c *Chains) getChainIDs(address string) ([]string, error) {
	prefix := []byte(fmt.Sprintf("%s%s:", prefixAddrChains, address))
	iter, err := c.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(append([]byte(nil), prefix...), 0xff),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var blockchainIDs []string
	for iter.First(); iter.Valid(); iter.Next() {
		key := string(iter.Key())
		parts := strings.Split(key, ":")
		if len(parts) >= 3 {
			blockchainIDs = append(blockchainIDs, parts[2])
		}
	}
	return blockchainIDs, nil
}

func (c *Chains) GetPChainWatermark() (uint64, error) {
	return c.getWatermark([]byte("p:watermark"))
}

func (c *Chains) GetXChainBlockWatermark() (uint64, error) {
	return c.getWatermark([]byte("x:block:watermark"))
}

func (c *Chains) GetXChainPreCortinaWatermark() (uint64, error) {
	return c.getWatermark([]byte("x:preCortina:watermark"))
}

func (c *Chains) GetCChainWatermark() (uint64, error) {
	return c.getWatermark([]byte("c:watermark"))
}

func (c *Chains) getWatermark(key []byte) (uint64, error) {
	val, closer, err := c.db.Get(key)
	if err == pebble.ErrNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer closer.Close()
	return binary.BigEndian.Uint64(val), nil
}
