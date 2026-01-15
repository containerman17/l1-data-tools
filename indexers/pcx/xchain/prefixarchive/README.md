# Prefix Archive

Generic compact binary archive format for hash→int64 mappings.

## Features

- **Prefix Truncation**: Uses 3-8 byte hash prefixes instead of full 32-byte hashes
- **Delta Encoding**: Stores value differences for sequential data
- **Collision Detection**: Automatically finds minimum safe prefix length
- **6-7× Compression**: Typical reduction from ~40 bytes/entry to ~6 bytes/entry

## Format

```
[magic: 4 bytes "PXAR"]
[version: 1 byte]
[prefix_len: 1 byte]  // 3-8, chosen automatically
[count: 4 bytes]
[base_value: 8 bytes]
[entries...]: sorted by value
  [prefix: prefix_len bytes]
  [delta: varint]
```

## Usage

```go
import "github.com/ava-labs/devrel-experiments/03_data_api/p-chain-indexer/xchain/prefixarchive"

// Build from data
data := map[string]int64{
    "2Z36RnQuk1hvFmomLV5JxzePwtQBMR7V51GxDDhBNuzR3VKBKS": 1600000000,
    // ... more entries
}
archive, err := prefixarchive.Build(data)

// Write to file
f, _ := os.Create("data.bin")
archive.WriteTo(f)

// Read back
archive2, _ := prefixarchive.Load("data.bin")

// Lookup
id, _ := ids.FromString("2Z36RnQuk1hvFmomLV5JxzePwtQBMR7V51GxDDhBNuzR3VKBKS")
value, found := archive2.Lookup(id)
```

## Compression Example

| Entries | Full Format | Prefix Archive | Ratio |
|---------|-------------|----------------|-------|
| 1M      | 40 MB       | 5.7 MB         | 7.0×  |
| 8.2M    | 328 MB      | ~50 MB         | 6.6×  |

## How It Works

1. **Prefix Selection**: Tries 3→4→5→...→8 byte prefixes until no collisions
2. **Sorting**: Entries sorted by value for optimal delta encoding
3. **Varint Encoding**: Deltas compressed using variable-length integers
4. **Lookup**: O(1) via hash map, using truncated prefix as key

## Limitations

- Maximum 8-byte prefixes (collision detection fails with >2^64 entries)
- Values should be reasonably sequential for good delta compression
- Hash strings must be valid AvalancheGo ID format

