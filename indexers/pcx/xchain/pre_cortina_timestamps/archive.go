// Package pre_cortina_timestamps provides timestamp lookups for pre-Cortina X-Chain data.
// These timestamps come from Glacier's API and are stored in a compact binary format.
package pre_cortina_timestamps

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/xchain/prefixarchive"
)

//go:embed fuji.bin
var fujiArchiveData []byte

var (
	fujiArchive     *Archive
	fujiArchiveOnce sync.Once
	fujiArchiveErr  error
)

// Archive is a lookup table for pre-Cortina X-Chain timestamps.
// Wraps prefixarchive.Archive with a domain-specific API.
type Archive struct {
	*prefixarchive.Archive
}

// GetFujiArchive returns the loaded Fuji archive, loading it on first call.
// Subsequent calls return the cached instance.
func GetFujiArchive() (*Archive, error) {
	fujiArchiveOnce.Do(func() {
		fujiArchive, fujiArchiveErr = ReadFrom(bytes.NewReader(fujiArchiveData))
	})
	return fujiArchive, fujiArchiveErr
}

// GetMainnetArchive returns the loaded Mainnet archive.
// Currently not implemented - mainnet.bin needs to be built first.
func GetMainnetArchive() (*Archive, error) {
	return nil, fmt.Errorf("mainnet archive not implemented yet - run: go run ./cmd/scrape_timestamps mainnet")
}

// Lookup returns the timestamp for a given ID from the appropriate network archive.
// For Fuji, uses the embedded archive. For Mainnet, returns error (not yet implemented).
func Lookup(network string, id ids.ID) (int64, bool, error) {
	var archive *Archive
	var err error

	switch network {
	case "fuji", "testnet":
		archive, err = GetFujiArchive()
	case "mainnet":
		archive, err = GetMainnetArchive()
	default:
		return 0, false, fmt.Errorf("unknown network: %s", network)
	}

	if err != nil {
		return 0, false, err
	}

	ts, found := archive.Lookup(id)
	return ts, found, nil
}

// Build creates an archive from hash => timestamp mappings.
func Build(data map[string]int64) (*Archive, error) {
	archive, err := prefixarchive.Build(data)
	if err != nil {
		return nil, err
	}
	return &Archive{Archive: archive}, nil
}

// ReadFrom reads an archive from a reader.
func ReadFrom(r io.Reader) (*Archive, error) {
	archive, err := prefixarchive.ReadFrom(r)
	if err != nil {
		return nil, err
	}
	return &Archive{Archive: archive}, nil
}

// Load reads an archive from a file.
func Load(path string) (*Archive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadFrom(f)
}

// LookupVertex returns the timestamp for a vertex ID.
// Alias for Lookup for semantic clarity.
func (a *Archive) LookupVertex(id ids.ID) (int64, bool) {
	return a.Lookup(id)
}

// LookupTransaction returns the timestamp for a transaction ID.
// Alias for Lookup for semantic clarity.
func (a *Archive) LookupTransaction(id ids.ID) (int64, bool) {
	return a.Lookup(id)
}
