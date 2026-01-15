package cchain

import (
	"encoding/json"
	"testing"
)

func TestBlockDataEncodeDecode(t *testing.T) {
	original := BlockData{
		Height:        12345678,
		Timestamp:     1765822217,
		Hash:          "0x310cc05701f076250416ef4b79790b6a622684a8223ee6ce753dc277f7e89ccb",
		ParentHash:    "0x9104c1e47dfdce595a9c423d3b46805d42e1954b0056f7a4f6ef89c196ea4cb9",
		Size:          9880,
		TxCount:       42,
		GasLimit:      15000000,
		GasUsed:       8500000,
		BaseFeePerGas: 25000000000,
		Miner:         "0x0100000000000000000000000000000000000005",
		ExtDataHash:   "0x0000000000000000000000000000000000000000000000000000000000000000",
		ExtraData:     []byte{0x01, 0x02, 0x03, 0x04, 0x05},
	}

	// Encode
	encoded := original.Encode()
	t.Logf("Encoded size: %d bytes (fixed: %d + extra: %d)", len(encoded), blockDataFixedSize, len(original.ExtraData))

	// Decode
	var decoded BlockData
	if err := decoded.Decode(encoded); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Compare
	if decoded.Height != original.Height {
		t.Errorf("Height mismatch: got %d, want %d", decoded.Height, original.Height)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp mismatch: got %d, want %d", decoded.Timestamp, original.Timestamp)
	}
	if decoded.Hash != original.Hash {
		t.Errorf("Hash mismatch: got %s, want %s", decoded.Hash, original.Hash)
	}
	if decoded.ParentHash != original.ParentHash {
		t.Errorf("ParentHash mismatch: got %s, want %s", decoded.ParentHash, original.ParentHash)
	}
	if decoded.Size != original.Size {
		t.Errorf("Size mismatch: got %d, want %d", decoded.Size, original.Size)
	}
	if decoded.TxCount != original.TxCount {
		t.Errorf("TxCount mismatch: got %d, want %d", decoded.TxCount, original.TxCount)
	}
	if decoded.GasLimit != original.GasLimit {
		t.Errorf("GasLimit mismatch: got %d, want %d", decoded.GasLimit, original.GasLimit)
	}
	if decoded.GasUsed != original.GasUsed {
		t.Errorf("GasUsed mismatch: got %d, want %d", decoded.GasUsed, original.GasUsed)
	}
	if decoded.BaseFeePerGas != original.BaseFeePerGas {
		t.Errorf("BaseFeePerGas mismatch: got %d, want %d", decoded.BaseFeePerGas, original.BaseFeePerGas)
	}
	if decoded.Miner != original.Miner {
		t.Errorf("Miner mismatch: got %s, want %s", decoded.Miner, original.Miner)
	}
	if decoded.ExtDataHash != original.ExtDataHash {
		t.Errorf("ExtDataHash mismatch: got %s, want %s", decoded.ExtDataHash, original.ExtDataHash)
	}
	if string(decoded.ExtraData) != string(original.ExtraData) {
		t.Errorf("ExtraData mismatch: got %v, want %v", decoded.ExtraData, original.ExtraData)
	}
}

