package rpc

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/containerman17/l1-data-tools/evm-ingestion/consts"
)

type RequestMetric struct {
	Timestamp time.Time
	Duration  time.Duration
	Success   bool
}

type Controller struct {
	url             string
	maxParallelism  int
	minParallelism  int
	targetLatency   time.Duration
	maxLatency      time.Duration
	maxErrorsPerMin int

	currentParallel atomic.Int32
	semaphore       chan struct{}

	metrics   []RequestMetric
	metricsMu sync.Mutex

	stopCh chan struct{}
	wg     sync.WaitGroup
}

func NewController(cfg ChainConfig) *Controller {
	maxP := cfg.MaxParallelism
	if maxP <= 0 {
		maxP = consts.RPCDefaultMaxParallelism
	}

	maxLatency := time.Duration(cfg.MaxLatencyMs) * time.Millisecond
	if maxLatency <= 0 {
		maxLatency = consts.RPCMaxLatency
	}
	// targetLatency is half of max - grow parallelism when below this
	targetLatency := maxLatency / 2

	// Derive everything else from maxParallelism
	minP := max(2, maxP/10)

	c := &Controller{
		url:             cfg.URL,
		maxParallelism:  maxP,
		minParallelism:  minP,
		targetLatency:   targetLatency,
		maxLatency:      maxLatency,
		maxErrorsPerMin: consts.RPCMaxErrorsPerMinute,
		semaphore:       make(chan struct{}, maxP), // Capacity is max, but we start with min tokens
		metrics:         make([]RequestMetric, 0, 1000),
		stopCh:          make(chan struct{}),
	}
	c.currentParallel.Store(int32(minP))

	// Fill semaphore to min capacity - will climb up based on performance
	for i := 0; i < minP; i++ {
		c.semaphore <- struct{}{}
	}

	// Start adjustment loop
	c.wg.Add(1)
	go c.adjustLoop()

	return c
}

func (c *Controller) URL() string {
	return c.url
}

func (c *Controller) CurrentParallelism() int {
	return int(c.currentParallel.Load())
}

// P95Latency returns the current P95 latency from the metrics window
func (c *Controller) P95Latency() time.Duration {
	c.metricsMu.Lock()
	defer c.metricsMu.Unlock()

	if len(c.metrics) < 10 {
		return 0
	}

	var durations []time.Duration
	for _, m := range c.metrics {
		durations = append(durations, m.Duration)
	}

	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})

	p95Idx := int(float64(len(durations)) * 0.95)
	if p95Idx >= len(durations) {
		p95Idx = len(durations) - 1
	}
	return durations[p95Idx]
}

// Acquire blocks until a slot is available
func (c *Controller) Acquire(ctx context.Context) error {
	select {
	case <-c.semaphore:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release returns a slot to the pool
func (c *Controller) Release() {
	select {
	case c.semaphore <- struct{}{}:
	default:
		// Semaphore full (parallelism was reduced), discard
	}
}

// RecordMetric records the result of a request
func (c *Controller) RecordMetric(duration time.Duration, success bool) {
	c.metricsMu.Lock()
	c.metrics = append(c.metrics, RequestMetric{
		Timestamp: time.Now(),
		Duration:  duration,
		Success:   success,
	})
	c.metricsMu.Unlock()
}

// Execute runs fn with rate limiting and records metrics
func (c *Controller) Execute(ctx context.Context, fn func() error) error {
	if err := c.Acquire(ctx); err != nil {
		return err
	}
	defer c.Release()

	start := time.Now()
	err := fn()
	c.RecordMetric(time.Since(start), err == nil)
	return err
}

func (c *Controller) adjustLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(consts.RPCAdjustInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.adjust()
		}
	}
}

func (c *Controller) adjust() {
	c.metricsMu.Lock()
	defer c.metricsMu.Unlock()

	// Prune old metrics
	cutoff := time.Now().Add(-consts.RPCMetricsWindow)
	validStart := 0
	for i, m := range c.metrics {
		if m.Timestamp.After(cutoff) {
			validStart = i
			break
		}
		if i == len(c.metrics)-1 {
			validStart = len(c.metrics)
		}
	}
	c.metrics = c.metrics[validStart:]

	if len(c.metrics) < 10 {
		// Not enough data to make decisions
		return
	}

	// Count errors in window
	errorCount := 0
	var durations []time.Duration
	for _, m := range c.metrics {
		if !m.Success {
			errorCount++
		}
		durations = append(durations, m.Duration)
	}

	// Calculate P95 latency
	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})
	p95Idx := int(float64(len(durations)) * 0.95)
	if p95Idx >= len(durations) {
		p95Idx = len(durations) - 1
	}
	p95Latency := durations[p95Idx]

	current := int(c.currentParallel.Load())
	newParallel := current

	// Adjustment logic
	if errorCount > c.maxErrorsPerMin {
		// Aggressive backoff on errors
		newParallel = current / 2
	} else if p95Latency > c.maxLatency {
		// Reduce on high latency
		newParallel = current - 2
	} else if p95Latency < c.targetLatency {
		// Grow faster when latency is way below target
		// At 10% of target: grow by ~10, at 50%: grow by ~2, at 90%: grow by 1
		ratio := float64(p95Latency) / float64(c.targetLatency)
		growth := int(1.0 / (ratio + 0.1)) // +0.1 to avoid division by zero
		if growth < 1 {
			growth = 1
		}
		if growth > 20 {
			growth = 20
		}
		newParallel = current + growth
	}

	// Clamp to bounds
	if newParallel < c.minParallelism {
		newParallel = c.minParallelism
	}
	if newParallel > c.maxParallelism {
		newParallel = c.maxParallelism
	}

	if newParallel != current {
		c.currentParallel.Store(int32(newParallel))
		// Adjust semaphore capacity
		if newParallel > current {
			// Add slots
			for i := 0; i < newParallel-current; i++ {
				select {
				case c.semaphore <- struct{}{}:
				default:
				}
			}
		}
		// If reducing, slots will naturally drain as Release() discards them
	}
}

func (c *Controller) Stop() {
	close(c.stopCh)
	c.wg.Wait()
}
