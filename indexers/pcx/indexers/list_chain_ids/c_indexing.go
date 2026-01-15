package list_chain_ids

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/indexer"
	"github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/pchain"
	"github.com/cockroachdb/pebble/v2"
)

// ApricotPhase5 timestamps (batch atomic txs)
const (
	ap5MainnetTimestamp int64 = 1638468000 // Dec 2, 2021 18:00 UTC
	ap5FujiTimestamp    int64 = 1637766000 // Nov 24, 2021 15:00 UTC
)

// Atomic transaction codec (matches coreth's codec exactly)
var (
	atomicCodec        codec.Manager
	atomicCodecVersion = uint16(0)
)

func init() {
	atomicCodec = codec.NewDefaultManager()
	lc := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		lc.RegisterType(&unsignedImportTx{}),
		lc.RegisterType(&unsignedExportTx{}),
	)
	lc.SkipRegistrations(3) // Skip unused types
	errs.Add(
		lc.RegisterType(&secp256k1fx.TransferInput{}),
	)
	lc.SkipRegistrations(1)
	errs.Add(
		lc.RegisterType(&secp256k1fx.TransferOutput{}),
	)
	lc.SkipRegistrations(1)
	errs.Add(
		lc.RegisterType(&secp256k1fx.Credential{}),
		lc.RegisterType(&secp256k1fx.Input{}),
		lc.RegisterType(&secp256k1fx.OutputOwners{}),
		atomicCodec.RegisterCodec(atomicCodecVersion, lc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

type atomicTx struct {
	UnsignedAtomicTx `serialize:"true"`
	Creds            []verify.Verifiable `serialize:"true"`
	id               ids.ID
	bytes            []byte
}

func (tx *atomicTx) ID() ids.ID {
	if tx.id == ids.Empty {
		tx.id = hashing.ComputeHash256Array(tx.bytes)
	}
	return tx.id
}

type UnsignedAtomicTx interface{}

type unsignedImportTx struct {
	NetworkID      uint32                    `serialize:"true"`
	BlockchainID   ids.ID                    `serialize:"true"`
	SourceChain    ids.ID                    `serialize:"true"`
	ImportedInputs []*avax.TransferableInput `serialize:"true"`
	Outs           []evmOutput               `serialize:"true"`
}

type unsignedExportTx struct {
	NetworkID        uint32                     `serialize:"true"`
	BlockchainID     ids.ID                     `serialize:"true"`
	DestinationChain ids.ID                     `serialize:"true"`
	Ins              []evmInput                 `serialize:"true"`
	ExportedOutputs  []*avax.TransferableOutput `serialize:"true"`
}

type evmOutput struct {
	Address [20]byte `serialize:"true"`
	Amount  uint64   `serialize:"true"`
	AssetID ids.ID   `serialize:"true"`
}

type evmInput struct {
	Address [20]byte `serialize:"true"`
	Amount  uint64   `serialize:"true"`
	AssetID ids.ID   `serialize:"true"`
	Nonce   uint64   `serialize:"true"`
}

func (c *Chains) ProcessCChainBatch(ctx context.Context, blocks []indexer.CBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	batch := c.db.NewIndexedBatch()
	defer batch.Close()

	hrp := pchain.GetHRP(c.networkID)
	cChainID := c.getCChainID()
	ap5Timestamp := ap5FujiTimestamp
	if c.networkID == 1 {
		ap5Timestamp = ap5MainnetTimestamp
	}

	for _, blk := range blocks {
		if len(blk.ExtraData) == 0 {
			continue
		}

		isAP5 := blk.Timestamp >= ap5Timestamp
		txs, err := extractAtomicTxs(blk.ExtraData, isAP5)
		if err != nil {
			return fmt.Errorf("extract atomic txs at block %d: %w", blk.Height, err)
		}

		for _, tx := range txs {
			c.processCTx(batch, tx, cChainID, hrp)
		}
	}

	lastHeight := blocks[len(blocks)-1].Height
	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, lastHeight)
	batch.Set([]byte("c:watermark"), heightBytes, nil)

	return batch.Commit(pebble.NoSync)
}

func (c *Chains) processCTx(batch *pebble.Batch, tx *atomicTx, cChainID string, hrp string) {
	switch utx := tx.UnsignedAtomicTx.(type) {
	case *unsignedExportTx:
		destChain := utx.DestinationChain.String()
		for _, out := range utx.ExportedOutputs {
			c.markAddressesTouched(batch, out.Out, cChainID, hrp)
			c.markAddressesTouched(batch, out.Out, destChain, hrp)
		}
	case *unsignedImportTx:
		sourceChain := utx.SourceChain.String()
		for _, in := range utx.ImportedInputs {
			c.markAddressesTouched(batch, in.In, cChainID, hrp)
			c.markAddressesTouched(batch, in.In, sourceChain, hrp)
		}
	}
}

func extractAtomicTxs(extraData []byte, isAP5 bool) ([]*atomicTx, error) {
	if len(extraData) == 0 {
		return nil, nil
	}

	if !isAP5 {
		tx := &atomicTx{}
		if _, err := atomicCodec.Unmarshal(extraData, tx); err != nil {
			return nil, fmt.Errorf("unmarshal pre-AP5 atomic tx: %w", err)
		}
		tx.bytes = extraData
		return []*atomicTx{tx}, nil
	}

	var txs []*atomicTx
	if _, err := atomicCodec.Unmarshal(extraData, &txs); err != nil {
		return nil, fmt.Errorf("unmarshal AP5 atomic txs: %w", err)
	}

	for i, tx := range txs {
		bytes, err := atomicCodec.Marshal(atomicCodecVersion, tx)
		if err != nil {
			return nil, fmt.Errorf("marshal atomic tx %d for ID: %w", i, err)
		}
		tx.bytes = bytes
	}

	return txs, nil
}