func TestBlockDataEncodeDecodeEmptyExtra(t *testing.T) {
	original := BlockData{
		Height:    999,
		Timestamp: 1234567890,
		Hash:      "0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	}

	encoded := original.Encode()
	t.Logf("Encoded size (no extra): %d bytes", len(encoded))

	var decoded BlockData
	if err := decoded.Decode(encoded); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decoded.Height != original.Height {
		t.Errorf("Height mismatch: got %d, want %d", decoded.Height, original.Height)
	}
	if len(decoded.ExtraData) != 0 {
		t.Errorf("ExtraData should be empty, got %d bytes", len(decoded.ExtraData))
	}
}

func BenchmarkBlockDataEncode(b *testing.B) {
	blk := BlockData{
		Height:        12345678,
		Timestamp:     1765822217,
		Hash:          "0x310cc05701f076250416ef4b79790b6a622684a8223ee6ce753dc277f7e89ccb",
		ParentHash:    "0x9104c1e47dfdce595a9c423d3b46805d42e1954b0056f7a4f6ef89c196ea4cb9",
		Size:          9880,
		TxCount:       42,
		GasLimit:      15000000,
		GasUsed:       8500000,
		BaseFeePerGas: 25000000000,
		Miner:         "0x0100000000000000000000000000000000000005",
		ExtDataHash:   "0x0000000000000000000000000000000000000000000000000000000000000000",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = blk.Encode()
	}
}

func BenchmarkBlockDataDecode(b *testing.B) {
	blk := BlockData{
		Height:        12345678,
		Timestamp:     1765822217,
		Hash:          "0x310cc05701f076250416ef4b79790b6a622684a8223ee6ce753dc277f7e89ccb",
		ParentHash:    "0x9104c1e47dfdce595a9c423d3b46805d42e1954b0056f7a4f6ef89c196ea4cb9",
		Size:          9880,
		TxCount:       42,
		GasLimit:      15000000,
		GasUsed:       8500000,
		BaseFeePerGas: 25000000000,
		Miner:         "0x0100000000000000000000000000000000000005",
		ExtDataHash:   "0x0000000000000000000000000000000000000000000000000000000000000000",
	}
	encoded := blk.Encode()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var decoded BlockData
		_ = decoded.Decode(encoded)
	}
}

// JSON comparison type for benchmarking
type blockDataJSON struct {
	Height        uint64 `json:"h"`
	Timestamp     int64  `json:"ts"`
	Hash          string `json:"hash"`
	ParentHash    string `json:"parent"`
	Size          int    `json:"size"`
	TxCount       int    `json:"txc"`
	GasLimit      uint64 `json:"gasLimit,omitempty"`
	GasUsed       uint64 `json:"gasUsed,omitempty"`
	BaseFeePerGas uint64 `json:"baseFee,omitempty"`
	Miner         string `json:"miner,omitempty"`
	ExtDataHash   string `json:"extraHash,omitempty"`
	ExtraData     []byte `json:"extra,omitempty"`
}

func BenchmarkBlockDataEncodeJSON(b *testing.B) {
	blk := blockDataJSON{
		Height:        12345678,
		Timestamp:     1765822217,
		Hash:          "0x310cc05701f076250416ef4b79790b6a622684a8223ee6ce753dc277f7e89ccb",
		ParentHash:    "0x9104c1e47dfdce595a9c423d3b46805d42e1954b0056f7a4f6ef89c196ea4cb9",
		Size:          9880,
		TxCount:       42,
		GasLimit:      15000000,
		GasUsed:       8500000,
		BaseFeePerGas: 25000000000,
		Miner:         "0x0100000000000000000000000000000000000005",
		ExtDataHash:   "0x0000000000000000000000000000000000000000000000000000000000000000",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(blk)
	}
}

func BenchmarkBlockDataDecodeJSON(b *testing.B) {
	blk := blockDataJSON{
		Height:        12345678,
		Timestamp:     1765822217,
		Hash:          "0x310cc05701f076250416ef4b79790b6a622684a8223ee6ce753dc277f7e89ccb",
		ParentHash:    "0x9104c1e47dfdce595a9c423d3b46805d42e1954b0056f7a4f6ef89c196ea4cb9",
		Size:          9880,
		TxCount:       42,
		GasLimit:      15000000,
		GasUsed:       8500000,
		BaseFeePerGas: 25000000000,
		Miner:         "0x0100000000000000000000000000000000000005",
		ExtDataHash:   "0x0000000000000000000000000000000000000000000000000000000000000000",
	}
	encoded, _ := json.Marshal(blk)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var decoded blockDataJSON
		_ = json.Unmarshal(encoded, &decoded)
	}
}
