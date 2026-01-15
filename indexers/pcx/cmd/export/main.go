package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/cockroachdb/pebble/v2"
)

// StoredUTXO mirrors the struct from indexers/utxos/store.go
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
	UTXOType         string  `json:"utxoType"`
	PlatformLocktime *uint64 `json:"platformLocktime,omitempty"`
	Staked           bool    `json:"staked"`

	// Creation metadata
	BlockNumber    string `json:"blockNumber"`
	BlockTimestamp int64  `json:"blockTimestamp"`

	// Chain info
	CreatedOnChainID  string `json:"createdOnChainId"`
	ConsumedOnChainID string `json:"consumedOnChainId"`

	// Consumption metadata
	ConsumingTxHash         *string `json:"consumingTxHash,omitempty"`
	ConsumingBlockNumber    *string `json:"consumingBlockNumber,omitempty"`
	ConsumingBlockTimestamp *int64  `json:"consumingBlockTimestamp,omitempty"`

	// Staking times
	UTXOStartTimestamp *int64 `json:"utxoStartTimestamp,omitempty"`
	UTXOEndTimestamp   *int64 `json:"utxoEndTimestamp,omitempty"`

	// Raw UTXO bytes
	UTXOBytes string `json:"utxoBytes,omitempty"`

	// NFT/Property fields
	Payload string  `json:"payload,omitempty"`
	GroupID *uint32 `json:"groupId,omitempty"`

	// Credentials
	Credentials []Credential `json:"credentials,omitempty"`

	// Snowflake export fields
	TxType            string   `json:"txType,omitempty"`
	FxID              string   `json:"fxId,omitempty"`
	IsReward          bool     `json:"isReward,omitempty"`
	StakeableLocktime *uint64  `json:"stakeableLocktime,omitempty"`
	NodeID            string   `json:"nodeId,omitempty"`
	RewardAddresses   []string `json:"rewardAddresses,omitempty"`
	LastUpdated       int64    `json:"lastUpdated,omitempty"`
}

type Credential struct {
	PublicKey string `json:"publicKey"`
	Signature string `json:"signature"`
}

// SnowflakeRow represents a row in the Snowflake UTXOS table
type SnowflakeRow struct {
	UTXO_ID                         string
	TRANSACTION_HASH                string
	BLOCK_INDEX                     string
	OUTPUT_INDEX                    uint32
	TIMESTAMP                       string // ISO format
	TX_TYPE                         string
	OUTPUT_TYPE                     string
	ADDRESS                         string
	CONSUMING_TRANSACTION_HASH      string
	ASSET_ID                        string
	LOCKTIME                        string
	THRESHOLD                       uint32
	PUBLIC_KEY                      string
	SIGNATURE                       string
	AMOUNT                          string
	GROUP_ID                        string
	PAYLOAD                         string
	FX_ID                           string
	CREATED_ON                      string
	CONSUMED_ON                     string
	PLATFORM_LOCKTIME               string
	STAKED                          bool
	STAKEABLE_LOCKTIME              string
	REWARD                          bool
	LAST_UPDATED                    string
	CONSUMING_TRANSACTION_TIMESTAMP string
	STAKING_START_TIME              string
	STAKING_END_TIME                string
	NODE_ID                         string
	REWARD_ADDRESSES                string // JSON array
	CONSUMING_BLOCK_HEIGHT          string
}

