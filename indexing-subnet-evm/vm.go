package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"unsafe"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"

	"github.com/containerman17/l1-data-tools/evm-ingestion/api"
	"github.com/containerman17/l1-data-tools/evm-ingestion/storage"

	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/eth"
	"github.com/ava-labs/subnet-evm/eth/tracers"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/plugin/evm"
)

var (
	errChainIDRequired = errors.New("GRPC_INDEXER_CHAIN_ID env var is required")
	errChainNotAllowed = errors.New("chain ID does not match GRPC_INDEXER_CHAIN_ID")
)

// indexerDBName is the name of the standalone database directory
const indexerDBName = "indexer"

// IndexingVM wraps the subnet-evm VM and intercepts key methods
type IndexingVM struct {
	*evm.VM

	// Storage (shared pebble implementation)
	store *storage.Storage

	// Direct access (via reflection)
	eth       *eth.Ethereum
	tracerAPI *tracers.API
	chain     *core.BlockChain
	config    *params.ChainConfig

	// State tracking
	stateHistory       uint64 // from VM config - how many blocks of state history
	lastAcceptedHeight atomic.Uint64
	lastIndexedHeight  atomic.Uint64

	// Compactor (shared implementation)
	compactor *storage.Compactor

	// Firehose server
	server *api.Server

	logger logging.Logger
}

func NewIndexingVM() *IndexingVM {
	return &IndexingVM{VM: &evm.VM{}}
}

// Initialize wraps the underlying VM initialization
func (vm *IndexingVM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	vm.logger = chainCtx.Log

	vm.logger.Info("IndexingVM: initializing",
		logging.UserString("chainID", chainCtx.ChainID.String()))

	// Check if this chain is allowed (GRPC_* env vars are passed to plugins by avalanchego)
	allowedChainID := os.Getenv("GRPC_INDEXER_CHAIN_ID")
	if allowedChainID == "" {
		vm.logger.Error("IndexingVM: GRPC_INDEXER_CHAIN_ID is required")
		return errChainIDRequired
	}
	if chainCtx.ChainID.String() != allowedChainID {
		vm.logger.Warn("IndexingVM: chain not allowed, refusing to start",
			logging.UserString("chainID", chainCtx.ChainID.String()),
			logging.UserString("allowedChainID", allowedChainID))
		return errChainNotAllowed
	}

	// Create standalone database in chainData/{chainID}/indexer/
	// This ensures it's deleted when chainData is deleted
	dbPath := filepath.Join(chainCtx.ChainDataDir, indexerDBName)
	store, err := storage.NewStorage(dbPath)
	if err != nil {
		return fmt.Errorf("failed to create indexer storage: %w", err)
	}
	vm.store = store
	vm.logger.Info("IndexingVM: created indexer storage",
		logging.UserString("path", dbPath))

	// Initialize the underlying VM
	if err := vm.VM.Initialize(ctx, chainCtx, db, genesisBytes, upgradeBytes, configBytes, fxs, appSender); err != nil {
		return err
	}

	// Extract internal fields via reflection
	vm.eth = vm.getEthFromVM()
	if vm.eth == nil {
		return fmt.Errorf("failed to get eth.Ethereum via reflection")
	}

	vm.tracerAPI = tracers.NewAPI(vm.eth.APIBackend)
	vm.chain = vm.eth.BlockChain()
	vm.config = vm.chain.Config()

	// Initialize lastAcceptedHeight from chain state
	// This is critical - Accept() only fires for new blocks, not existing ones
	if currentBlock := vm.chain.CurrentBlock(); currentBlock != nil {
		vm.lastAcceptedHeight.Store(currentBlock.Number.Uint64())
		vm.logger.Info("IndexingVM: chain tip",
			logging.UserString("height", fmt.Sprintf("%d", currentBlock.Number.Uint64())))
	}

	// Read state history from config
	vm.stateHistory = vm.getStateHistory()
	vm.logger.Info("IndexingVM: state history",
		logging.UserString("blocks", fmt.Sprintf("%d", vm.stateHistory)))

	// Restore last indexed height from storage
	// Check BOTH meta (last compacted) AND latest individual block, use the higher
	// This handles the case where individual blocks exist above the last compacted batch
	lastIndexed := vm.store.GetMeta()
	if latest, ok := vm.store.LatestBlock(); ok && latest > lastIndexed {
		lastIndexed = latest
	}
	if lastIndexed > 0 {
		vm.lastIndexedHeight.Store(lastIndexed)
		vm.logger.Info("IndexingVM: restored lastIndexed",
			logging.UserString("height", fmt.Sprintf("%d", lastIndexed)),
			logging.UserString("meta", fmt.Sprintf("%d", vm.store.GetMeta())))
	}

	// FATAL CHECK: ANY gap on restart is unrecoverable
	// State history is in-memory only (lost on restart), and reexec=0 means no re-execution.
	// To trace block N, we need state at N-1 (parent). After restart, only tip state exists.
	//
	// NOTE: This should NOT happen during normal operation because Accept() indexes
	// BEFORE accepting. A gap can only occur if crash happens in the tiny window
	// between indexing and accepting (extremely rare).
	lastAccepted := vm.lastAcceptedHeight.Load()
	if lastIndexed > 0 && lastAccepted > lastIndexed {
		gap := lastAccepted - lastIndexed
		vm.logger.Error("IndexingVM: FATAL - gap detected, cannot trace historical blocks",
			logging.UserString("lastIndexed", fmt.Sprintf("%d", lastIndexed)),
			logging.UserString("lastAccepted", fmt.Sprintf("%d", lastAccepted)),
			logging.UserString("gap", fmt.Sprintf("%d", gap)),
			logging.UserString("fix", fmt.Sprintf("rm -rf %s", dbPath)))
		return fmt.Errorf("indexer gap of %d blocks detected - state history lost on restart - delete indexer DB to resync: rm -rf %s", gap, dbPath)
	}

	// Create compactor (shared implementation)
	compactorLogger := &pluginLogger{log: vm.logger}
	vm.compactor = storage.NewCompactorWithLogger(vm.store, compactorLogger)
	vm.compactor.Start(context.Background())
	vm.logger.Info("IndexingVM: compactor started")

	// Start firehose server
	vm.server = api.NewServer(vm.store, chainCtx.ChainID.String())
	// Initialize server's latestBlock from restored lastIndexed (otherwise stays 0 until new blocks arrive)
	if lastIndexed > 0 {
		vm.server.UpdateLatestBlock(lastIndexed)
	}
	actualAddr, err := vm.server.Start(":9090")
	if err != nil {
		return fmt.Errorf("firehose server failed: %w", err)
	}
	vm.logger.Info("IndexingVM: firehose server started",
		logging.UserString("addr", actualAddr))

	vm.logger.Info("IndexingVM: ready (sync indexing in Accept)")
	return nil
}

