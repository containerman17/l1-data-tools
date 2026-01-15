package assets

import (
	"context"
	"path/filepath"

	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/db"
	"github.com/cockroachdb/pebble/v2"
)

type Assets struct {
	db        *pebble.DB
	networkID uint32
}

func New() *Assets {
	return &Assets{}
}

func (a *Assets) Name() string {
	return "assets"
}

func (a *Assets) Init(ctx context.Context, baseDir string, networkID uint32) error {
	a.networkID = networkID
	assetsDir := filepath.Join(baseDir, "assets")
	storage, err := pebble.Open(assetsDir, &pebble.Options{
		Logger: db.QuietLogger(),
	})
	if err != nil {
		return err
	}
	a.db = storage

	// Hardcode AVAX metadata if it doesn't exist
	batch := a.db.NewBatch()
	avax := &AssetMetadata{
		AssetID:            "U8iRqJoiJm8xZHAacmvYyZVwqQx6uDNtQeP3CQ6fcgQk3JqnK",
		Name:               "Avalanche",
		Symbol:             "AVAX",
		Denomination:       9,
		CreatedAtTimestamp: 1599696000,
		Type:               "secp256k1",
		Cap:                "fixed",
	}
	if err := a.saveAsset(batch, avax); err != nil {
		batch.Close()
		return err
	}
	if err := batch.Commit(pebble.Sync); err != nil {
		return err
	}

	return nil
}

func (a *Assets) Close() error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// Interface methods to satisfy requirements

func (a *Assets) GetXChainBlockWatermark() (uint64, error) {
	return db.GetWatermark(a.db, "x-blocks")
}

func (a *Assets) GetXChainPreCortinaWatermark() (uint64, error) {
	return db.GetWatermark(a.db, "x-precortina")
}
