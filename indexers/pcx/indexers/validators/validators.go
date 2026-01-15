package validators

import (
	"context"
	"path/filepath"

	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/db"
	"github.com/cockroachdb/pebble/v2"
)

type Validators struct {
	db        *pebble.DB
	networkID uint32
}

func New() *Validators {
	return &Validators{}
}

func (v *Validators) Name() string {
	return "validators"
}

func (v *Validators) Init(ctx context.Context, baseDir string, networkID uint32) error {
	v.networkID = networkID
	dir := filepath.Join(baseDir, "validators")
	storage, err := pebble.Open(dir, &pebble.Options{
		Logger: db.QuietLogger(),
	})
	if err != nil {
		return err
	}
	v.db = storage
	return nil
}

func (v *Validators) Close() error {
	if v.db != nil {
		return v.db.Close()
	}
	return nil
}

func (v *Validators) GetPChainWatermark() (uint64, error) {
	return db.GetWatermark(v.db, "p-blocks")
}
