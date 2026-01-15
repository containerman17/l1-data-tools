package utxos

import (
	"encoding/binary"
	"encoding/json"

	"github.com/cockroachdb/pebble/v2"
)

// Storage prefixes - chain-specific for different API schemas
const (
	// P-Chain storage (PChainUtxo schema)
	prefixPChainUTXO = "p-utxo:" // utxoID -> JSON
	prefixPChainAddr = "p-addr:" // addr:utxoID -> empty (address index)

	// C-Chain storage (Utxo schema - same struct, different serialization)
	prefixCChainUTXO = "c-utxo:" // utxoID -> JSON
	prefixCChainAddr = "c-addr:" // addr:utxoID -> empty (address index)

	// X-Chain storage (Utxo schema - same as C-Chain)
	prefixXChainUTXO = "x-utxo:" // utxoID -> JSON
	prefixXChainAddr = "x-addr:" // addr:utxoID -> empty (address index)

	// Spend index - write-only for fast consumption marking
	// Avoids read-modify-write pattern by storing spend info separately
	prefixSpent = "spent:" // chain:utxoID -> SpendInfo JSON

	// Asset metadata
	prefixAsset = "asset:" // assetID -> JSON (AssetMetadata)

	// Staking tx to stake output UTXO IDs mapping (for marking consumed on RewardValidatorTx)
	prefixStakingOutputs = "staking:outputs:" // stakingTxID -> JSON array of stake output UTXO IDs

	// FX Type IDs (Avalanche standard IDs for Snowflake export)
	fxIDSecp256k1 = "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ"
	fxIDNFT       = "qd2U4HDWUvMrVUeTcCHp6xH3Qpnn1XbU5MDdnBoiifFqvgXwT"
	fxIDProperty  = "2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w"

	// Legacy - keeping for backward compat during migration
	prefixUTXO = "utxo:" // old prefix
	prefixAddr = "addr:" // old prefix
)

// StoredUTXO is stored as JSON, matching API response format.
// All fields are JSON-compatible types (strings for big numbers, etc).
type StoredUTXO struct {
	// Core identity
	UTXOId      string `json:"utxoId"`
	TxHash      string `json:"txHash"`
	OutputIndex uint32 `json:"outputIndex"`

	// Value
	Amount  string `json:"amount"`
	AssetID string `json:"assetId"`

	// Ownership
	Addresses []string `json:"addresses"`
	Threshold uint32   `json:"threshold"`

	// Type info
	UTXOType         string  `json:"utxoType"` // "TRANSFER" or "STAKEABLE_LOCK"
	PlatformLocktime *uint64 `json:"platformLocktime,omitempty"`
	Staked           bool    `json:"staked"`

	// Creation metadata
	BlockNumber    string `json:"blockNumber"`
	BlockTimestamp int64  `json:"blockTimestamp"`

	// Chain info
	CreatedOnChainID  string `json:"createdOnChainId"`
	ConsumedOnChainID string `json:"consumedOnChainId"`

	// Consumption metadata (nil if unspent)
	ConsumingTxHash         *string `json:"consumingTxHash,omitempty"`
	ConsumingBlockNumber    *string `json:"consumingBlockNumber,omitempty"`
	ConsumingBlockTimestamp *int64  `json:"consumingBlockTimestamp,omitempty"`

	// Staking times (only for staked UTXOs)
	UTXOStartTimestamp *int64 `json:"utxoStartTimestamp,omitempty"`
	UTXOEndTimestamp   *int64 `json:"utxoEndTimestamp,omitempty"`

	// Raw UTXO bytes for API response
	UTXOBytes string `json:"utxoBytes,omitempty"`

	// NFT/Property fields
	Payload string  `json:"payload,omitempty"` // hex
	GroupID *uint32 `json:"groupId,omitempty"`

	// Credentials (C-Chain only) - signature proof of who authorized the tx
	Credentials []Credential `json:"credentials,omitempty"`

	// Snowflake export fields
	TxType            string   `json:"txType,omitempty"`            // Transaction type: AddValidator, ExportTx, etc.
	FxID              string   `json:"fxId,omitempty"`              // FX that created output: secp256k1fx, nftfx, propertyfx
	IsReward          bool     `json:"isReward,omitempty"`          // True if this is a staking reward UTXO
	StakeableLocktime *uint64  `json:"stakeableLocktime,omitempty"` // Locktime for StakeableLockOut
	NodeID            string   `json:"nodeId,omitempty"`            // Validator/delegator node ID
	RewardAddresses   []string `json:"rewardAddresses,omitempty"`   // Staking reward destination addresses
	LastUpdated       int64    `json:"lastUpdated,omitempty"`       // Unix timestamp of last update
}

// Credential represents a signature and recovered public key.
type Credential struct {
	PublicKey string `json:"publicKey"` // base64
	Signature string `json:"signature"` // base64
}