func main() {
	dataDir := flag.String("data", "data/1/utxos", "Path to UTXO database")
	outputFile := flag.String("output", "utxos_export.csv", "Output file path (use .csv or .json)")
	chain := flag.String("chain", "p", "Chain to export: p, x, or c")
	limit := flag.Int("limit", 0, "Limit number of rows (0 = no limit)")
	flag.Parse()

	// Determine prefix based on chain
	var prefix string
	switch *chain {
	case "p":
		prefix = "p-utxo:"
	case "x":
		prefix = "x-utxo:"
	case "c":
		prefix = "c-utxo:"
	default:
		log.Fatalf("Invalid chain: %s. Use p, x, or c", *chain)
	}

	// Open database
	db, err := pebble.Open(*dataDir, &pebble.Options{ReadOnly: true})
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Determine output format
	isJSON := strings.HasSuffix(*outputFile, ".json")

	// Open output file
	file, err := os.Create(*outputFile)
	if err != nil {
		log.Fatalf("Failed to create output file: %v", err)
	}
	defer file.Close()

	var csvWriter *csv.Writer
	var jsonEncoder *json.Encoder

	if isJSON {
		jsonEncoder = json.NewEncoder(file)
		file.WriteString("[\n")
	} else {
		csvWriter = csv.NewWriter(file)
		// Write header
		csvWriter.Write([]string{
			"UTXO_ID", "TRANSACTION_HASH", "BLOCK_INDEX", "OUTPUT_INDEX", "TIMESTAMP",
			"TX_TYPE", "OUTPUT_TYPE", "ADDRESS", "CONSUMING_TRANSACTION_HASH", "ASSET_ID",
			"LOCKTIME", "THRESHOLD", "PUBLIC_KEY", "SIGNATURE", "AMOUNT",
			"GROUP_ID", "PAYLOAD", "FX_ID", "CREATED_ON", "CONSUMED_ON",
			"PLATFORM_LOCKTIME", "STAKED", "STAKEABLE_LOCKTIME", "REWARD", "LAST_UPDATED",
			"CONSUMING_TRANSACTION_TIMESTAMP", "STAKING_START_TIME", "STAKING_END_TIME",
			"NODE_ID", "REWARD_ADDRESSES", "CONSUMING_BLOCK_HEIGHT",
		})
	}

	// Iterate over UTXOs
	iter, err := db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "\xff"),
	})
	if err != nil {
		log.Fatalf("Failed to create iterator: %v", err)
	}
	defer iter.Close()

	count := 0
	firstJSON := true
	for iter.First(); iter.Valid(); iter.Next() {
		if *limit > 0 && count >= *limit {
			break
		}

		var utxo StoredUTXO
		if err := json.Unmarshal(iter.Value(), &utxo); err != nil {
			log.Printf("Warning: failed to unmarshal UTXO: %v", err)
			continue
		}

		row := convertToSnowflakeRow(&utxo)

		if isJSON {
			if !firstJSON {
				file.WriteString(",\n")
			}
			firstJSON = false
			jsonEncoder.Encode(row)
		} else {
			csvWriter.Write(rowToStringSlice(row))
		}

		count++
		if count%10000 == 0 {
			log.Printf("Exported %d UTXOs...", count)
		}
	}

	if isJSON {
		file.WriteString("\n]")
	} else {
		csvWriter.Flush()
	}

	log.Printf("Done! Exported %d UTXOs to %s", count, *outputFile)
}

