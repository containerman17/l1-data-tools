package api

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/containerman17/l1-data-tools/evm-ingestion/consts"
	"github.com/containerman17/l1-data-tools/evm-ingestion/storage"

	"github.com/gorilla/websocket"
	"github.com/klauspost/compress/zstd"
)

type Server struct {
	httpServer  *http.Server
	store       *storage.Storage
	latestBlock atomic.Uint64
	ctx         context.Context
	cancel      context.CancelFunc
	zstdEnc     *zstd.Encoder
	mu          sync.RWMutex
	chainID     string // 32-byte Avalanche chain ID (base58)
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 64 * 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func NewServer(store *storage.Storage, chainID string) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	enc, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	return &Server{
		store:   store,
		ctx:     ctx,
		cancel:  cancel,
		zstdEnc: enc,
		chainID: chainID,
	}
}

// UpdateLatestBlock updates the latest known block
func (s *Server) UpdateLatestBlock(blockNum uint64) {
	for {
		current := s.latestBlock.Load()
		if blockNum <= current {
			return
		}
		if s.latestBlock.CompareAndSwap(current, blockNum) {
			return
		}
	}
}

// GetLatestBlock returns the latest known block
func (s *Server) GetLatestBlock() uint64 {
	return s.latestBlock.Load()
}

func (s *Server) Start(addr string) (string, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /info", s.handleInfo)
	mux.HandleFunc("GET /ws", s.handleWS)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	s.httpServer = &http.Server{Handler: mux}
	go func() {
		if err := s.httpServer.Serve(listener); err != http.ErrServerClosed {
			log.Printf("[Server] HTTP server error: %v", err)
		}
	}()

	log.Printf("[Server] Listening on %s", addr)
	return addr, nil
}

func (s *Server) Stop() {
	s.cancel()
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpServer.Shutdown(ctx)
	}
}

// handleInfo returns chain info as JSON
func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"chainID":"%s","latestBlock":%d}`, s.chainID, s.latestBlock.Load())
}

// handleWS upgrades to WebSocket and streams blocks
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	fromBlockStr := r.URL.Query().Get("from")

	fromBlock := uint64(1)
	if fromBlockStr != "" {
		var err error
		fromBlock, err = strconv.ParseUint(fromBlockStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid from parameter", http.StatusBadRequest)
			return
		}
	}
	if fromBlock == 0 {
		fromBlock = 1
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[Server] WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("[Server] Client connected from block %d", fromBlock)

	if err := s.streamBlocks(conn, fromBlock); err != nil {
		log.Printf("[Server] Client stream ended: %v", err)
	}
}

// streamBlocks streams blocks over WebSocket
// Binary frames: zstd(NormalizedBlock\n...) - 1 to 100 blocks per frame
func (s *Server) streamBlocks(conn *websocket.Conn, fromBlock uint64) error {
	ctx := s.ctx
	currentBlock := fromBlock

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 1. Try single block from local store
		data, err := s.store.GetBlock(currentBlock)
		if err == nil && len(data) > 0 {
			compressed := s.zstdEnc.EncodeAll(append(data, '\n'), nil)
			if err := conn.WriteMessage(websocket.BinaryMessage, compressed); err != nil {
				return err
			}
			currentBlock++
			continue
		}

		// 2. Try compressed batch from local store
		batchStart := storage.BatchStart(currentBlock)
		batchData, err := s.store.GetBatchCompressed(batchStart)
		if err == nil && len(batchData) > 0 {
			// Send as-is (already zstd compressed JSONL)
			if err := conn.WriteMessage(websocket.BinaryMessage, batchData); err != nil {
				return err
			}
			currentBlock = batchStart + storage.BatchSize
			continue
		}

		// Block not available - we're at the tip, wait
		time.Sleep(consts.ServerTipPollInterval)
	}
}
