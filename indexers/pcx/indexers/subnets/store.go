package subnets

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/cockroachdb/pebble/v2"
)

type SubnetMetadata struct {
	SubnetID                    string                     `json:"subnetId"`
	CreateBlockTimestamp        int64                      `json:"createBlockTimestamp"`
	CreateBlockIndex            string                     `json:"createBlockIndex"`
	IsL1                        bool                       `json:"isL1"`
	Locktime                    uint64                     `json:"locktime"`
	Threshold                   uint32                     `json:"threshold"`
	OwnerAddresses              []string                   `json:"ownerAddresses"`
	SubnetOwnershipInfo         *OwnershipInfo             `json:"subnetOwnershipInfo"`
	Blockchains                 []*SubnetBlockchain        `json:"blockchains"`
	L1ConversionTransactionHash string                     `json:"l1ConversionTransactionHash,omitempty"`
	L1ValidatorManagerDetails   *L1ValidatorManagerDetails `json:"l1ValidatorManagerDetails,omitempty"`
}

type L1ValidatorManagerDetails struct {
	BlockchainID    string `json:"blockchainId"`
	ContractAddress string `json:"contractAddress"`
}

type OwnershipInfo struct {
	Threshold uint32   `json:"threshold"`
	Addresses []string `json:"addresses"`
	Locktime  uint64   `json:"locktime"`
}

type SubnetBlockchain struct {
	BlockchainID         string `json:"blockchainId"`
	BlockchainName       string `json:"blockchainName"`
	CreateBlockNumber    string `json:"createBlockNumber"`
	CreateBlockTimestamp int64  `json:"createBlockTimestamp"`
	EVMChainID           *int   `json:"evmChainId,omitempty"`
	SubnetID             string `json:"subnetId"`
	VMID                 string `json:"vmId"`
}

func (s *Subnets) saveSubnet(batch *pebble.Batch, meta *SubnetMetadata, height uint64) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	// Save by ID
	idKey := []byte(fmt.Sprintf("sh:%s", meta.SubnetID))
	if err := batch.Set(idKey, data, pebble.Sync); err != nil {
		return err
	}

	// Save for listing (index by height/id to maintain sort)
	// ONLY if height is provided (not an update)
	if height != 0 || meta.SubnetID == "11111111111111111111111111111111LpoYY" {
		listKey := []byte(fmt.Sprintf("sl:%020d:%s", height, meta.SubnetID))
		if err := batch.Set(listKey, []byte(meta.SubnetID), pebble.Sync); err != nil {
			return err
		}
	}

	return nil
}

func (s *Subnets) saveSubnetBlockchain(batch *pebble.Batch, meta *SubnetBlockchain) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	key := []byte(fmt.Sprintf("sb:%s:%s", meta.SubnetID, meta.BlockchainID))
	return batch.Set(key, data, pebble.Sync)
}

func (s *Subnets) getSubnet(subnetID string, includeBlockchains bool) (*SubnetMetadata, error) {
	return s.getSubnetFromDB(subnetID, includeBlockchains, nil)
}

func (s *Subnets) getSubnetFromBatch(batch *pebble.Batch, subnetID string) (*SubnetMetadata, error) {
	return s.getSubnetFromDB(subnetID, false, batch)
}

func (s *Subnets) getSubnetFromDB(subnetID string, includeBlockchains bool, batch *pebble.Batch) (*SubnetMetadata, error) {
	key := []byte(fmt.Sprintf("sh:%s", subnetID))

	var data []byte
	var err error

	// Try to read from batch first if provided
	if batch != nil {
		data, _, err = batch.Get(key)
	}

	// Fallback to DB if not in batch or no batch provided
	if err != nil || batch == nil {
		var closer io.Closer
		data, closer, err = s.db.Get(key)
		if err != nil {
			return nil, err
		}
		defer closer.Close()
	}

	var meta SubnetMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}

	if includeBlockchains {
		blockchains, err := s.listSubnetBlockchains(subnetID)
		if err == nil {
			meta.Blockchains = blockchains
		}
	}

	// Ensure blockchains is never nil
	if meta.Blockchains == nil {
		meta.Blockchains = []*SubnetBlockchain{}
	}

	return &meta, nil
}

func (s *Subnets) listSubnetBlockchains(subnetID string) ([]*SubnetBlockchain, error) {
	var results []*SubnetBlockchain
	prefix := []byte(fmt.Sprintf("sb:%s:", subnetID))
	upper := []byte(fmt.Sprintf("sb:%s;", subnetID))
	iter, _ := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: upper,
	})
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var sbc SubnetBlockchain
		if err := json.Unmarshal(iter.Value(), &sbc); err == nil {
			results = append(results, &sbc)
		}
	}
	if results == nil {
		results = []*SubnetBlockchain{}
	}
	return results, nil
}

func (s *Subnets) listSubnets(pageSize int) ([]*SubnetMetadata, error) {
	if pageSize <= 0 {
		pageSize = 10
	}

	var result []*SubnetMetadata
	iter, _ := s.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte("sl:"),
		UpperBound: []byte("sl;"),
	})
	defer iter.Close()

	count := 0
	for iter.Last(); iter.Valid() && count < pageSize; iter.Prev() {
		subnetID := string(iter.Value())
		meta, err := s.getSubnet(subnetID, true)
		if err != nil {
			continue
		}
		result = append(result, meta)
		count++
	}

	return result, nil
}
