package utxos

import (
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
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

// atomicTx represents a C-Chain atomic transaction.
// Must match coreth's Tx struct exactly - UnsignedAtomicTx is EMBEDDED and EXPORTED.
type atomicTx struct {
	UnsignedAtomicTx `serialize:"true"`  // Embedded interface (matches coreth)
	Creds            []verify.Verifiable `serialize:"true"`

	// Cached
	id    ids.ID
	bytes []byte
}

func (tx *atomicTx) ID() ids.ID {
	if tx.id == ids.Empty {
		tx.id = hashing.ComputeHash256Array(tx.bytes)
	}
	return tx.id
}

// UnsignedAtomicTx is the interface for unsigned atomic transactions.
type UnsignedAtomicTx interface{}

// unsignedImportTx imports assets from another chain to C-Chain.
type unsignedImportTx struct {
	NetworkID      uint32                    `serialize:"true"`
	BlockchainID   ids.ID                    `serialize:"true"`
	SourceChain    ids.ID                    `serialize:"true"`
	ImportedInputs []*avax.TransferableInput `serialize:"true"`
	Outs           []evmOutput               `serialize:"true"`
}

// unsignedExportTx exports assets from C-Chain to another chain.
type unsignedExportTx struct {
	NetworkID        uint32                     `serialize:"true"`
	BlockchainID     ids.ID                     `serialize:"true"`
	DestinationChain ids.ID                     `serialize:"true"`
	Ins              []evmInput                 `serialize:"true"`
	ExportedOutputs  []*avax.TransferableOutput `serialize:"true"`
}

// evmOutput represents an output credited to an EVM address.
type evmOutput struct {
	Address [20]byte `serialize:"true"`
	Amount  uint64   `serialize:"true"`
	AssetID ids.ID   `serialize:"true"`
}

// evmInput represents an input debited from an EVM address.
type evmInput struct {
	Address [20]byte `serialize:"true"`
	Amount  uint64   `serialize:"true"`
	AssetID ids.ID   `serialize:"true"`
	Nonce   uint64   `serialize:"true"`
}

// extractAtomicTxs parses atomic transactions from blockExtraData.
// If isAP5 is true, expects a batch of transactions (post-ApricotPhase5).
// If false, expects a single transaction (pre-ApricotPhase5).
func extractAtomicTxs(extraData []byte, isAP5 bool) ([]*atomicTx, error) {
	if len(extraData) == 0 {
		return nil, nil
	}

	if !isAP5 {
		// Pre-AP5: single transaction
		tx := &atomicTx{}
		if _, err := atomicCodec.Unmarshal(extraData, tx); err != nil {
			return nil, fmt.Errorf("unmarshal pre-AP5 atomic tx: %w", err)
		}
		tx.bytes = extraData
		return []*atomicTx{tx}, nil
	}

	// Post-AP5: batch of transactions
	var txs []*atomicTx
	if _, err := atomicCodec.Unmarshal(extraData, &txs); err != nil {
		return nil, fmt.Errorf("unmarshal AP5 atomic txs: %w", err)
	}

	// Initialize each tx
	for i, tx := range txs {
		// Re-marshal to get bytes for ID calculation
		bytes, err := atomicCodec.Marshal(atomicCodecVersion, tx)
		if err != nil {
			return nil, fmt.Errorf("marshal atomic tx %d for ID: %w", i, err)
		}
		tx.bytes = bytes
	}

	return txs, nil
}
