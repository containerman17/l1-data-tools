package utxos

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStoredUTXO_MarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		utxo StoredUTXO
		want map[string]bool // fields that should be present
		omit map[string]bool // fields that should be omitted
	}{
		{
			name: "P-Chain UTXO with platformLocktime",
			utxo: StoredUTXO{
				UTXOId:           "test-id",
				TxHash:           "test-tx",
				OutputIndex:      0,
				Amount:           "1000",
				AssetID:          "test-asset",
				Addresses:        []string{"addr1"},
				Threshold:        1,
				UTXOType:         "TRANSFER",
				PlatformLocktime: uint64Ptr(1234),
				Staked:           false,
				BlockNumber:      "100",
				BlockTimestamp:   1000000,
				CreatedOnChainID: "11111111111111111111111111111111LpoYY",
			},
			want: map[string]bool{
				"platformLocktime": true,
			},
			omit: map[string]bool{},
		},
		{
			name: "Cross-chain UTXO without platformLocktime",
			utxo: StoredUTXO{
				UTXOId:           "test-id-2",
				TxHash:           "test-tx-2",
				OutputIndex:      0,
				Amount:           "2000",
				AssetID:          "test-asset",
				Addresses:        []string{"addr1"},
				Threshold:        1,
				UTXOType:         "TRANSFER",
				PlatformLocktime: nil, // Cross-chain UTXO
				Staked:           false,
				BlockNumber:      "200",
				BlockTimestamp:   2000000,
				CreatedOnChainID: "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp",
			},
			want: map[string]bool{},
			omit: map[string]bool{
				"platformLocktime": true,
			},
		},
		{
			name: "UTXO with optional fields",
			utxo: StoredUTXO{
				UTXOId:                  "test-id-3",
				TxHash:                  "test-tx-3",
				OutputIndex:             0,
				Amount:                  "3000",
				AssetID:                 "test-asset",
				Addresses:               []string{"addr1"},
				Threshold:               1,
				UTXOType:                "TRANSFER",
				PlatformLocktime:        uint64Ptr(0),
				Staked:                  false,
				BlockNumber:             "300",
				BlockTimestamp:          3000000,
				CreatedOnChainID:        "11111111111111111111111111111111LpoYY",
				ConsumingTxHash:         stringPtr("consuming-tx"),
				ConsumingBlockNumber:    stringPtr("400"),
				ConsumingBlockTimestamp: int64Ptr(4000000),
				UTXOBytes:               "0x1234",
			},
			want: map[string]bool{
				"platformLocktime":        true,
				"consumingTxHash":         true,
				"consumingBlockNumber":    true,
				"consumingBlockTimestamp": true,
				"utxoBytes":               true,
			},
			omit: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.utxo)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			jsonStr := string(data)

			// Check for expected fields
			for field := range tt.want {
				if !strings.Contains(jsonStr, `"`+field+`"`) {
					t.Errorf("Expected field %q to be present in JSON, but it's missing.\nJSON: %s", field, jsonStr)
				}
			}

			// Check that omitted fields are not present
			for field := range tt.omit {
				if strings.Contains(jsonStr, `"`+field+`"`) {
					t.Errorf("Expected field %q to be omitted from JSON, but it's present.\nJSON: %s", field, jsonStr)
				}
			}

			// Verify it can be unmarshaled back
			var unmarshaledMap map[string]interface{}
			if err := json.Unmarshal(data, &unmarshaledMap); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			// Verify presence/absence in unmarshaled map
			for field := range tt.want {
				if _, exists := unmarshaledMap[field]; !exists {
					t.Errorf("Field %q should exist in unmarshaled map", field)
				}
			}

			for field := range tt.omit {
				if val, exists := unmarshaledMap[field]; exists {
					t.Errorf("Field %q should NOT exist in unmarshaled map, but has value: %v", field, val)
				}
			}
		})
	}
}

// Helper functions for creating pointers
func uint64Ptr(v uint64) *uint64 {
	return &v
}

func stringPtr(s string) *string {
	return &s
}

func int64Ptr(i int64) *int64 {
	return &i
}
