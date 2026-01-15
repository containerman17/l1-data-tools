package cchain

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client is an RPC client for C-Chain.
type Client struct {
	url    string
	client *http.Client
}

// NewClient creates a new C-Chain RPC client.
func NewClient(baseURL string) *Client {
	return &Client{
		url:    strings.TrimSuffix(baseURL, "/") + "/ext/bc/C/rpc",
		client: &http.Client{},
	}
}

type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

type rpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type blockResult struct {
	Number         string `json:"number"`
	Timestamp      string `json:"timestamp"`
	Hash           string `json:"hash"`
	ParentHash     string `json:"parentHash"`
	Size           string `json:"size"`
	GasLimit       string `json:"gasLimit"`
	GasUsed        string `json:"gasUsed"`
	BaseFeePerGas  string `json:"baseFeePerGas"`
	Miner          string `json:"miner"`
	Transactions   []any  `json:"transactions"`
	BlockExtraData string `json:"blockExtraData"`
	ExtDataHash    string `json:"extDataHash"`
}

// BlockData contains the data we store for a C-Chain block.
// Uses custom binary encoding for fast serialization (~100x faster than JSON).
type BlockData struct {
	Height        uint64
	Timestamp     int64
	Hash          string // 0x-prefixed hex, 32 bytes
	ParentHash    string // 0x-prefixed hex, 32 bytes
	Size          int
	TxCount       int
	GasLimit      uint64
	GasUsed       uint64
	BaseFeePerGas uint64
	Miner         string // 0x-prefixed hex, 20 bytes
	ExtDataHash   string // 0x-prefixed hex, 32 bytes
	ExtraData     []byte // blockExtraData (atomic txs)
}

// Binary layout (168 bytes fixed + variable ExtraData):
// [0:8]     Height (uint64 BE)
// [8:16]    Timestamp (int64 BE)
// [16:48]   Hash (32 bytes raw)
// [48:80]   ParentHash (32 bytes raw)
// [80:84]   Size (uint32 BE)
// [84:88]   TxCount (uint32 BE)
// [88:96]   GasLimit (uint64 BE)
// [96:104]  GasUsed (uint64 BE)
// [104:112] BaseFeePerGas (uint64 BE)
// [112:132] Miner (20 bytes raw)
// [132:164] ExtDataHash (32 bytes raw)
// [164:168] ExtraDataLen (uint32 BE)
// [168:...]  ExtraData (variable)

const blockDataFixedSize = 168

// Encode serializes BlockData to binary format.
func (b *BlockData) Encode() []byte {
	buf := make([]byte, blockDataFixedSize+len(b.ExtraData))

	binary.BigEndian.PutUint64(buf[0:8], b.Height)
	binary.BigEndian.PutUint64(buf[8:16], uint64(b.Timestamp))
	decodeHexInto(b.Hash, buf[16:48])
	decodeHexInto(b.ParentHash, buf[48:80])
	binary.BigEndian.PutUint32(buf[80:84], uint32(b.Size))
	binary.BigEndian.PutUint32(buf[84:88], uint32(b.TxCount))
	binary.BigEndian.PutUint64(buf[88:96], b.GasLimit)
	binary.BigEndian.PutUint64(buf[96:104], b.GasUsed)
	binary.BigEndian.PutUint64(buf[104:112], b.BaseFeePerGas)
	decodeHexInto(b.Miner, buf[112:132])
	decodeHexInto(b.ExtDataHash, buf[132:164])
	binary.BigEndian.PutUint32(buf[164:168], uint32(len(b.ExtraData)))
	copy(buf[168:], b.ExtraData)

	return buf
}

// Decode deserializes BlockData from binary format.
func (b *BlockData) Decode(buf []byte) error {
	if len(buf) < blockDataFixedSize {
		return fmt.Errorf("buffer too small: %d < %d", len(buf), blockDataFixedSize)
	}

	b.Height = binary.BigEndian.Uint64(buf[0:8])
	b.Timestamp = int64(binary.BigEndian.Uint64(buf[8:16]))
	b.Hash = "0x" + hex.EncodeToString(buf[16:48])
	b.ParentHash = "0x" + hex.EncodeToString(buf[48:80])
	b.Size = int(binary.BigEndian.Uint32(buf[80:84]))
	b.TxCount = int(binary.BigEndian.Uint32(buf[84:88]))
	b.GasLimit = binary.BigEndian.Uint64(buf[88:96])
	b.GasUsed = binary.BigEndian.Uint64(buf[96:104])
	b.BaseFeePerGas = binary.BigEndian.Uint64(buf[104:112])
	b.Miner = "0x" + hex.EncodeToString(buf[112:132])
	b.ExtDataHash = "0x" + hex.EncodeToString(buf[132:164])

	extraLen := binary.BigEndian.Uint32(buf[164:168])
	if extraLen > 0 {
		if len(buf) < int(blockDataFixedSize+extraLen) {
			return fmt.Errorf("buffer too small for extraData")
		}
		b.ExtraData = make([]byte, extraLen)
		copy(b.ExtraData, buf[168:168+extraLen])
	}

	return nil
}

