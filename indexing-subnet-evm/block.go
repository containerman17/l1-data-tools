package main

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// IndexingBlock wraps a snowman.Block to intercept Accept
type IndexingBlock struct {
	snowman.Block
	vm *IndexingVM
}

// Accept is called when block is accepted into chain (both bootstrap and live).
// We index BEFORE accepting - this guarantees indexer is always >= chain.
// If crash after index but before accept: indexer ahead (safe, will skip on re-accept)
// If crash after accept: both consistent
func (b *IndexingBlock) Accept(ctx context.Context) error {
	height := b.Height()

	// Skip if already indexed (restart case - chain re-accepting what we already have)
	if height <= b.vm.lastIndexedHeight.Load() {
		return b.Block.Accept(ctx)
	}

	// INDEX FIRST (before chain commits)
	// This ensures indexer >= chain, never behind
	if err := b.vm.indexBlock(ctx, height); err != nil {
		b.vm.logger.Error("IndexingVM: failed to index block",
			logging.UserString("height", fmt.Sprintf("%d", height)),
			logging.UserString("error", err.Error()))
		return fmt.Errorf("indexing block %d: %w", height, err)
	}

	// THEN accept (commits to chain via versiondb.Commit())
	if err := b.Block.Accept(ctx); err != nil {
		// If Accept() fails, versiondb.Abort() clears ALL mem including our indexer data.
		// We return error here - Snowman will retry. Since lastIndexedHeight wasn't updated,
		// the skip check won't trigger and we'll re-index correctly.
		return err
	}

	// Only update heights AFTER successful Accept.
	// This ensures retry after failure will re-index.
	b.vm.lastIndexedHeight.Store(height)
	b.vm.lastAcceptedHeight.Store(height)
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