// AssetMetadata stores information about an asset.
type AssetMetadata struct {
	AssetID      string `json:"assetId"`
	Name         string `json:"name"`
	Symbol       string `json:"symbol"`
	Denomination int    `json:"denomination"`
}

// SpendInfo stores consumption info separately from UTXO records.
// This allows write-only marking of spent UTXOs (no read-modify-write).
type SpendInfo struct {
	ConsumingTxHash      string       `json:"consumingTxHash"`
	ConsumingTime        int64        `json:"consumingTime"`
	ConsumingBlockNumber string       `json:"consumingBlockNumber,omitempty"`
	Credentials          []Credential `json:"credentials,omitempty"`
	ConsumedOnChainID    string       `json:"consumedOnChainId,omitempty"` // For cross-chain
	CreatedOnChainID     string       `json:"createdOnChainId,omitempty"`  // For imports
}

// markSpent writes spend info to the spend index. Write-only, no reads.
// chainPrefix is "x:", "p:", or "c:" to namespace by chain.
func markSpent(batch *pebble.Batch, chainPrefix, utxoID string, info *SpendInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return batch.Set([]byte(prefixSpent+chainPrefix+utxoID), data, nil)
}

// getSpendInfo retrieves spend info for a UTXO. Returns nil if not spent.
func (u *UTXOs) getSpendInfo(chainPrefix, utxoID string) *SpendInfo {
	val, closer, err := u.db.Get([]byte(prefixSpent + chainPrefix + utxoID))
	if err != nil {
		return nil
	}
	defer closer.Close()

	var info SpendInfo
	if err := json.Unmarshal(val, &info); err != nil {
		return nil
	}
	return &info
}

// loadUTXO loads a UTXO by ID from the database using the given prefix.
func (u *UTXOs) loadUTXO(prefix, utxoID string) *StoredUTXO {
	val, closer, err := u.db.Get([]byte(prefix + utxoID))
	if err != nil {
		return nil
	}
	defer closer.Close()

	var stored StoredUTXO
	if err := json.Unmarshal(val, &stored); err != nil {
		return nil
	}
	return &stored
}

// loadPChainUTXO loads a P-Chain UTXO by ID.
func (u *UTXOs) loadPChainUTXO(utxoID string) *StoredUTXO {
	return u.loadUTXO(prefixPChainUTXO, utxoID)
}

// loadCChainUTXO loads a C-Chain UTXO by ID.
func (u *UTXOs) loadCChainUTXO(utxoID string) *StoredUTXO {
	return u.loadUTXO(prefixCChainUTXO, utxoID)
}

// saveUTXO saves a UTXO to the batch with the given prefix.
func saveUTXO(batch *pebble.Batch, prefix string, utxo *StoredUTXO) error {
	data, err := json.Marshal(utxo)
	if err != nil {
		return err
	}
	return batch.Set([]byte(prefix+utxo.UTXOId), data, nil)
}

// savePChainUTXO saves a P-Chain UTXO.
func savePChainUTXO(batch *pebble.Batch, utxo *StoredUTXO) error {
	return saveUTXO(batch, prefixPChainUTXO, utxo)
}

// saveCChainUTXO saves a C-Chain UTXO.
func saveCChainUTXO(batch *pebble.Batch, utxo *StoredUTXO) error {
	return saveUTXO(batch, prefixCChainUTXO, utxo)
}

