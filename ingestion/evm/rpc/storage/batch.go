package storage

import (
	"bufio"
	"bytes"
	"fmt"

	"github.com/klauspost/compress/zstd"
)

// BatchStart returns the first block of the batch containing blockNum
// Batches are 1-based: 1-100, 101-200, etc.
func BatchStart(blockNum uint64) uint64 {
	if blockNum == 0 {
		return 1
	}
	return ((blockNum-1)/BatchSize)*BatchSize + 1
}

// BatchEnd returns the last block of the batch starting at batchStart
func BatchEnd(batchStart uint64) uint64 {
	return batchStart + BatchSize - 1
}

// CompressBlocks compresses blocks into JSONL+zstd format
func CompressBlocks(blocks [][]byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd writer: %w", err)
	}

	for _, block := range blocks {
		if _, err := zw.Write(block); err != nil {
			zw.Close()
			return nil, fmt.Errorf("failed to write block: %w", err)
		}
		if _, err := zw.Write([]byte{'\n'}); err != nil {
			zw.Close()
			return nil, fmt.Errorf("failed to write newline: %w", err)
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close zstd writer: %w", err)
	}

	return buf.Bytes(), nil
}

// DecompressBlocks decompresses JSONL+zstd data into individual block JSONs
func DecompressBlocks(data []byte) ([][]byte, error) {
	zr, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd reader: %w", err)
	}
	defer zr.Close()

	var blocks [][]byte
	scanner := bufio.NewScanner(zr)
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024) // 10MB max line

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		block := make([]byte, len(line))
		copy(block, line)
		blocks = append(blocks, block)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan JSONL: %w", err)
	}

	return blocks, nil
}
