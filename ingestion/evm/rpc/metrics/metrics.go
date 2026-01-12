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
)

func init() {
	prometheus.MustRegister(BlocksTotal)
	prometheus.MustRegister(BlocksBehind)
	prometheus.MustRegister(LastIngestedBlock)
	prometheus.MustRegister(ChainHead)
	prometheus.MustRegister(RPCRequestsTotal)
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
