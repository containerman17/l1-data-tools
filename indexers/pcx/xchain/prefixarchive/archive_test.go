package prefixarchive

import (
	"bytes"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

func TestBuildAndLookup(t *testing.T) {
	// Create test data with valid IDs
	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()
	id3 := ids.GenerateTestID()

	data := map[string]int64{
		id1.String(): 1600000000,
		id2.String(): 1600000100,
		id3.String(): 1600000200,
	}

	// Build
	archive, err := Build(data)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if archive.Len() != 3 {
		t.Errorf("Expected 3 entries, got %d", archive.Len())
	}

	// Lookup
	ts, found := archive.Lookup(id1)
	if !found {
		t.Error("Expected to find entry")
	}
	if ts != 1600000000 {
		t.Errorf("Expected timestamp 1600000000, got %d", ts)
	}

	// Lookup non-existent
	fakeID := ids.GenerateTestID()
	_, found = archive.Lookup(fakeID)
	if found {
		t.Error("Should not find non-existent entry")
	}
}

func TestWriteAndRead(t *testing.T) {
	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()
	id3 := ids.GenerateTestID()

	data := map[string]int64{
		id1.String(): 1600000000,
		id2.String(): 1600000100,
		id3.String(): 1600000200,
	}

	// Build
	archive, err := Build(data)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Write
	var buf bytes.Buffer
	n, err := archive.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	t.Logf("Archive size: %d bytes for %d entries", n, archive.Len())

	// Read back
	archive2, err := ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}

	if archive2.Len() != archive.Len() {
		t.Errorf("Entry count mismatch: %d vs %d", archive2.Len(), archive.Len())
	}

	if archive2.PrefixLen() != archive.PrefixLen() {
		t.Errorf("Prefix length mismatch: %d vs %d", archive2.PrefixLen(), archive.PrefixLen())
	}

	// Verify lookups still work
	ts, found := archive2.Lookup(id1)
	if !found || ts != 1600000000 {
		t.Errorf("Lookup after read failed: found=%v, ts=%d", found, ts)
	}
}

func TestEmptyArchive(t *testing.T) {
	archive, err := Build(map[string]int64{})
	if err != nil {
		t.Fatalf("Build empty failed: %v", err)
	}

	var buf bytes.Buffer
	_, err = archive.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	archive2, err := ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}

	if archive2.Len() != 0 {
		t.Errorf("Expected 0 entries, got %d", archive2.Len())
	}
}

func TestPrefixLengthSelection(t *testing.T) {
	// Generate enough entries to force larger prefix
	data := make(map[string]int64)
	for i := 0; i < 1000; i++ {
		id := ids.GenerateTestID()
		data[id.String()] = int64(1600000000 + i)
	}

	archive, err := Build(data)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	t.Logf("1000 entries uses %d-byte prefixes", archive.PrefixLen())

	// Verify all lookups work
	for hashStr, expectedTs := range data {
		id, _ := ids.FromString(hashStr)
		ts, found := archive.Lookup(id)
		if !found {
			t.Errorf("Entry %s not found", hashStr)
			continue
		}
		if ts != expectedTs {
			t.Errorf("Timestamp mismatch for %s: expected %d, got %d", hashStr, expectedTs, ts)
		}
	}
}