// decodeHexInto decodes hex string (with or without 0x) into dst.
func decodeHexInto(s string, dst []byte) {
	s = strings.TrimPrefix(s, "0x")
	if len(s) > 0 {
		hex.Decode(dst, []byte(s))
	}
}

func (c *Client) call(ctx context.Context, method string, params []interface{}) (json.RawMessage, error) {
	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}
	body, _ := json.Marshal(req)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, err
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// GetHeight returns the latest block number.
func (c *Client) GetHeight(ctx context.Context) (uint64, error) {
	result, err := c.call(ctx, "eth_blockNumber", []interface{}{})
	if err != nil {
		return 0, err
	}

	var hexNum string
	if err := json.Unmarshal(result, &hexNum); err != nil {
		return 0, err
	}

	return parseHexUint64(hexNum)
}

// GetBlocksBatch fetches block data for a range of heights in a single batch request.
// Returns a slice of BlockData in order (one per height from start to end inclusive).
func (c *Client) GetBlocksBatch(ctx context.Context, start, end uint64) ([]BlockData, error) {
	count := int(end - start + 1)

	// Build batch request
	requests := make([]rpcRequest, count)
	for i := 0; i < count; i++ {
		height := start + uint64(i)
		requests[i] = rpcRequest{
			JSONRPC: "2.0",
			Method:  "eth_getBlockByNumber",
			Params:  []interface{}{fmt.Sprintf("0x%x", height), false},
			ID:      i,
		}
	}

	body, err := json.Marshal(requests)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var responses []rpcResponse
	if err := json.Unmarshal(respBody, &responses); err != nil {
		return nil, fmt.Errorf("unmarshal batch response: %w", err)
	}

	if len(responses) != count {
		return nil, fmt.Errorf("expected %d responses, got %d", count, len(responses))
	}

	// Results are returned in order by ID
	results := make([]BlockData, count)
	for _, r := range responses {
		if r.Error != nil {
			return nil, fmt.Errorf("rpc error for block %d: %s", start+uint64(r.ID), r.Error.Message)
		}

		var block blockResult
		if err := json.Unmarshal(r.Result, &block); err != nil {
			return nil, fmt.Errorf("unmarshal block %d: %w", start+uint64(r.ID), err)
		}

		height := start + uint64(r.ID)
		ts, _ := parseHexUint64(block.Timestamp)
		size, _ := parseHexUint64(block.Size)
		gasLimit, _ := parseHexUint64(block.GasLimit)
		gasUsed, _ := parseHexUint64(block.GasUsed)
		baseFee, _ := parseHexUint64(block.BaseFeePerGas)
		extraData, err := decodeHex(block.BlockExtraData)
		if err != nil {
			return nil, fmt.Errorf("decode blockExtraData for %d: %w", height, err)
		}

		results[r.ID] = BlockData{
			Height:        height,
			Timestamp:     int64(ts),
			Hash:          block.Hash,
			ParentHash:    block.ParentHash,
			Size:          int(size),
			TxCount:       len(block.Transactions),
			GasLimit:      gasLimit,
			GasUsed:       gasUsed,
			BaseFeePerGas: baseFee,
			Miner:         block.Miner,
			ExtraData:     extraData,
			ExtDataHash:   block.ExtDataHash,
		}
	}

	return results, nil
}

func parseHexUint64(s string) (uint64, error) {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return 0, nil
	}
	var n uint64
	for _, c := range s {
		n *= 16
		switch {
		case c >= '0' && c <= '9':
			n += uint64(c - '0')
		case c >= 'a' && c <= 'f':
			n += uint64(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			n += uint64(c - 'A' + 10)
		default:
			return 0, fmt.Errorf("invalid hex char: %c", c)
		}
	}
	return n, nil
}

func decodeHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return []byte{}, nil
	}
	return hex.DecodeString(s)
}