// upsertUTXO loads existing UTXO (if any) and applies updates.
// Only non-nil fields in updates are applied.
func (u *UTXOs) upsertUTXO(batch *pebble.Batch, prefix, utxoID string, updates map[string]any) (*StoredUTXO, error) {
	// Try to load existing from batch first, then from db
	var existing *StoredUTXO
	key := []byte(prefix + utxoID)

	if val, closer, err := batch.Get(key); err == nil {
		var stored StoredUTXO
		if json.Unmarshal(val, &stored) == nil {
			existing = &stored
		}
		closer.Close()
	} else if val, closer, err := u.db.Get(key); err == nil {
		var stored StoredUTXO
		if json.Unmarshal(val, &stored) == nil {
			existing = &stored
		}
		closer.Close()
	}

	if existing == nil {
		existing = &StoredUTXO{UTXOId: utxoID}
	}

	// Apply updates
	for k, v := range updates {
		if v == nil {
			continue
		}
		switch k {
		case "txHash":
			existing.TxHash = v.(string)
		case "outputIndex":
			existing.OutputIndex = v.(uint32)
		case "amount":
			existing.Amount = v.(string)
		case "assetId":
			existing.AssetID = v.(string)
		case "addresses":
			existing.Addresses = v.([]string)
		case "threshold":
			existing.Threshold = v.(uint32)
		case "utxoType":
			existing.UTXOType = v.(string)
		case "platformLocktime":
			if locktime, ok := v.(uint64); ok {
				existing.PlatformLocktime = &locktime
			} else if locktime, ok := v.(*uint64); ok {
				existing.PlatformLocktime = locktime
			}
		case "staked":
			existing.Staked = v.(bool)
		case "blockNumber":
			existing.BlockNumber = v.(string)
		case "blockTimestamp":
			existing.BlockTimestamp = v.(int64)
		case "createdOnChainId":
			existing.CreatedOnChainID = v.(string)
		case "consumedOnChainId":
			existing.ConsumedOnChainID = v.(string)
		case "consumingTxHash":
			s := v.(string)
			existing.ConsumingTxHash = &s
		case "consumingBlockNumber":
			s := v.(string)
			existing.ConsumingBlockNumber = &s
		case "consumingBlockTimestamp":
			i := v.(int64)
			existing.ConsumingBlockTimestamp = &i
		case "utxoStartTimestamp":
			i := v.(int64)
			existing.UTXOStartTimestamp = &i
		case "utxoEndTimestamp":
			i := v.(int64)
			existing.UTXOEndTimestamp = &i
		case "utxoBytes":
			existing.UTXOBytes = v.(string)
		case "credentials":
			existing.Credentials = v.([]Credential)
		case "groupId":
			if gid, ok := v.(uint32); ok {
				existing.GroupID = &gid
			} else if gid, ok := v.(*uint32); ok {
				existing.GroupID = gid
			}
		// Snowflake export fields
		case "txType":
			existing.TxType = v.(string)
		case "fxId":
			existing.FxID = v.(string)
		case "isReward":
			existing.IsReward = v.(bool)
		case "stakeableLocktime":
			if locktime, ok := v.(uint64); ok {
				existing.StakeableLocktime = &locktime
			} else if locktime, ok := v.(*uint64); ok {
				existing.StakeableLocktime = locktime
			}
		case "nodeId":
			existing.NodeID = v.(string)
		case "rewardAddresses":
			existing.RewardAddresses = v.([]string)
		case "lastUpdated":
			existing.LastUpdated = v.(int64)
		}
	}

	// Save updated UTXO
	if err := saveUTXO(batch, prefix, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// upsertPChainUTXO upserts a P-Chain UTXO.
func (u *UTXOs) upsertPChainUTXO(batch *pebble.Batch, utxoID string, updates map[string]any) (*StoredUTXO, error) {
	return u.upsertUTXO(batch, prefixPChainUTXO, utxoID, updates)
}

// upsertCChainUTXO upserts a C-Chain UTXO.
func (u *UTXOs) upsertCChainUTXO(batch *pebble.Batch, utxoID string, updates map[string]any) (*StoredUTXO, error) {
	return u.upsertUTXO(batch, prefixCChainUTXO, utxoID, updates)
}

// upsertXChainUTXO upserts an X-Chain UTXO.
func (u *UTXOs) upsertXChainUTXO(batch *pebble.Batch, utxoID string, updates map[string]any) (*StoredUTXO, error) {
	return u.upsertUTXO(batch, prefixXChainUTXO, utxoID, updates)
}

// saveAssetMetadata saves asset metadata.
func saveAssetMetadata(batch *pebble.Batch, meta *AssetMetadata) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return batch.Set([]byte(prefixAsset+meta.AssetID), data, nil)
}

// loadAssetMetadata loads asset metadata.
func (u *UTXOs) loadAssetMetadata(assetID string) *AssetMetadata {
	val, closer, err := u.db.Get([]byte(prefixAsset + assetID))
	if err != nil {
		return nil
	}
	defer closer.Close()

	var meta AssetMetadata
	if err := json.Unmarshal(val, &meta); err != nil {
		return nil
	}
	return &meta
}

// GetPChainWatermark returns the last processed P-chain block height.
func (u *UTXOs) GetPChainWatermark() (uint64, error) {
	val, closer, err := u.db.Get([]byte("p:watermark"))
	if err == pebble.ErrNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer closer.Close()
	return binary.BigEndian.Uint64(val), nil
}

// GetXChainPreCortinaWatermark returns the last processed X-chain tx index.
func (u *UTXOs) GetXChainPreCortinaWatermark() (uint64, error) {
	val, closer, err := u.db.Get([]byte("x:preCortina:watermark"))
	if err == pebble.ErrNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer closer.Close()
	return binary.BigEndian.Uint64(val), nil
}

// GetXChainBlockWatermark returns the last processed X-chain block height.
func (u *UTXOs) GetXChainBlockWatermark() (uint64, error) {
	val, closer, err := u.db.Get([]byte("x:block:watermark"))
	if err == pebble.ErrNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer closer.Close()
	return binary.BigEndian.Uint64(val), nil
}

// GetCChainWatermark returns the last processed C-chain block height.
func (u *UTXOs) GetCChainWatermark() (uint64, error) {
	val, closer, err := u.db.Get([]byte("c:watermark"))
	if err == pebble.ErrNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer closer.Close()
	return binary.BigEndian.Uint64(val), nil
}
