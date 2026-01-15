package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/cockroachdb/pebble/v2"
)

func getRPCURL() string {
	if url := os.Getenv("RPC_URL"); url != "" {
		return url + "/ext/bc/P"
	}
	return "http://localhost:9650/ext/bc/P"
}

func getChainHeight(ctx context.Context) (uint64, error) {
	rpcURL := getRPCURL()
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "platform.getHeight",
		"params":  map[string]any{},
	}
	jsonData, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Result struct {
			Height string `json:"height"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("unmarshal: %w", err)
	}
	if result.Error != nil {
		return 0, fmt.Errorf("rpc error: %s", result.Error.Message)
	}

	var height uint64
	fmt.Sscanf(result.Result.Height, "%d", &height)
	return height, nil
}

func getBlockByHeight(ctx context.Context, height uint64) ([]byte, error) {
	rpcURL := getRPCURL()
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "platform.getBlockByHeight",
		"params": map[string]any{
			"height":   fmt.Sprintf("%d", height),
			"encoding": "hex",
		},
	}
	jsonData, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Result struct {
			Block    string `json:"block"`
			Encoding string `json:"encoding"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("rpc error: %s", result.Error.Message)
	}

	return formatting.Decode(formatting.Hex, result.Result.Block)
}

func CatchUpBlocks(blocksDir string) error {
	ctx := context.Background()

	db, err := pebble.Open(blocksDir, &pebble.Options{})
	if err != nil {
		return fmt.Errorf("failed to open blocks DB: %w", err)
	}
	defer db.Close()

	// Get local height
	localHeight := uint64(0)
	if val, closer, err := db.Get([]byte("meta:latest")); err == nil {
		localHeight = binary.BigEndian.Uint64(val)
		closer.Close()
	}

	// Get chain height
	chainHeight, err := getChainHeight(ctx)
	if err != nil {
		return fmt.Errorf("failed to get chain height: %w", err)
	}

	if localHeight >= chainHeight {
		fmt.Printf("Blocks up to date: local=%d, chain=%d\n", localHeight, chainHeight)
		return nil
	}

	fmt.Printf("Catching up blocks: local=%d, chain=%d, missing=%d\n", localHeight, chainHeight, chainHeight-localHeight)

	// Fetch in batches of 10
	batchSize := uint64(10)
	for start := localHeight + 1; start <= chainHeight; {
		end := start + batchSize - 1
		if end > chainHeight {
			end = chainHeight
		}

		count := int(end - start + 1)
		blocks := make([][]byte, count)
		var mu sync.Mutex
		var wg sync.WaitGroup
		var fetchErr error

		for i := 0; i < count; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				h := start + uint64(idx)
				blkBytes, err := getBlockByHeight(ctx, h)
				if err != nil {
					mu.Lock()
					if fetchErr == nil {
						fetchErr = err
					}
					mu.Unlock()
					return
				}
				mu.Lock()
				blocks[idx] = blkBytes
				mu.Unlock()
			}(i)
		}
		wg.Wait()

		if fetchErr != nil {
			return fetchErr
		}

		// Store blocks
		batch := db.NewBatch()
		for i, blkBytes := range blocks {
			h := start + uint64(i)
			key := make([]byte, 12)
			copy(key, []byte("blk:"))
			binary.BigEndian.PutUint64(key[4:], h)
			batch.Set(key, blkBytes, pebble.NoSync)
		}

		// Update latest
		latestBuf := make([]byte, 8)
		binary.BigEndian.PutUint64(latestBuf, end)
		batch.Set([]byte("meta:latest"), latestBuf, pebble.NoSync)
		batch.Commit(pebble.NoSync)
		batch.Close()

		fmt.Printf("  Fetched blocks %d -> %d\n", start, end)
		start = end + 1
	}

	return nil
}
