package metrics

import (
	"fmt"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// BlocksTotal counts total blocks ingested per chain
	BlocksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ingestion_blocks_total",
			Help: "Total number of blocks ingested",
		},
		[]string{"chain"},
	)

	// BlocksBehind shows how many blocks behind head per chain
	BlocksBehind = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ingestion_blocks_behind",
			Help: "Number of blocks behind chain head",
		},
		[]string{"chain"},
	)

	// LastIngestedBlock shows the last ingested block number per chain
	LastIngestedBlock = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ingestion_last_block",
			Help: "Last ingested block number",
		},
		[]string{"chain"},
	)

	// ChainHead shows the latest block number on the chain
	ChainHead = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ingestion_chain_head",
			Help: "Latest block number on the chain",
		},
		[]string{"chain"},
	)

	// RPCRequestsTotal counts RPC HTTP requests per chain and status
	RPCRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ingestion_rpc_requests_total",
			Help: "Total RPC HTTP requests",
		},
		[]string{"chain", "status"},
	)

	// Client buffer metrics

	// ClientBufferUsedBytes shows current compressed bytes in buffer
	ClientBufferUsedBytes = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ingestion_client_buffer_used_bytes",
			Help: "Current compressed bytes in client receive buffer",
		},
	)

	// ClientBufferCapacityBytes shows buffer size limit
	ClientBufferCapacityBytes = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ingestion_client_buffer_capacity_bytes",
			Help: "Client receive buffer size limit",
		},
	)

	// ClientBatchesProcessedTotal counts number of batches processed
	ClientBatchesProcessedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ingestion_client_batches_processed_total",
			Help: "Total number of batches processed by client",
		},
	)

	// ClientBatchSizeBytes histogram of compressed size per batch
	ClientBatchSizeBytes = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "ingestion_client_batch_size_bytes",
			Help:    "Compressed size per batch in bytes",
			Buckets: prometheus.ExponentialBuckets(1024, 4, 10), // 1KB to ~256MB
		},
	)

	// ClientBackpressureWaitSeconds histogram of time spent waiting for buffer space
	ClientBackpressureWaitSeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "ingestion_client_backpressure_wait_seconds",
			Help:    "Time spent waiting for buffer space due to backpressure",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to ~16s
		},
	)
)

func init() {
	prometheus.MustRegister(BlocksTotal)
	prometheus.MustRegister(BlocksBehind)
	prometheus.MustRegister(LastIngestedBlock)
	prometheus.MustRegister(ChainHead)
	prometheus.MustRegister(RPCRequestsTotal)
	prometheus.MustRegister(ClientBufferUsedBytes)
	prometheus.MustRegister(ClientBufferCapacityBytes)
	prometheus.MustRegister(ClientBatchesProcessedTotal)
	prometheus.MustRegister(ClientBatchSizeBytes)
	prometheus.MustRegister(ClientBackpressureWaitSeconds)
}

// ChainLabel returns the combined chain label "name_id"
func ChainLabel(name string, id uint64) string {
	return fmt.Sprintf("%s_%d", name, id)
}

// InitChain initializes all metrics for a chain with zero values
// This ensures metrics appear in Prometheus even before any events
func InitChain(chainLabel string) {
	BlocksTotal.WithLabelValues(chainLabel).Add(0)
	BlocksBehind.WithLabelValues(chainLabel).Set(0)
	LastIngestedBlock.WithLabelValues(chainLabel).Set(0)
	ChainHead.WithLabelValues(chainLabel).Set(0)
	RPCRequestsTotal.WithLabelValues(chainLabel, "success").Add(0)
	RPCRequestsTotal.WithLabelValues(chainLabel, "error").Add(0)
}

// StartServer starts the metrics HTTP server on the given address
func StartServer(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	go func() {
		log.Printf("[Metrics] Listening on %s", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("[Metrics] Server error: %v", err)
		}
	}()
}