func convertToSnowflakeRow(u *StoredUTXO) *SnowflakeRow {
	row := &SnowflakeRow{
		UTXO_ID:          u.UTXOId,
		TRANSACTION_HASH: u.TxHash,
		BLOCK_INDEX:      u.BlockNumber,
		OUTPUT_INDEX:     u.OutputIndex,
		TX_TYPE:          u.TxType,
		OUTPUT_TYPE:      u.UTXOType,
		ASSET_ID:         u.AssetID,
		THRESHOLD:        u.Threshold,
		AMOUNT:           u.Amount,
		PAYLOAD:          u.Payload,
		FX_ID:            u.FxID,
		CREATED_ON:       u.CreatedOnChainID,
		CONSUMED_ON:      u.ConsumedOnChainID,
		STAKED:           u.Staked,
		REWARD:           u.IsReward,
		NODE_ID:          u.NodeID,
	}

	// Convert timestamp to ISO format
	if u.BlockTimestamp > 0 {
		row.TIMESTAMP = time.Unix(u.BlockTimestamp, 0).UTC().Format(time.RFC3339)
	}

	// First address
	if len(u.Addresses) > 0 {
		row.ADDRESS = u.Addresses[0]
	}

	// Consumption fields
	if u.ConsumingTxHash != nil {
		row.CONSUMING_TRANSACTION_HASH = *u.ConsumingTxHash
	}
	if u.ConsumingBlockNumber != nil {
		row.CONSUMING_BLOCK_HEIGHT = *u.ConsumingBlockNumber
	}
	if u.ConsumingBlockTimestamp != nil {
		row.CONSUMING_TRANSACTION_TIMESTAMP = time.Unix(*u.ConsumingBlockTimestamp, 0).UTC().Format(time.RFC3339)
	}

	// Locktime fields
	if u.PlatformLocktime != nil {
		row.PLATFORM_LOCKTIME = time.Unix(int64(*u.PlatformLocktime), 0).UTC().Format(time.RFC3339)
		row.LOCKTIME = fmt.Sprintf("%d", *u.PlatformLocktime)
	}
	if u.StakeableLocktime != nil {
		row.STAKEABLE_LOCKTIME = time.Unix(int64(*u.StakeableLocktime), 0).UTC().Format(time.RFC3339)
	}

	// Staking times
	if u.UTXOStartTimestamp != nil {
		row.STAKING_START_TIME = time.Unix(*u.UTXOStartTimestamp, 0).UTC().Format(time.RFC3339)
	}
	if u.UTXOEndTimestamp != nil {
		row.STAKING_END_TIME = time.Unix(*u.UTXOEndTimestamp, 0).UTC().Format(time.RFC3339)
	}

	// Last updated
	if u.LastUpdated > 0 {
		row.LAST_UPDATED = time.Unix(u.LastUpdated, 0).UTC().Format(time.RFC3339)
	}

	// Group ID
	if u.GroupID != nil {
		row.GROUP_ID = fmt.Sprintf("%d", *u.GroupID)
	}

	// Credentials (first one if exists)
	if len(u.Credentials) > 0 {
		row.PUBLIC_KEY = u.Credentials[0].PublicKey
		row.SIGNATURE = u.Credentials[0].Signature
	}

	// Reward addresses as JSON array
	if len(u.RewardAddresses) > 0 {
		data, _ := json.Marshal(u.RewardAddresses)
		row.REWARD_ADDRESSES = string(data)
	}

	return row
}

func rowToStringSlice(r *SnowflakeRow) []string {
	return []string{
		r.UTXO_ID,
		r.TRANSACTION_HASH,
		r.BLOCK_INDEX,
		fmt.Sprintf("%d", r.OUTPUT_INDEX),
		r.TIMESTAMP,
		r.TX_TYPE,
		r.OUTPUT_TYPE,
		r.ADDRESS,
		r.CONSUMING_TRANSACTION_HASH,
		r.ASSET_ID,
		r.LOCKTIME,
		fmt.Sprintf("%d", r.THRESHOLD),
		r.PUBLIC_KEY,
		r.SIGNATURE,
		r.AMOUNT,
		r.GROUP_ID,
		r.PAYLOAD,
		r.FX_ID,
		r.CREATED_ON,
		r.CONSUMED_ON,
		r.PLATFORM_LOCKTIME,
		fmt.Sprintf("%t", r.STAKED),
		r.STAKEABLE_LOCKTIME,
		fmt.Sprintf("%t", r.REWARD),
		r.LAST_UPDATED,
		r.CONSUMING_TRANSACTION_TIMESTAMP,
		r.STAKING_START_TIME,
		r.STAKING_END_TIME,
		r.NODE_ID,
		r.REWARD_ADDRESSES,
		r.CONSUMING_BLOCK_HEIGHT,
	}
}
