// Package prefixarchive provides a compact binary archive format for hash→int64 mappings.
// Uses truncated hash prefixes and delta-encoded timestamps to minimize storage.
package prefixarchive

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/ava-labs/avalanchego/ids"
)

const (
	magic        = "PXAR" // Prefix Archive
	version      = 1
	maxPrefixLen = 8
	minPrefixLen = 3
)

// Archive is an in-memory lookup table using truncated hash prefixes.
type Archive struct {
	prefixLen int
	entries   map[uint64]int64
}

// Lookup returns the value for a given ID.
// Returns 0, false if not found.
func (a *Archive) Lookup(id ids.ID) (int64, bool) {
	key := prefixToKey(id[:], a.prefixLen)
	val, ok := a.entries[key]
	return val, ok
}

// Len returns the number of entries in the archive.
func (a *Archive) Len() int {
	return len(a.entries)
}

// PrefixLen returns the prefix length used by this archive.
func (a *Archive) PrefixLen() int {
	return a.prefixLen
}

// entry is used during building/encoding
type entry struct {
	prefix uint64
	value  int64
}

// Build creates an archive from a map of hash strings to int64 values.
// Automatically finds the minimum prefix length (3-8 bytes) with no collisions.
// Returns error if collisions exist even with 8-byte prefixes.
func Build(data map[string]int64) (*Archive, error) {
	if len(data) == 0 {
		return &Archive{prefixLen: minPrefixLen, entries: make(map[uint64]int64)}, nil
	}

	// Parse all hashes
	entries := make([]entry, 0, len(data))
	for hashStr, val := range data {
		id, err := ids.FromString(hashStr)
		if err != nil {
			return nil, fmt.Errorf("invalid hash %s: %w", hashStr, err)
		}
		entries = append(entries, entry{
			prefix: binary.BigEndian.Uint64(id[:8]), // store full 8 bytes, truncate later
			value:  val,
		})
	}

	// Try increasing prefix lengths until no collisions
	for prefixLen := minPrefixLen; prefixLen <= maxPrefixLen; prefixLen++ {
		mask := prefixMask(prefixLen)
		seen := make(map[uint64]bool, len(entries))
		collision := false

		for _, e := range entries {
			key := e.prefix & mask
			if seen[key] {
				collision = true
				break
			}
			seen[key] = true
		}

		if !collision {
			// Build final map
			result := make(map[uint64]int64, len(entries))
			for _, e := range entries {
				result[e.prefix&mask] = e.value
			}
			return &Archive{prefixLen: prefixLen, entries: result}, nil
		}
	}

	return nil, fmt.Errorf("collisions exist even with %d-byte prefixes for %d entries", maxPrefixLen, len(entries))
}

// WriteTo writes the archive in compact binary format.
// Returns the number of bytes written.
// Format:
//
//	[magic: 4 bytes]
//	[version: 1 byte]
//	[prefix_len: 1 byte]
//	[count: 4 bytes]
//	[base_value: 8 bytes]
//	[entries...]: sorted by value for delta encoding
//	  [prefix: prefix_len bytes]
//	  [delta: varint]
func (a *Archive) WriteTo(w io.Writer) (int64, error) {
	var written int64

	// Header
	n, err := w.Write([]byte(magic))
	written += int64(n)
	if err != nil {
		return written, err
	}

	n, err = w.Write([]byte{version, byte(a.prefixLen)})
	written += int64(n)
	if err != nil {
		return written, err
	}

	if err := binary.Write(w, binary.BigEndian, uint32(len(a.entries))); err != nil {
		return written, err
	}
	written += 4

	if len(a.entries) == 0 {
		return written, nil
	}

	// Convert to sorted slice for delta encoding
	entries := make([]entry, 0, len(a.entries))
	for prefix, val := range a.entries {
		entries = append(entries, entry{prefix: prefix, value: val})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].value < entries[j].value
	})

	// Base value
	if err := binary.Write(w, binary.BigEndian, entries[0].value); err != nil {
		return written, err
	}
	written += 8

	// Entries: prefix + delta
	prevVal := entries[0].value
	buf := make([]byte, a.prefixLen+binary.MaxVarintLen64)
	for _, e := range entries {
		// Write prefix (big-endian, truncated)
		for i := 0; i < a.prefixLen; i++ {
			buf[i] = byte(e.prefix >> (8 * (7 - i)))
		}
		// Write delta as varint
		delta := e.value - prevVal
		n := binary.PutVarint(buf[a.prefixLen:], delta)
		nw, err := w.Write(buf[:a.prefixLen+n])
		written += int64(nw)
		if err != nil {
			return written, err
		}
		prevVal = e.value
	}

	return written, nil
}

