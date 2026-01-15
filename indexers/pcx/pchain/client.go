package pchain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/btcsuite/btcutil/bech32"
	"golang.org/x/sync/singleflight"
)

// Client is a P-Chain RPC client with connection pooling and request deduplication.
type Client struct {
	url        string
	httpClient *http.Client
	hrp        string // bech32 HRP (avax/fuji)

	// Singleflight groups for expensive calls - prevents thundering herd
	sfUTXOs      singleflight.Group
	sfValidators singleflight.Group
}

// NewClient creates a new P-Chain client with connection pooling.
func NewClient(url string) *Client {
	transport := &http.Transport{
		MaxIdleConns:        1000,
		MaxIdleConnsPerHost: 1000,
		MaxConnsPerHost:     1000,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	return &Client{
		url: url,
		hrp: "avax", // default, call SetNetworkID to change
		httpClient: &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
		},
	}
}

// SetNetworkID sets the network-specific HRP for address formatting.
func (c *Client) SetNetworkID(networkID uint32) {
	c.hrp = GetHRP(networkID)
}

// GetHRP returns the bech32 HRP for the given network ID.
func GetHRP(networkID uint32) string {
	switch networkID {
	case 1:
		return "avax"
	case 5:
		return "fuji"
	default:
		return "custom"
	}
}

// GetHeight returns the current P-Chain height.
func (c *Client) GetHeight(ctx context.Context) (uint64, error) {
	var result struct {
		Height json.Number `json:"height"`
	}
	if err := c.rpcCall(ctx, "platform.getHeight", struct{}{}, &result); err != nil {
		return 0, err
	}
	h, err := result.Height.Int64()
	return uint64(h), err
}

// GetNetworkID returns the network ID (1 for mainnet, 5 for fuji).
func (c *Client) GetNetworkID(ctx context.Context) (uint32, error) {
	var result struct {
		NetworkID json.Number `json:"networkID"`
	}
	if err := c.infoRpcCall(ctx, "info.getNetworkID", struct{}{}, &result); err != nil {
		return 0, err
	}
	id, err := result.NetworkID.Int64()
	return uint32(id), err
}

// GetBlockByHeight returns raw block bytes for a given height.
func (c *Client) GetBlockByHeight(ctx context.Context, height uint64) ([]byte, error) {
	var result struct {
		Block    string              `json:"block"`
		Encoding formatting.Encoding `json:"encoding"`
	}

	params := map[string]any{
		"height":   height,
		"encoding": "hexnc",
	}

	if err := c.rpcCall(ctx, "platform.getBlockByHeight", params, &result); err != nil {
		return nil, err
	}

	return formatting.Decode(result.Encoding, result.Block)
}

// GetCurrentValidators returns current validators, optionally filtered by nodeIDs.
// Uses singleflight to deduplicate concurrent requests for same nodeIDs.
func (c *Client) GetCurrentValidators(ctx context.Context, nodeIDs []string) (json.RawMessage, error) {
	// Build cache key from sorted nodeIDs
	key := "all"
	if len(nodeIDs) > 0 {
		sorted := make([]string, len(nodeIDs))
		copy(sorted, nodeIDs)
		sort.Strings(sorted)
		key = strings.Join(sorted, ",")
	}

	result, err, _ := c.sfValidators.Do(key, func() (any, error) {
		params := map[string]any{}
		if len(nodeIDs) > 0 {
			params["nodeIDs"] = nodeIDs
		}

		var result json.RawMessage
		if err := c.rpcCall(ctx, "platform.getCurrentValidators", params, &result); err != nil {
			return nil, err
		}
		return result, nil
	})

	if err != nil {
		return nil, err
	}
	return result.(json.RawMessage), nil
}