// Shutdown wraps VM shutdown
func (vm *IndexingVM) Shutdown(ctx context.Context) error {
	vm.logger.Info("IndexingVM: shutting down")

	if vm.compactor != nil {
		vm.compactor.Stop()
	}
	if vm.server != nil {
		vm.server.Stop()
	}
	if vm.store != nil {
		vm.store.Close()
	}

	return vm.VM.Shutdown(ctx)
}

// SetState wraps state changes
func (vm *IndexingVM) SetState(ctx context.Context, state snow.State) error {
	if err := vm.VM.SetState(ctx, state); err != nil {
		return err
	}

	if state == snow.NormalOp {
		vm.logger.Info("IndexingVM: entered NormalOp (bootstrap complete)")

		// Run format validation in background
		go vm.validateAfterBootstrap()
	}

	return nil
}

// BuildBlock wraps block building
func (vm *IndexingVM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	blk, err := vm.VM.BuildBlock(ctx)
	if err != nil {
		return nil, err
	}
	return &IndexingBlock{Block: blk, vm: vm}, nil
}

// ParseBlock wraps block parsing
func (vm *IndexingVM) ParseBlock(ctx context.Context, b []byte) (snowman.Block, error) {
	blk, err := vm.VM.ParseBlock(ctx, b)
	if err != nil {
		return nil, err
	}
	return &IndexingBlock{Block: blk, vm: vm}, nil
}

// GetBlock wraps block retrieval
func (vm *IndexingVM) GetBlock(ctx context.Context, id ids.ID) (snowman.Block, error) {
	blk, err := vm.VM.GetBlock(ctx, id)
	if err != nil {
		return nil, err
	}
	return &IndexingBlock{Block: blk, vm: vm}, nil
}

// ===== Reflection helpers =====

func (vm *IndexingVM) getEthFromVM() *eth.Ethereum {
	vmVal := reflect.ValueOf(vm.VM).Elem()
	ethField := vmVal.FieldByName("eth")
	if !ethField.IsValid() {
		return nil
	}
	ethPtr := reflect.NewAt(ethField.Type(), unsafe.Pointer(ethField.UnsafeAddr())).Elem()
	if ethPtr.IsNil() {
		return nil
	}
	return ethPtr.Interface().(*eth.Ethereum)
}

func (vm *IndexingVM) getStateHistory() uint64 {
	vmVal := reflect.ValueOf(vm.VM).Elem()
	configField := vmVal.FieldByName("config")
	if !configField.IsValid() {
		return 32 // default
	}
	configVal := reflect.NewAt(configField.Type(), unsafe.Pointer(configField.UnsafeAddr())).Elem()
	stateHistField := configVal.FieldByName("StateHistory")
	if !stateHistField.IsValid() {
		return 32
	}
	return stateHistField.Uint()
}

// pluginLogger wraps avalanchego logger for CompactorLogger interface
type pluginLogger struct {
	log logging.Logger
}

func (l *pluginLogger) Info(msg string, args ...any) {
	l.log.Info(fmt.Sprintf("Compactor: %s %v", msg, args))
}
func (l *pluginLogger) Warn(msg string, args ...any) {
	l.log.Warn(fmt.Sprintf("Compactor: %s %v", msg, args))
}
func (l *pluginLogger) Error(msg string, args ...any) {
	l.log.Error(fmt.Sprintf("Compactor: %s %v", msg, args))
}
