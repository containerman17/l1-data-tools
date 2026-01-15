package pre_cortina_timestamps

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

func TestLoadFujiBinary(t *testing.T) {
	// Try to load the real Fuji binary if it exists
	archive, err := Load("fuji.bin")
	if err != nil {
		t.Skipf("Skipping: fuji.bin not found or invalid: %v", err)
	}

	t.Logf("Loaded fuji.bin: %d entries, %d-byte prefixes", archive.Len(), archive.PrefixLen())

	if archive.Len() == 0 {
		t.Error("Expected non-empty archive")
	}
}

func TestGetFujiArchive(t *testing.T) {
	// Test loading embedded archive
	archive, err := GetFujiArchive()
	if err != nil {
		t.Fatalf("GetFujiArchive failed: %v", err)
	}

	if archive.Len() == 0 {
		t.Error("Expected non-empty archive")
	}

	t.Logf("Loaded embedded Fuji archive: %d entries, %d-byte prefixes", archive.Len(), archive.PrefixLen())

	// Test that calling it again returns the same instance (cached)
	archive2, err := GetFujiArchive()
	if err != nil {
		t.Fatalf("Second GetFujiArchive failed: %v", err)
	}
	if archive != archive2 {
		t.Error("Expected cached instance to be returned")
	}
}

func TestLookup(t *testing.T) {
	// Test Fuji lookup
	testID := ids.GenerateTestID()
	_, found, err := Lookup("fuji", testID)
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}
	// Won't find random ID, but shouldn't error
	if found {
		t.Error("Unexpected: found random ID")
	}

	// Test testnet alias
	_, _, err = Lookup("testnet", testID)
	if err != nil {
		t.Fatalf("Lookup with 'testnet' failed: %v", err)
	}

	// Test mainnet (should error as not implemented)
	_, _, err = Lookup("mainnet", testID)
	if err == nil {
		t.Error("Expected error for mainnet lookup")
	}
	t.Logf("Mainnet lookup correctly returns: %v", err)

	// Test unknown network
	_, _, err = Lookup("unknown", testID)
	if err == nil {
		t.Error("Expected error for unknown network")
	}
}