// GetPendingValidators returns pending validators, optionally filtered by nodeIDs.
func (c *Client) GetPendingValidators(ctx context.Context, nodeIDs []string) (json.RawMessage, error) {
	params := map[string]any{}
	if len(nodeIDs) > 0 {
		params["nodeIDs"] = nodeIDs
	}

	var result json.RawMessage
	if err := c.rpcCall(ctx, "platform.getPendingValidators", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetCurrentSupply returns the current supply of AVAX.
func (c *Client) GetCurrentSupply(ctx context.Context) (uint64, error) {
	var result struct {
		Supply json.Number `json:"supply"`
	}
	if err := c.rpcCall(ctx, "platform.getCurrentSupply", struct{}{}, &result); err != nil {
		return 0, err
	}
	s, err := result.Supply.Int64()
	return uint64(s), err
}

// GetPeers returns the list of connected peers.
func (c *Client) GetPeers(ctx context.Context) (json.RawMessage, error) {
	var result json.RawMessage
	if err := c.infoRpcCall(ctx, "info.peers", struct{}{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetStake returns staked UTXOs for the given addresses.
func (c *Client) GetStake(ctx context.Context, addrs []ids.ShortID) ([][]byte, error) {
	// Convert addresses to bech32 format
	addrStrs := make([]string, len(addrs))
	for i, addr := range addrs {
		addrStrs[i] = "P-" + formatBech32(addr, c.hrp)
	}

	var result struct {
		Staked        string   `json:"staked"`
		StakedOutputs []string `json:"stakedOutputs"`
		Encoding      string   `json:"encoding"`
	}

	params := map[string]any{
		"addresses": addrStrs,
		"encoding":  "hex",
	}

	if err := c.rpcCall(ctx, "platform.getStake", params, &result); err != nil {
		return nil, err
	}

	utxos := make([][]byte, 0, len(result.StakedOutputs))
	for _, utxoHex := range result.StakedOutputs {
		utxoBytes, err := formatting.Decode(formatting.Hex, utxoHex)
		if err != nil {
			continue
		}
		utxos = append(utxos, utxoBytes)
	}

	return utxos, nil
}

// GetTx returns raw transaction bytes for a given txID.
func (c *Client) GetTx(ctx context.Context, txID string) ([]byte, error) {
	var result struct {
		Tx       string              `json:"tx"`
		Encoding formatting.Encoding `json:"encoding"`
	}

	params := map[string]any{
		"txID":     txID,
		"encoding": "hexnc",
	}

	if err := c.rpcCall(ctx, "platform.getTx", params, &result); err != nil {
		return nil, err
	}

	return formatting.Decode(result.Encoding, result.Tx)
}

// GetUTXOs returns UTXOs for given addresses.
// Uses singleflight to deduplicate concurrent requests for same addresses.
func (c *Client) GetUTXOs(ctx context.Context, addrs []ids.ShortID) ([][]byte, error) {
	// Build cache key from sorted addresses
	addrStrs := make([]string, len(addrs))
	for i, addr := range addrs {
		addrStrs[i] = addr.String()
	}
	sort.Strings(addrStrs)
	key := strings.Join(addrStrs, ",")

	result, err, _ := c.sfUTXOs.Do(key, func() (any, error) {
		return c.getUTXOsInternal(ctx, addrs)
	})

	if err != nil {
		return nil, err
	}
	return result.([][]byte), nil
}

func (c *Client) getUTXOsInternal(ctx context.Context, addrs []ids.ShortID) ([][]byte, error) {
	// Convert addresses to bech32 format (P-avax1... or P-fuji1...)
	addrStrs := make([]string, len(addrs))
	for i, addr := range addrs {
		addrStrs[i] = "P-" + formatBech32(addr, c.hrp)
	}

	var allUTXOs [][]byte
	var startAddr, startUTXO string

	for {
		var result struct {
			UTXOs    []string `json:"utxos"`
			EndIndex struct {
				Address string `json:"address"`
				UTXO    string `json:"utxo"`
			} `json:"endIndex"`
			Encoding formatting.Encoding `json:"encoding"`
		}

		params := map[string]any{
			"addresses": addrStrs,
			"limit":     1024,
			"encoding":  "hexnc",
		}
		if startAddr != "" {
			params["startIndex"] = map[string]string{
				"address": startAddr,
				"utxo":    startUTXO,
			}
		}

		if err := c.rpcCall(ctx, "platform.getUTXOs", params, &result); err != nil {
			return nil, err
		}

		for _, utxoHex := range result.UTXOs {
			utxoBytes, err := formatting.Decode(result.Encoding, utxoHex)
			if err != nil {
				continue
			}
			allUTXOs = append(allUTXOs, utxoBytes)
		}

		if len(result.UTXOs) < 1024 {
			break
		}
		startAddr = result.EndIndex.Address
		startUTXO = result.EndIndex.UTXO
	}

	return allUTXOs, nil
}

// GetAtomicUTXOs returns UTXOs in shared memory (exported from P-Chain waiting to be imported elsewhere).
// Queries both X-Chain and C-Chain for UTXOs exported from P-Chain.
// Falls back to public API if local node doesn't support X/C chains.
func (c *Client) GetAtomicUTXOs(ctx context.Context, addrs []ids.ShortID) ([][]byte, string, error) {
	// Convert addresses to bech32 format (using network-aware HRP)
	xAddrs := make([]string, len(addrs))
	cAddrs := make([]string, len(addrs))
	for i, addr := range addrs {
		bech := formatBech32(addr, c.hrp)
		xAddrs[i] = "X-" + bech
		cAddrs[i] = "C-" + bech
	}

	var allUTXOs [][]byte
	var sharedChainID string

	// Determine URL to use (local or public fallback)
	baseURL := c.url
	usePublic := false
	if strings.Contains(c.url, "localhost") || strings.Contains(c.url, "127.0.0.1") {
		// Local node likely doesn't have X/C chains, use public API
		if c.hrp == "fuji" {
			baseURL = "https://api.avax-test.network"
		} else {
			baseURL = "https://api.avax.network"
		}
		usePublic = true
	}

	// Query X-Chain for UTXOs exported from P-Chain
	xUTXOs, err := c.getAtomicUTXOsFromChainWithURL(ctx, baseURL, "/ext/bc/X", "avm.getUTXOs", xAddrs)
	if err == nil && len(xUTXOs) > 0 {
		allUTXOs = append(allUTXOs, xUTXOs...)
		if c.hrp == "fuji" {
			sharedChainID = "2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm"
		} else {
			sharedChainID = "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM"
		}
	} else if usePublic && err != nil {
		// If public API also fails, silently continue
	}

	// Query C-Chain for UTXOs exported from P-Chain
	cUTXOs, err := c.getAtomicUTXOsFromChainWithURL(ctx, baseURL, "/ext/bc/C/avax", "avax.getUTXOs", cAddrs)
	if err == nil && len(cUTXOs) > 0 {
		allUTXOs = append(allUTXOs, cUTXOs...)
		if c.hrp == "fuji" {
			sharedChainID = "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp"
		} else {
			sharedChainID = "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5"
		}
	}

	return allUTXOs, sharedChainID, nil
}

func (c *Client) getAtomicUTXOsFromChain(ctx context.Context, endpoint, method string, addrs []string) ([][]byte, error) {
	return c.getAtomicUTXOsFromChainWithURL(ctx, c.url, endpoint, method, addrs)
}

func (c *Client) getAtomicUTXOsFromChainWithURL(ctx context.Context, baseURL, endpoint, method string, addrs []string) ([][]byte, error) {
	var allUTXOs [][]byte
	var startAddr, startUTXO string

	for {
		var result struct {
			UTXOs    []string `json:"utxos"`
			EndIndex struct {
				Address string `json:"address"`
				UTXO    string `json:"utxo"`
			} `json:"endIndex"`
			Encoding formatting.Encoding `json:"encoding"`
		}

		params := map[string]any{
			"addresses":   addrs,
			"sourceChain": "P",
			"limit":       1024,
			"encoding":    "hexnc",
		}
		if startAddr != "" {
			params["startIndex"] = map[string]string{
				"address": startAddr,
				"utxo":    startUTXO,
			}
		}

		if err := c.rpcCallURL(ctx, baseURL, endpoint, method, params, &result); err != nil {
			return nil, err
		}

		for _, utxoHex := range result.UTXOs {
			utxoBytes, err := formatting.Decode(result.Encoding, utxoHex)
			if err != nil {
				continue
			}
			allUTXOs = append(allUTXOs, utxoBytes)
		}

		if len(result.UTXOs) < 1024 {
			break
		}
		startAddr = result.EndIndex.Address
		startUTXO = result.EndIndex.UTXO
	}

	return allUTXOs, nil
}

// rpcCallEndpoint makes a JSON-RPC call to a specific endpoint.
func (c *Client) rpcCallEndpoint(ctx context.Context, endpoint, method string, params any, result any) error {
	return c.rpcCallURL(ctx, c.url, endpoint, method, params, result)
}

// rpcCallURL makes a JSON-RPC call to an arbitrary URL.
func (c *Client) rpcCallURL(ctx context.Context, baseURL, endpoint, method string, params any, result any) error {
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	var rpcResp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(bodyBytes, &rpcResp); err != nil {
		return fmt.Errorf("decode response: %w (body: %s)", err, string(bodyBytes[:min(500, len(bodyBytes))]))
	}

	if rpcResp.Error != nil {
		return fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if result != nil {
		if err := json.Unmarshal(rpcResp.Result, result); err != nil {
			return fmt.Errorf("unmarshal result: %w", err)
		}
	}

	return nil
}

// rpcCall makes a JSON-RPC call to P-Chain.
func (c *Client) rpcCall(ctx context.Context, method string, params any, result any) error {
	return c.rpcCallEndpoint(ctx, "/ext/bc/P", method, params, result)
}

func (c *Client) infoRpcCall(ctx context.Context, method string, params any, result any) error {
	return c.rpcCallEndpoint(ctx, "/ext/info", method, params, result)
}

// rpcCallChain makes a JSON-RPC call to a specific chain's avax endpoint.
// For C-Chain, uses /ext/bc/C/avax
func (c *Client) rpcCallChain(ctx context.Context, chainID, method string, params any, result any) error {
	// C-Chain IDs
	cChainMainnet := "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5"
	cChainFuji := "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp"

	var endpoint string
	if chainID == cChainMainnet || chainID == cChainFuji {
		endpoint = "/ext/bc/C/avax"
	} else {
		// X-Chain or unknown
		endpoint = "/ext/bc/X"
	}

	// Use public API if local node
	baseURL := c.url
	if strings.Contains(c.url, "localhost") || strings.Contains(c.url, "127.0.0.1") {
		if c.hrp == "fuji" {
			baseURL = "https://api.avax-test.network"
		} else {
			baseURL = "https://api.avax.network"
		}
	}

	return c.rpcCallURL(ctx, baseURL, endpoint, method, params, result)
}

// rpcCallChainEVM makes a JSON-RPC call to a chain's EVM RPC endpoint.
// For C-Chain, uses /ext/bc/C/rpc
func (c *Client) rpcCallChainEVM(ctx context.Context, chainID, method string, params any, result any) error {
	// C-Chain IDs
	cChainMainnet := "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5"
	cChainFuji := "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp"

	if chainID != cChainMainnet && chainID != cChainFuji {
		return fmt.Errorf("EVM RPC only supported for C-Chain, got chain %s", chainID)
	}

	endpoint := "/ext/bc/C/rpc"

	// Use public API if local node
	baseURL := c.url
	if strings.Contains(c.url, "localhost") || strings.Contains(c.url, "127.0.0.1") {
		if c.hrp == "fuji" {
			baseURL = "https://api.avax-test.network"
		} else {
			baseURL = "https://api.avax.network"
		}
	}

	return c.rpcCallURL(ctx, baseURL, endpoint, method, params, result)
}

// GetRewardUTXOs returns reward UTXOs for a completed staking tx.
func (c *Client) GetRewardUTXOs(ctx context.Context, stakingTxID string) ([][]byte, error) {
	var result struct {
		UTXOs    []string            `json:"utxos"`
		Encoding formatting.Encoding `json:"encoding"`
	}

	params := map[string]any{
		"txID":     stakingTxID,
		"encoding": "hexnc",
	}

	if err := c.rpcCall(ctx, "platform.getRewardUTXOs", params, &result); err != nil {
		return nil, err
	}

	utxos := make([][]byte, len(result.UTXOs))
	for i, utxoHex := range result.UTXOs {
		utxoBytes, err := formatting.Decode(result.Encoding, utxoHex)
		if err != nil {
			return nil, fmt.Errorf("decode UTXO %d: %w", i, err)
		}
		utxos[i] = utxoBytes
	}

	return utxos, nil
}

// formatBech32 encodes a short ID to bech32 format (e.g., avax1...)
func formatBech32(addr ids.ShortID, hrp string) string {
	// Convert 8-bit to 5-bit
	conv, err := bech32.ConvertBits(addr[:], 8, 5, true)
	if err != nil {
		return addr.String() // fallback to base58
	}
	encoded, err := bech32.Encode(hrp, conv)
	if err != nil {
		return addr.String() // fallback to base58
	}
	return encoded
}
