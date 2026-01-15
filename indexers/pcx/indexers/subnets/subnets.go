package subnets

import (
	"context"
	"path/filepath"

	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/db"
	"github.com/cockroachdb/pebble/v2"
)

type Subnets struct {
	db        *pebble.DB
	networkID uint32
}

func New() *Subnets {
	return &Subnets{}
}

func (s *Subnets) Name() string {
	return "subnets"
}

func (s *Subnets) Init(ctx context.Context, baseDir string, networkID uint32) error {
	s.networkID = networkID
	dir := filepath.Join(baseDir, "subnets")
	storage, err := pebble.Open(dir, &pebble.Options{
		Logger: db.QuietLogger(),
	})
	if err != nil {
		return err
	}
	s.db = storage

	if err := s.initPrimarySubnet(); err != nil {
		return err
	}

	return nil
}

func (s *Subnets) initPrimarySubnet() error {
	// Check if already initialized
	primaryID := "11111111111111111111111111111111LpoYY"
	if _, err := s.getSubnet(primaryID, false); err == nil {
		return nil
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	meta := &SubnetMetadata{
		SubnetID:             primaryID,
		CreateBlockTimestamp: 1599696000, // Avalanche Genesis
		CreateBlockIndex:     "-1",
		IsL1:                 false,
		Locktime:             0,
		Threshold:            0,
		OwnerAddresses:       []string{""}, // Glacier shows an empty string in the list
		SubnetOwnershipInfo: &OwnershipInfo{
			Threshold: 0,
			Addresses: []string{""},
			Locktime:  0,
		},
		Blockchains: []*SubnetBlockchain{},
	}

	if err := s.saveSubnet(batch, meta, 0); err != nil {
		return err
	}

	// Primary Subnet Blockchains
	pChainID := "11111111111111111111111111111111LpoYY"
	xChainID := "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM" // Mainnet
	cChainID := "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5"  // Mainnet
	cCID := 43114
	if s.networkID == 5 {
		xChainID = "2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm"
		cChainID = "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp"
		cCID = 43113
	}

	blockchains := []*SubnetBlockchain{
		{
			BlockchainID:         pChainID,
			BlockchainName:       "P-Chain",
			CreateBlockNumber:    "-1",
			CreateBlockTimestamp: 1599696000,
			SubnetID:             primaryID,
			VMID:                 "platformvm",
		},
		{
			BlockchainID:         xChainID,
			BlockchainName:       "X-Chain",
			CreateBlockNumber:    "-1",
			CreateBlockTimestamp: 1599696000,
			SubnetID:             primaryID,
			VMID:                 "jvYyfQTxGMJLuGWa55kdP2p2zSUYsQ5Raupu4TW34ZAUBAbtq",
		},
		{
			BlockchainID:         cChainID,
			BlockchainName:       "C-Chain",
			CreateBlockNumber:    "-1",
			CreateBlockTimestamp: 1599696000,
			SubnetID:             primaryID,
			VMID:                 "mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6",
			EVMChainID:           &cCID,
		},
	}

	for _, bc := range blockchains {
		if err := s.saveSubnetBlockchain(batch, bc); err != nil {
			return err
		}
	}

	return batch.Commit(pebble.Sync)
}

func (s *Subnets) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Subnets) GetPChainWatermark() (uint64, error) {
	return db.GetWatermark(s.db, "p-blocks")
}
