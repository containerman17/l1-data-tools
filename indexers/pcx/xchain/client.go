package xchain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/ava-labs/avalanchego/utils/formatting"
)

// Client is an X-Chain RPC client with connection pooling.
type Client struct {
	url        string
	httpClient *http.Client
}

// NewClient creates a new X-Chain client with connection pooling.
func NewClient(url string) *Client {
	transport := &http.Transport{
		MaxIdleConns:        1000,
		MaxIdleConnsPerHost: 1000,
		MaxConnsPerHost:     1000,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	return &Client{
		url: url,
		httpClient: &http.Client{
			Timeout:   2 * time.Minute,
			Transport: transport,
		},
	}
}

// GetHeight returns the current X-Chain height.
func (c *Client) GetHeight(ctx context.Context) (uint64, error) {
	var result struct {
		Height json.Number `json:"height"`
	}
	if err := c.rpcCall(ctx, "avm.getHeight", struct{}{}, &result); err != nil {
		return 0, err
	}
	h, err := result.Height.Int64()
	return uint64(h), err
}

// GetBlockByHeight returns raw block bytes for a given height.
func (c *Client) GetBlockByHeight(ctx context.Context, height uint64) ([]byte, error) {
	var result struct {
		Block    string              `json:"block"`
		Encoding formatting.Encoding `json:"encoding"`
	}

	params := map[string]any{
		"height":   fmt.Sprintf("%d", height), // X-Chain expects string
		"encoding": "hex",
	}

	if err := c.rpcCall(ctx, "avm.getBlockByHeight", params, &result); err != nil {
		return nil, err
	}

	return formatting.Decode(result.Encoding, result.Block)
}

// IndexedTx represents a transaction from the Index API.
type IndexedTx struct {
	ID        string `json:"id"`
	Bytes     []byte // decoded from hex
	Timestamp int64  // unix timestamp
	Index     uint64
}

// GetLastAcceptedIndex returns the index of the last accepted transaction.
// Requires --index-enabled=true on the node.
func (c *Client) GetLastAcceptedIndex(ctx context.Context) (uint64, error) {
	var result struct {
		Index string `json:"index"`
	}

	params := map[string]any{
		"encoding": "hex",
	}

	if err := c.indexRpcCall(ctx, "index.getLastAccepted", params, &result); err != nil {
		return 0, err
	}

	idx, err := strconv.ParseUint(result.Index, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse index: %w", err)
	}
	return idx, nil
}

// GetContainerRange returns a batch of transactions from the Index API.
// Max numToFetch is 1024.
func (c *Client) GetContainerRange(ctx context.Context, startIndex uint64, numToFetch int) ([]IndexedTx, error) {
	var result struct {
		Containers []struct {
			ID        string `json:"id"`
			Bytes     string `json:"bytes"`
			Timestamp string `json:"timestamp"`
			Index     string `json:"index"`
		} `json:"containers"`
	}

	params := map[string]any{
		"startIndex": startIndex,
		"numToFetch": numToFetch,
		"encoding":   "hex",
	}

	if err := c.indexRpcCall(ctx, "index.getContainerRange", params, &result); err != nil {
		return nil, err
	}

	txs := make([]IndexedTx, len(result.Containers))
	for i, c := range result.Containers {
		decoded, err := formatting.Decode(formatting.Hex, c.Bytes)
		if err != nil {
			return nil, fmt.Errorf("decode tx bytes: %w", err)
		}

		ts, err := time.Parse(time.RFC3339Nano, c.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("parse timestamp: %w", err)
		}

		idx, err := strconv.ParseUint(c.Index, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse index: %w", err)
		}

		txs[i] = IndexedTx{
			ID:        c.ID,
			Bytes:     decoded,
			Timestamp: ts.Unix(),
			Index:     idx,
		}
	}

	return txs, nil
}

func (c *Client) rpcCall(ctx context.Context, method string, params any, result any) error {
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return err
	}

	// X-Chain endpoint
	endpoint := c.url + "/ext/bc/X"

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return fmt.Errorf("unmarshal response: %w (body: %s)", err, string(respBody))
	}

	if rpcResp.Error != nil {
		return fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return json.Unmarshal(rpcResp.Result, result)
}

// indexRpcCall makes an RPC call to the Index API endpoint (/ext/index/X/tx).
func (c *Client) indexRpcCall(ctx context.Context, method string, params any, result any) error {
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return err
	}

	// Index API endpoint for X-Chain transactions
	endpoint := c.url + "/ext/index/X/tx"

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return fmt.Errorf("unmarshal response: %w (body: %s)", err, string(respBody))
	}

	if rpcResp.Error != nil {
		return fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return json.Unmarshal(rpcResp.Result, result)
}
