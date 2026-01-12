// Package consts contains all tunable constants in one place
package consts

import "time"

// =============================================================================
// RPC Controller - Adaptive parallelism tuning
// =============================================================================

const (
	// RPCMetricsWindow is the sliding window for latency/error metrics
	RPCMetricsWindow = 60 * time.Second

	// RPCAdjustInterval is how often parallelism is adjusted
	RPCAdjustInterval = 5 * time.Second

	// RPCDefaultMaxParallelism if not specified in config
	RPCDefaultMaxParallelism = 200

	// RPCMaxLatency - default max P95 latency before reducing parallelism
	RPCMaxLatency = 1000 * time.Millisecond

	// RPCMaxErrorsPerMinute - halve parallelism if exceeded
	RPCMaxErrorsPerMinute = 10
)

// =============================================================================
// RPC Fetcher - Batch sizes and timeouts
// =============================================================================

const (
	// FetcherBatchSize for standard RPC calls (blocks, receipts)
	FetcherBatchSize = 50

	// FetcherDebugBatchSizeMax caps debug_trace* batch size
	FetcherDebugBatchSizeMax = 2

	// FetcherMaxRetries for failed RPC calls
	FetcherMaxRetries = 20

	// FetcherRetryDelay base delay between retries (exponential backoff)
	FetcherRetryDelay = 500 * time.Millisecond

	// FetcherHTTPTimeout for RPC requests
	FetcherHTTPTimeout = 30 * time.Second

	// FetcherMaxIdleConns for HTTP connection pool
	FetcherMaxIdleConns = 100

	// FetcherMaxConnsPerHost limits concurrent active connections per host
	// Without this, Go creates unlimited connections → port exhaustion
	FetcherMaxConnsPerHost = 50

	// FetcherIdleConnTimeout for HTTP keep-alive
	FetcherIdleConnTimeout = 90 * time.Second

	// FetcherDialTimeout for new connections
	FetcherDialTimeout = 30 * time.Second
)

// =============================================================================
// Storage - Compaction
// =============================================================================

const (
	// StorageBatchSize is blocks per compressed batch
	StorageBatchSize = 100

	// StorageMinBlocksBeforeCompaction keeps this many individual blocks before compacting
	StorageMinBlocksBeforeCompaction = 1000

	// StorageCompactionInterval is how often to check for compaction
	StorageCompactionInterval = 3 * time.Second
)

// =============================================================================
// Server - WebSocket streaming
// =============================================================================

const (
	// ServerListenAddr is the HTTP/WebSocket server address
	ServerListenAddr = ":9090"

	// MetricsListenAddr is the Prometheus metrics server address
	MetricsListenAddr = ":9091"

	// ServerTipPollInterval when waiting for new blocks at tip
	ServerTipPollInterval = 50 * time.Millisecond
)
