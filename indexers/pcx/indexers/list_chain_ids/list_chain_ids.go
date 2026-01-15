package list_chain_ids

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/db"
	"github.com/cockroachdb/pebble/v2"
)

// Chain IDs
const (
	pChainID        = "11111111111111111111111111111111LpoYY"
	cChainIDMainnet = "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5"
	cChainIDFuji    = "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp"
	xChainIDMainnet = "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM"
	xChainIDFuji    = "2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm"
)

type Chains struct {
	db        *pebble.DB
	networkID uint32
	initOnce  sync.Once
	initErr   error
}

func New() *Chains {
	return &Chains{}
}

func (c *Chains) Name() string { return "list_chain_ids" }

func (c *Chains) Init(ctx context.Context, baseDir string, networkID uint32) error {
	c.initOnce.Do(func() {
		c.networkID = networkID
		dir := filepath.Join(baseDir, "list_chain_ids")
		var err error
		c.db, err = pebble.Open(dir, &pebble.Options{Logger: db.QuietLogger()})
		if err != nil {
			c.initErr = err
			return
		}
	})
	return c.initErr
}

func (c *Chains) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

func (c *Chains) getCChainID() string {
	if c.networkID == 1 {
		return cChainIDMainnet
	}
	return cChainIDFuji
}

func (c *Chains) getXChainID() string {
	if c.networkID == 1 {
		return xChainIDMainnet
	}
	return xChainIDFuji
}
