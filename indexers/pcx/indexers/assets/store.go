package assets

import (
	"encoding/json"

	"github.com/cockroachdb/pebble/v2"
)

const (
	prefixAsset = "asset:" // assetID -> JSON (AssetMetadata)
)

type AssetMetadata struct {
	AssetID            string `json:"assetId"`
	Name               string `json:"name"`
	Symbol             string `json:"symbol"`
	Denomination       int    `json:"denomination"`
	CreatedAtTimestamp int64  `json:"createdAtTimestamp"`
	Type               string `json:"type"`
	Cap                string `json:"cap"`
}

func (a *Assets) saveAsset(batch *pebble.Batch, meta *AssetMetadata) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return batch.Set([]byte(prefixAsset+meta.AssetID), data, pebble.NoSync)
}

func (a *Assets) getAsset(assetID string) (*AssetMetadata, error) {
	data, closer, err := a.db.Get([]byte(prefixAsset + assetID))
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	var meta AssetMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}
