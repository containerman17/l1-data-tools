package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// IndexingBlock wraps a snowman.Block to intercept Accept
type IndexingBlock struct {
	snowman.Block
	vm *IndexingVM
}

// Accept is called when block is accepted into chain
// Just updates the counter and applies backpressure if indexer falls behind
func (b *IndexingBlock) Accept(ctx context.Context) error {
	height := b.Height()

	// Accept the block first
	if err := b.Block.Accept(ctx); err != nil {
		return err
	}

	// Update counter FIRST so indexing loop can see the new block
	b.vm.lastAcceptedHeight.Store(height)

	// Backpressure: if indexer falls too far behind, wait
	// Threshold = stateHistory - 2 (padding)
	threshold := b.vm.stateHistory
	if threshold > 2 {
		threshold -= 2
	}
	if threshold == 0 {
		threshold = 30 // fallback (32-2)
	}

	var waitStart time.Time
	waited := false

	for {
		lastIndexed := b.vm.lastIndexedHeight.Load()
		// If already indexed (restart case) or within safe margin, proceed
		if height <= lastIndexed || height-lastIndexed < threshold {
			break
		}
		// Chain is walking too far ahead, wait for indexer to catch up
		if !waited {
			waitStart = time.Now()
			waited = true
		}
		time.Sleep(1 * time.Millisecond)
	}

	if waited {
		b.vm.logger.Warn("IndexingVM: Accept blocked waiting for indexer",
			logging.UserString("height", fmt.Sprintf("%d", height)),
			logging.UserString("waited", time.Since(waitStart).String()))
	}

	return nil
}

// Reject wraps block rejection
func (b *IndexingBlock) Reject(ctx context.Context) error {
	return b.Block.Reject(ctx)
}

// Verify wraps block verification
func (b *IndexingBlock) Verify(ctx context.Context) error {
	return b.Block.Verify(ctx)
}