// ReadFrom reads an archive from compact binary format.
func ReadFrom(r io.Reader) (*Archive, error) {
	// Read magic
	magicBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, magicBuf); err != nil {
		return nil, fmt.Errorf("reading magic: %w", err)
	}
	if string(magicBuf) != magic {
		return nil, fmt.Errorf("invalid magic: %s", magicBuf)
	}

	// Read version and prefix length
	header := make([]byte, 2)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	if header[0] != version {
		return nil, fmt.Errorf("unsupported version: %d", header[0])
	}
	prefixLen := int(header[1])
	if prefixLen < minPrefixLen || prefixLen > maxPrefixLen {
		return nil, fmt.Errorf("invalid prefix length: %d", prefixLen)
	}

	// Read count
	var count uint32
	if err := binary.Read(r, binary.BigEndian, &count); err != nil {
		return nil, fmt.Errorf("reading count: %w", err)
	}

	if count == 0 {
		return &Archive{prefixLen: prefixLen, entries: make(map[uint64]int64)}, nil
	}

	// Read base value
	var baseVal int64
	if err := binary.Read(r, binary.BigEndian, &baseVal); err != nil {
		return nil, fmt.Errorf("reading base value: %w", err)
	}

	// Read entries
	entries := make(map[uint64]int64, count)
	prevVal := baseVal
	prefixBuf := make([]byte, prefixLen)
	br := newVarintReader(r)

	for i := uint32(0); i < count; i++ {
		// Read prefix
		if _, err := io.ReadFull(r, prefixBuf); err != nil {
			return nil, fmt.Errorf("reading prefix %d: %w", i, err)
		}
		// Build key in high bits (same as Build does)
		var key uint64
		for j := 0; j < prefixLen; j++ {
			key = (key << 8) | uint64(prefixBuf[j])
		}
		key = key << (8 * (8 - prefixLen)) // Shift to high bits

		// Read delta
		delta, err := br.ReadVarint()
		if err != nil {
			return nil, fmt.Errorf("reading delta %d: %w", i, err)
		}

		val := prevVal + delta
		entries[key] = val
		prevVal = val
	}

	return &Archive{prefixLen: prefixLen, entries: entries}, nil
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

// prefixToKey extracts the first n bytes of data as a uint64 key (in high bits).
func prefixToKey(data []byte, n int) uint64 {
	return binary.BigEndian.Uint64(data[:8]) & prefixMask(n)
}

// prefixMask returns a mask for the top n bytes of a uint64.
func prefixMask(n int) uint64 {
	return ^uint64(0) << (8 * (8 - n))
}

// varintReader wraps an io.Reader for reading varints.
type varintReader struct {
	r   io.Reader
	buf [1]byte
}

func newVarintReader(r io.Reader) *varintReader {
	return &varintReader{r: r}
}

func (vr *varintReader) ReadVarint() (int64, error) {
	var x uint64
	var s uint
	for i := 0; i < binary.MaxVarintLen64; i++ {
		if _, err := vr.r.Read(vr.buf[:]); err != nil {
			return 0, err
		}
		b := vr.buf[0]
		if b < 0x80 {
			if i == binary.MaxVarintLen64-1 && b > 1 {
				return 0, fmt.Errorf("varint overflow")
			}
			x |= uint64(b) << s
			// Decode zigzag
			return int64(x>>1) ^ -int64(x&1), nil
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
	return 0, fmt.Errorf("varint too long")
}
