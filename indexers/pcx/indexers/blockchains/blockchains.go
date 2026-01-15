package blockchains

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/db"
	"github.com/cockroachdb/pebble/v2"
)

type Blockchains struct {
	db        *pebble.DB
	networkID uint32
}

func New() *Blockchains {
	return &Blockchains{}
}

func (b *Blockchains) Name() string {
	return "blockchains"
}

func (b *Blockchains) Init(ctx context.Context, baseDir string, networkID uint32) error {
	b.networkID = networkID
	dir := filepath.Join(baseDir, "blockchains")
	storage, err := pebble.Open(dir, &pebble.Options{
		Logger: db.QuietLogger(),
	})
	if err != nil {
		return err
	}
	b.db = storage

	if err := b.initPrimaryBlockchains(); err != nil {
		return err
	}

	return nil
}

func (b *Blockchains) initPrimaryBlockchains() error {
	// Check if already initialized
	pChainID := "11111111111111111111111111111111LpoYY"
	if _, err := b.getBlockchain(pChainID); err == nil {
		return nil
	}

	batch := b.db.NewBatch()
	defer batch.Close()

	// Fetch genesis data from avalanchego
	config := genesis.GetConfig(b.networkID)
	pChainGenesisBytes, _, err := genesis.FromConfig(config)
	if err != nil {
		return err
	}

	// P-Chain - no genesis data in Glacier response
	pMeta := &BlockchainMetadata{
		CreateBlockTimestamp: 1599696000,
		CreateBlockNumber:    "-1",
		BlockchainID:         pChainID,
		VMID:                 "platformvm",
		SubnetID:             "11111111111111111111111111111111LpoYY",
		BlockchainName:       "P-Chain",
		// P-Chain doesn't expose genesis data in the API
	}
	if err := b.saveBlockchain(batch, pMeta, 0); err != nil {
		return err
	}

	// X-Chain
	xID := "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM" // Mainnet
	if b.networkID == 5 {
		xID = "2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm"
	}

	// Extract X-Chain genesis from P-Chain genesis
	xChainTx, err := genesis.VMGenesis(pChainGenesisBytes, constants.AVMID)
	var xChainGenesisData any
	if err == nil {
		if createChainTx, ok := xChainTx.Unsigned.(*txs.CreateChainTx); ok {
			xChainGenesisData = parseGenesisData(createChainTx.GenesisData)
		}
	}

	xMeta := &BlockchainMetadata{
		CreateBlockTimestamp: 1599696000,
		CreateBlockNumber:    "-1",
		BlockchainID:         xID,
		VMID:                 "jvYyfQTxGMJLuGWa55kdP2p2zSUYsQ5Raupu4TW34ZAUBAbtq", // AVM
		SubnetID:             "11111111111111111111111111111111LpoYY",
		BlockchainName:       "X-Chain",
		GenesisData:          xChainGenesisData,
	}
	if err := b.saveBlockchain(batch, xMeta, 0); err != nil {
		return err
	}

	// C-Chain
	cID := "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5" // Mainnet
	cCID := 43114
	if b.networkID == 5 {
		cID = "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp"
		cCID = 43113
	}

	// Extract C-Chain genesis from P-Chain genesis
	cChainTx, err := genesis.VMGenesis(pChainGenesisBytes, constants.EVMID)
	var cChainGenesisData any
	if err == nil {
		if createChainTx, ok := cChainTx.Unsigned.(*txs.CreateChainTx); ok {
			cChainGenesisData = parseGenesisData(createChainTx.GenesisData)
		}
	}

	cMeta := &BlockchainMetadata{
		CreateBlockTimestamp: 1599696000,
		CreateBlockNumber:    "-1",
		BlockchainID:         cID,
		VMID:                 "mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6", // Coreth
		SubnetID:             "11111111111111111111111111111111LpoYY",
		BlockchainName:       "C-Chain",
		EVMChainID:           &cCID,
		GenesisData:          cChainGenesisData,
	}
	if err := b.saveBlockchain(batch, cMeta, 0); err != nil {
		return err
	}

	return batch.Commit(pebble.Sync)
}

func (b *Blockchains) Close() error {
	if b.db != nil {
		return b.db.Close()
	}
	return nil
}

func (b *Blockchains) GetPChainWatermark() (uint64, error) {
	return db.GetWatermark(b.db, "p-blocks")
}

func parseGenesisData(data []byte) any {
	if len(data) == 0 {
		return nil
	}
	// Try to unmarshal as JSON (for EVM chains)
	var obj any
	if err := json.Unmarshal(data, &obj); err == nil {
		return obj
	}
	// Not JSON, return as []byte (Go will base64 encode it in JSON)
	return data
}
