package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// HeadTracker tracks the latest block number via WebSocket subscription
type HeadTracker struct {
	rpcURL      string
	wsURL       string
	latestBlock atomic.Uint64
	chainID     uint64
	chainName   string

	conn   *websocket.Conn
	connMu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewHeadTracker creates a new head tracker
// rpcURL must contain /rpc, which will be converted to /ws for WebSocket
// Returns error if URL doesn't contain /rpc
func NewHeadTracker(ctx context.Context, rpcURL string, chainID uint64, chainName string) (*HeadTracker, error) {
	if !strings.Contains(rpcURL, "/rpc") {
		return nil, fmt.Errorf("RPC URL must contain '/rpc' for WebSocket conversion, got: %s", rpcURL)
	}

	wsURL := strings.Replace(rpcURL, "/rpc", "/ws", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)

	trackerCtx, cancel := context.WithCancel(ctx)
	return &HeadTracker{
		rpcURL:    rpcURL,
		wsURL:     wsURL,
		chainID:   chainID,
		chainName: chainName,
		ctx:       trackerCtx,
		cancel:    cancel,
	}, nil
}

// Start initializes the tracker: fetches initial block via RPC, then starts WebSocket subscription
func (h *HeadTracker) Start() error {
	// Get initial block number via RPC
	initialBlock, err := h.fetchBlockNumberRPC()
	if err != nil {
		return fmt.Errorf("failed to get initial block number: %w", err)
	}
	h.latestBlock.Store(initialBlock)
	log.Printf("[HeadTracker %d - %s] Initial block: %d", h.chainID, h.chainName, initialBlock)

	// Start WebSocket subscription in background
	h.wg.Add(1)
	go h.runWebSocket()

	return nil
}

// Stop stops the tracker
func (h *HeadTracker) Stop() {
	h.cancel()
	h.connMu.Lock()
	if h.conn != nil {
		h.conn.Close()
	}
	h.connMu.Unlock()
	h.wg.Wait()
}

// GetLatestBlock returns the cached latest block number (instant, no RPC call)
func (h *HeadTracker) GetLatestBlock() uint64 {
	return h.latestBlock.Load()
}

// fetchBlockNumberRPC gets the current block number via HTTP RPC
func (h *HeadTracker) fetchBlockNumberRPC() (uint64, error) {
	req := JSONRPCRequest{
		Jsonrpc: "2.0",
		Method:  "eth_blockNumber",
		Params:  []interface{}{},
		ID:      1,
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return 0, err
	}

	resp, err := http.Post(h.rpcURL, "application/json", bytes.NewReader(reqData))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return 0, err
	}

	if rpcResp.Error != nil {
		return 0, fmt.Errorf("RPC error: %s", rpcResp.Error.Message)
	}

	var blockNumHex string
	if err := json.Unmarshal(rpcResp.Result, &blockNumHex); err != nil {
		return 0, err
	}

	blockNum, err := strconv.ParseUint(strings.TrimPrefix(blockNumHex, "0x"), 16, 64)
	if err != nil {
		return 0, err
	}

	return blockNum, nil
}

// runWebSocket manages the WebSocket connection and subscription
func (h *HeadTracker) runWebSocket() {
	defer h.wg.Done()

	for {
		select {
		case <-h.ctx.Done():
			return
		default:
		}

		if err := h.connectAndSubscribe(); err != nil {
			log.Printf("[HeadTracker %d - %s] WebSocket error: %v, reconnecting in 5s", h.chainID, h.chainName, err)
			time.Sleep(5 * time.Second)
			continue
		}
	}
}

// connectAndSubscribe connects to WebSocket and subscribes to newHeads
func (h *HeadTracker) connectAndSubscribe() error {
	// Connect
	conn, _, err := websocket.DefaultDialer.DialContext(h.ctx, h.wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	h.connMu.Lock()
	h.conn = conn
	h.connMu.Unlock()

	defer func() {
		h.connMu.Lock()
		h.conn = nil
		conn.Close()
		h.connMu.Unlock()
	}()

	// Subscribe to newHeads
	subReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_subscribe",
		"params":  []string{"newHeads"},
	}
	if err := conn.WriteJSON(subReq); err != nil {
		return fmt.Errorf("failed to send subscribe: %w", err)
	}

	// Read subscription response
	var subResp struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *JSONRPCError   `json:"error"`
	}
	if err := conn.ReadJSON(&subResp); err != nil {
		return fmt.Errorf("failed to read subscribe response: %w", err)
	}
	if subResp.Error != nil {
		return fmt.Errorf("subscribe error: %s", subResp.Error.Message)
	}

	log.Printf("[HeadTracker %d - %s] WebSocket connected, subscribed to newHeads", h.chainID, h.chainName)

	// Read new heads
	for {
		select {
		case <-h.ctx.Done():
			return nil
		default:
		}

		var msg struct {
			Method string `json:"method"`
			Params struct {
				Result struct {
					Number string `json:"number"`
				} `json:"result"`
			} `json:"params"`
		}

		if err := conn.ReadJSON(&msg); err != nil {
			return fmt.Errorf("failed to read message: %w", err)
		}

		if msg.Method != "eth_subscription" || msg.Params.Result.Number == "" {
			continue
		}

		blockNum, err := strconv.ParseUint(strings.TrimPrefix(msg.Params.Result.Number, "0x"), 16, 64)
		if err != nil {
			log.Printf("[HeadTracker %d - %s] Failed to parse block number %s: %v", h.chainID, h.chainName, msg.Params.Result.Number, err)
			continue
		}

		old := h.latestBlock.Load()
		if blockNum > old {
			h.latestBlock.Store(blockNum)
		}
	}
}
