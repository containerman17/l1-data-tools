# AvalancheGo UTXO & Atomic Transactions: Complete Navigation Guide

**Version**: Cross-referenced from 4 AI analyses + codebase verification  
**Date**: December 2025  
**Purpose**: Authoritative guide for building UTXO/atomic transaction experts

---

## Executive Summary

Avalanche's Primary Network consists of 3 chains:
- **X-Chain** (Exchange/AVM): UTXO-based, multi-asset, DAG consensus
- **P-Chain** (Platform): UTXO-based, validator/subnet management, AVAX only
- **C-Chain** (Contract/EVM): Account-based EVM, with UTXO bridge via atomic transactions

**Critical Insight**: **Shared Memory is NOT a 4th chain.** It's a database primitive creating pair-wise bidirectional channels between chain pairs (P↔X, P↔C, X↔C), each isolated.

---

## Table of Contents

1. [Shared Memory Architecture](#1-shared-memory-architecture)
2. [Core UTXO Components](#2-core-utxo-components)
3. [X-Chain (AVM) Implementation](#3-x-chain-avm-implementation)
4. [P-Chain (PlatformVM) Implementation](#4-p-chain-platformvm-implementation)
5. [C-Chain Atomic Transactions](#5-c-chain-atomic-transactions)
6. [Atomic Transaction Flow](#6-atomic-transaction-flow)
7. [RPC APIs & Endpoints](#7-rpc-apis--endpoints)
8. [Balance Calculation](#8-balance-calculation)
9. [Key Data Structures](#9-key-data-structures)
10. [File System Navigation](#10-file-system-navigation)
11. [Critical Constants & IDs](#11-critical-constants--ids)
12. [Common Patterns & Gotchas](#12-common-patterns--gotchas)
13. [Testing & Examples](#13-testing--examples)
14. [FAQ](#14-faq)

---

## 1. Shared Memory Architecture

### 1.1 Core Concept

**Location**: `avalanchego/chains/atomic/`

**Read This First**: `chains/atomic/README.md` - The definitive explanation

Shared memory enables cross-chain communication by creating a shared database layer accessible to all VMs on the same subnet. It's **NOT a separate chain**, but rather a database abstraction.

### 1.2 Architecture

```
Base Database (leveldb)
├── Chain A prefixdb (ChainA's own state)
├── Chain B prefixdb (ChainB's own state)
└── Shared Memory prefixdb
    └── For each chain pair:
        sharedID = hash(sort([chainA_ID, chainB_ID]))
        ├── Inbound state (messages TO ChainA FROM ChainB)
        └── Outbound state (messages TO ChainB FROM ChainA)
```

**Key Properties**:
- Uses `prefixdb` to partition a single LevelDB
- Each chain pair (P↔X, P↔C, X↔C) gets unique `sharedID`
- Operations are atomic with chain's own database operations
- Concurrency control via per-pair locks

### 1.3 Key Files

```
chains/atomic/
├── README.md              # ⭐ START HERE - Complete explanation
├── shared_memory.go       # SharedMemory interface & implementation
├── memory.go              # Memory management, locking
├── state.go               # Inbound/outbound state management
├── codec.go               # Serialization
├── prefixes.go            # Database prefixes
├── writer.go              # Batch operations
└── gsharedmemory/         # gRPC implementation for remote VMs
    ├── shared_memory_client.go
    └── shared_memory_server.go
```

### 1.4 SharedMemory Interface

**File**: `chains/atomic/shared_memory.go`

```go
type SharedMemory interface {
    // Get fetches values for specific keys from peer chain
    Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error)
    
    // Indexed returns paginated values with specified traits (addresses)
    Indexed(
        peerChainID ids.ID,
        traits [][]byte,        // Address bytes for filtering
        startTrait []byte,      // Pagination start trait
        startKey []byte,        // Pagination start key
        limit int,
    ) (values [][]byte, lastTrait []byte, lastKey []byte, error)
    
    // Apply atomically applies operations across chains + batches
    Apply(requests map[ids.ID]*Requests, batches ...database.Batch) error
}

type Requests struct {
    RemoveRequests [][]byte    // Keys to remove from inbound
    PutRequests    []*Element  // Elements to add to outbound
}

type Element struct {
    Key    []byte   // UTXO ID
    Value  []byte   // Serialized UTXO
    Traits [][]byte // Addresses (for indexing)
}
```

### 1.5 State Management

**File**: `chains/atomic/state.go`

Two databases per state:
- **valueDB**: Direct `Key → Value` mapping
- **indexDB**: `Trait → [Keys]` mapping (one-to-many)

This enables:
- Fast `Get()` by UTXO ID
- Fast `Indexed()` by address (trait)

### 1.6 Lifecycle Example

```
┌─────────────────────────────────────────────────────────┐
│ Export P-Chain → C-Chain                                │
├─────────────────────────────────────────────────────────┤
│ 1. P-Chain executes ExportTx                            │
│    └─> SharedMemory.Apply({                             │
│          C-Chain: {PutRequests: [UTXO elements]}        │
│        })                                                │
│    └─> UTXO added to C-Chain's INBOUND state            │
│                                                          │
│ 2. C-Chain queries:                                     │
│    avax.getUTXOs(sourceChain="P", addresses=[...])     │
│    └─> SharedMemory.Indexed(P-Chain, address_traits)   │
│    └─> Returns UTXOs in C-Chain's inbound from P-Chain │
│                                                          │
│ 3. C-Chain executes ImportTx                            │
│    └─> SharedMemory.Apply({                             │
│          P-Chain: {RemoveRequests: [UTXO_IDs]}          │
│        })                                                │
│    └─> UTXO removed from C-Chain's INBOUND state        │
│    └─> EVM balance credited                             │
└─────────────────────────────────────────────────────────┘
```

---

## 2. Core UTXO Components

### 2.1 Location

**Base**: `avalanchego/vms/components/avax/`

This package contains shared UTXO primitives used by X-Chain and P-Chain.

### 2.2 Key Files

```
vms/components/avax/
├── utxo.go              # ⭐ UTXO struct definition
├── utxo_id.go           # UTXOID: TxID + OutputIndex
├── utxo_state.go        # UTXOState interface for state management
├── utxo_fetching.go     # GetBalance, GetPaginatedUTXOs
├── atomic_utxos.go      # ⭐ GetAtomicUTXOs (from shared memory)
├── transferables.go     # TransferableInput/Output
├── base_tx.go           # BaseTx structure
├── flow_checker.go      # Verify inputs >= outputs + fees
├── state.go             # Consume/Produce helper functions
└── addresses.go         # Address parsing utilities
```

### 2.3 UTXO Structure

**File**: `vms/components/avax/utxo.go`

```go
type UTXO struct {
    UTXOID          // TxID + OutputIndex (unique identifier)
    Asset  Asset    // AssetID
    Out    verify.State  // The actual output (e.g., TransferOutput)
}

type UTXOID struct {
    TxID        ids.ID
    OutputIndex uint32
}

type Asset struct {
    ID ids.ID  // AssetID (AVAX or custom)
}
```

### 2.4 Transferable Types

**File**: `vms/components/avax/transferables.go`

```go
type TransferableInput struct {
    UTXOID          // Which UTXO being spent
    Asset           // Which asset
    In TransferableIn  // The input (amount + signature indices)
}

type TransferableOutput struct {
    Asset              // Which asset
    Out TransferableOut  // The output (amount + owners)
}
```

### 2.5 UTXO State Management

**Interface**: `UTXOState` in `vms/components/avax/utxo_state.go`

```go
type UTXOState interface {
    UTXOReader
    UTXOWriter
    UTXODeleter
}

type UTXOReader interface {
    GetUTXO(utxoID ids.ID) (*UTXO, error)
    UTXOIDs(addr []byte, start ids.ID, limit int) ([]ids.ID, error)
}
```

**Implementation**: Each chain implements this differently:
- **X-Chain**: `vms/avm/state/state.go`
- **P-Chain**: `vms/platformvm/state/state.go`

### 2.6 UTXO Fetching

#### Local UTXOs

**File**: `vms/components/avax/utxo_fetching.go`

```go
// GetBalance sums all UTXO amounts for addresses
func GetBalance(db UTXOReader, addrs set.Set[ids.ShortID]) (uint64, error)

// GetPaginatedUTXOs returns UTXOs with pagination
func GetPaginatedUTXOs(
    db UTXOReader,
    addrs set.Set[ids.ShortID],
    lastAddr ids.ShortID,
    lastUTXOID ids.ID,
    limit int,
) ([]*UTXO, ids.ShortID, ids.ID, error)
```

**Key insight**: Iterates sorted addresses, fetches UTXOs per address, deduplicates by UTXO ID.

#### Atomic UTXOs

**File**: `vms/components/avax/atomic_utxos.go`

```go
// GetAtomicUTXOs fetches UTXOs from shared memory by address traits
func GetAtomicUTXOs(
    sharedMemory atomic.SharedMemory,
    codec codec.Manager,
    chainID ids.ID,           // Source chain ID
    addrs set.Set[ids.ShortID],  // Filter by these addresses
    startAddr ids.ShortID,
    startUTXOID ids.ID,
    limit int,
) ([]*UTXO, ids.ShortID, ids.ID, error) {
    // Converts addresses to traits
    addrsList := make([][]byte, addrs.Len())
    for addr := range addrs {
        addrsList[i] = addr[:]
    }
    
    // Queries shared memory with address traits
    allUTXOBytes, lastAddr, lastUTXO, err := sharedMemory.Indexed(
        chainID,      // Peer chain (source of UTXOs)
        addrsList,    // Traits (addresses)
        startAddr.Bytes(),
        startUTXOID[:],
        limit,
    )
    
    // Deserializes UTXOs
    for _, utxoBytes := range allUTXOBytes {
        codec.Unmarshal(utxoBytes, utxo)
    }
}
```

**Critical**: This is how ALL chains query atomic UTXOs from shared memory.

---

## 3. X-Chain (AVM) Implementation

### 3.1 Overview

**Location**: `avalanchego/vms/avm/`

- UTXO-based
- Multi-asset support (AVAX + custom ANTs)
- DAG consensus (Avalanche)
- Asset creation & operations

### 3.2 Directory Structure

```
vms/avm/
├── vm.go                  # VM implementation
├── service.go             # ⭐ RPC API implementation
├── service.md             # ⭐ API documentation
├── client.go              # Go RPC client
├── config/                # Configuration
├── txs/                   # ⭐ Transaction types
│   ├── base_tx.go         # Simple UTXO transfer
│   ├── import_tx.go       # ⭐ Import from P/C-Chain
│   ├── export_tx.go       # ⭐ Export to P/C-Chain
│   ├── create_asset_tx.go # Create ANT
│   ├── operation_tx.go    # NFT/mint operations
│   ├── executor/          # ⭐ Transaction execution
│   │   ├── executor.go
│   │   └── semantic_verifier.go
│   └── codec.go           # Type registration
├── state/                 # State management
│   ├── state.go
│   └── diff.go
├── utxo/                  # UTXO selection
│   └── spender.go
└── block/                 # Block building
```

### 3.3 Transaction Types

#### Import Transaction

**File**: `vms/avm/txs/import_tx.go`

```go
type ImportTx struct {
    BaseTx                          // Ins, Outs, Memo
    SourceChain    ids.ID           // P-Chain or C-Chain
    ImportedInputs []*avax.TransferableInput  // UTXOs from shared memory
}
```

**Execution**: `vms/avm/txs/executor/executor.go`

```go
func (e *executor) ImportTx(tx *txs.ImportTx) error {
    // 1. Verify UTXOs exist in shared memory
    utxoIDs := make([][]byte, len(tx.ImportedInputs))
    allUTXOBytes, _ := e.sharedMemory.Get(tx.SourceChain, utxoIDs)
    
    // 2. Verify signatures, flow check
    // ...
    
    // 3. On accept: Remove from shared memory, add to X-Chain
    e.atomicRequests = map[ids.ID]*atomic.Requests{
        tx.SourceChain: {
            RemoveRequests: utxoIDs,  // Remove from inbound
        },
    }
    avax.Produce(e.state, txID, tx.Outs)  // Create on X-Chain
}
```

#### Export Transaction

**File**: `vms/avm/txs/export_tx.go`

```go
type ExportTx struct {
    BaseTx                             // Ins, Outs, Memo
    DestinationChain ids.ID            // P-Chain or C-Chain
    ExportedOutputs  []*avax.TransferableOutput  // UTXOs to send
}
```

**Execution**: `vms/avm/txs/executor/executor.go`

```go
func (e *executor) ExportTx(tx *txs.ExportTx) error {
    // 1. Consume UTXOs on X-Chain
    avax.Consume(e.state, tx.Ins)
    avax.Produce(e.state, txID, tx.Outs)  // Change outputs
    
    // 2. Create atomic elements with address traits
    elems := make([]*atomic.Element, len(tx.ExportedOutputs))
    for i, out := range tx.ExportedOutputs {
        utxo := &avax.UTXO{
            UTXOID: avax.UTXOID{TxID: txID, OutputIndex: uint32(i)},
            Asset:  out.Asset,
            Out:    out.Out,
        }
        utxoBytes, _ := txs.Codec.Marshal(txs.CodecVersion, utxo)
        
        // Extract addresses as traits
        addressable, _ := out.Out.(Addressable)
        traits := make([][]byte, len(addressable.Addresses()))
        for j, addr := range addressable.Addresses() {
            traits[j] = addr[:]
        }
        
        elems[i] = &atomic.Element{
            Key:    utxo.InputID().Bytes(),
            Value:  utxoBytes,
            Traits: traits,
        }
    }
    
    // 3. Put to shared memory for destination chain
    e.atomicRequests = map[ids.ID]*atomic.Requests{
        tx.DestinationChain: {
            PutRequests: elems,  // Add to destination's inbound
        },
    }
}
```

### 3.4 RPC API

**File**: `vms/avm/service.go`

#### Key Methods

```go
// GetUTXOs returns UTXOs for addresses
func (s *Service) GetUTXOs(args *api.GetUTXOsArgs, reply *api.GetUTXOsReply) error {
    sourceChain := args.SourceChain  // Optional
    
    if sourceChain == "" || sourceChain == thisChain {
        // Local UTXOs
        utxos, _, _, _ = avax.GetPaginatedUTXOs(s.vm.state, addrs, ...)
    } else {
        // Atomic UTXOs from shared memory
        utxos, _, _, _ = avax.GetAtomicUTXOs(
            s.vm.ctx.SharedMemory,
            s.vm.parser.Codec(),
            sourceChainID,
            addrs,
            ...
        )
    }
}

// GetBalance (DEPRECATED) - sum of UTXOs
func (s *Service) GetBalance(args *GetBalanceArgs, reply *GetBalanceReply) error
```

**Endpoint**: `/ext/bc/X`

**Methods**:
- `avm.getUTXOs` - Get UTXOs (local or atomic)
- `avm.getBalance` - DEPRECATED, sum UTXOs client-side
- `avm.getAllBalances` - All assets
- `avm.issueTx` - Submit transaction
- `avm.getAtomicTx` - Get atomic transaction
- `avm.getAtomicTxStatus` - Status

---

## 4. P-Chain (PlatformVM) Implementation

### 4.1 Overview

**Location**: `avalanchego/vms/platformvm/`

- UTXO-based
- AVAX only
- Validator/delegator management
- Subnet/blockchain registration
- Staking with time-locked outputs

### 4.2 Directory Structure

```
vms/platformvm/
├── vm.go                  # VM implementation
├── service.go             # ⭐ RPC API (2152 lines!)
├── service.md             # ⭐ API docs (2198 lines!)
├── client.go              # Go RPC client
├── config/                # Configuration
├── txs/                   # ⭐ Transaction types
│   ├── base_tx.go
│   ├── import_tx.go       # ⭐ Import from X/C-Chain
│   ├── export_tx.go       # ⭐ Export to X/C-Chain
│   ├── add_validator_tx.go
│   ├── add_delegator_tx.go
│   ├── add_permissionless_validator_tx.go
│   ├── add_permissionless_delegator_tx.go
│   ├── add_subnet_validator_tx.go
│   ├── create_subnet_tx.go
│   ├── create_chain_tx.go
│   ├── ... (15+ more types)
│   ├── executor/          # ⭐ Execution logic
│   │   ├── standard_tx_executor.go    # Import/Export execution
│   │   ├── atomic_tx_executor.go
│   │   ├── proposal_tx_executor.go    # Validator txs
│   │   └── staker_tx_verification.go
│   └── fee/               # Fee calculation
├── state/                 # ⭐ Complex state
│   ├── state.go           # UTXOs + validators + subnets
│   ├── diff.go
│   ├── staker.go
│   └── expiry.go
├── stakeable/             # ⭐ Locked outputs
│   ├── lockout.go
│   └── lockin.go
├── utxo/                  # UTXO verification
│   └── verifier.go
└── validators/            # Validator set management
```

### 4.3 Transaction Types

#### Import Transaction

**File**: `vms/platformvm/txs/import_tx.go`

```go
type ImportTx struct {
    BaseTx                          // Ins, Outs
    SourceChain    ids.ID           // X-Chain or C-Chain
    ImportedInputs []*avax.TransferableInput
}
```

**Execution**: `vms/platformvm/txs/executor/standard_tx_executor.go`

```go
func (e *standardTxExecutor) ImportTx(tx *txs.ImportTx) error {
    // 1. Get UTXOs from shared memory
    utxoIDs := make([][]byte, len(tx.ImportedInputs))
    allUTXOBytes, _ := e.backend.Ctx.SharedMemory.Get(tx.SourceChain, utxoIDs)
    
    // 2. Verify and consume
    avax.Consume(e.state, tx.Ins)      // Consume P-Chain UTXOs (for fees)
    avax.Produce(e.state, txID, tx.Outs)  // Produce new P-Chain UTXOs
    
    // 3. Remove from shared memory on accept
    e.atomicRequests = map[ids.ID]*atomic.Requests{
        tx.SourceChain: {
            RemoveRequests: utxoIDs,
        },
    }
}
```

#### Export Transaction

**File**: `vms/platformvm/txs/export_tx.go`

```go
type ExportTx struct {
    BaseTx                             // Ins, Outs
    DestinationChain ids.ID
    ExportedOutputs  []*avax.TransferableOutput
}
```

**Execution**: Similar to X-Chain, creates Elements and Puts to shared memory.

### 4.4 Stakeable Locked Outputs

**Location**: `vms/platformvm/stakeable/`

```go
// LockOut wraps an output with time-lock for staking
type LockOut struct {
    Locktime uint64                  // Unlock timestamp
    TransferableOut verify.State     // Inner output
}

// LockIn wraps an input for spending locked outputs
type LockIn struct {
    Locktime uint64
    TransferableIn verify.State
}
```

**Usage**: When staking, UTXOs are locked until `stakeEndTime`. After that, they become spendable.

### 4.5 RPC API

**File**: `vms/platformvm/service.go`

**Endpoint**: `/ext/bc/P` or `/ext/P`

#### Key Methods

```go
// GetUTXOs - Same pattern as X-Chain
func (s *Service) GetUTXOs(args *api.GetUTXOsArgs, response *api.GetUTXOsReply) error {
    sourceChain := args.SourceChain
    
    if sourceChain == "" || sourceChain == s.vm.ctx.ChainID {
        // Local P-Chain UTXOs
        utxos, _, _, _ = avax.GetPaginatedUTXOs(s.vm.state, addrs, ...)
    } else {
        // Atomic UTXOs from X or C-Chain
        utxos, _, _, _ = avax.GetAtomicUTXOs(
            s.vm.ctx.SharedMemory,
            txs.Codec,
            sourceChainID,
            addrs,
            ...
        )
    }
}

// GetBalance (DEPRECATED) - Returns categorized balances
func (s *Service) GetBalance(args *GetBalanceArgs, response *GetBalanceReply) error {
    // Categories:
    // - unlockeds: Available now
    // - lockedStakeables: Locked but stakeable
    // - lockedNotStakeables: Time-locked, not yet stakeable
}
```

**Methods**:
- `platform.getUTXOs` - Get UTXOs (local or atomic)
- `platform.getBalance` - DEPRECATED, categorized balance
- `platform.getStake` - Staking info for address
- `platform.getCurrentValidators` - Current validator set
- `platform.getPendingValidators` - Pending validators
- `platform.issueTx` - Submit transaction
- `platform.getAtomicTx` - Get atomic transaction
- Plus 50+ more methods

---

## 5. C-Chain Atomic Transactions

### 5.1 Overview

**Location**: `avalanchego/graft/coreth/plugin/evm/atomic/`

C-Chain is an EVM fork (Coreth = "Core Ethereum") but extends it with atomic transactions to bridge UTXO ↔ EVM balance.

**Key Challenge**: Convert between:
- **X/P-Chain**: nanoAVAX (10^-9 AVAX)
- **C-Chain**: Wei (10^-18 AVAX)

**Solution**: `X2CRate = 1,000,000,000` (1 billion)

### 5.2 Directory Structure

```
graft/coreth/plugin/evm/atomic/
├── tx.go                  # ⭐ X2CRate, EVMInput/Output
├── import_tx.go           # ⭐ Import: UTXO → EVM balance
├── export_tx.go           # ⭐ Export: EVM balance → UTXO
├── codec.go               # Type registration
├── params.go              # Gas constants
├── metadata.go            # Transaction metadata
├── status.go              # Processing/Accepted/Unknown
├── state/                 # ⭐ Atomic state management
│   ├── atomic_backend.go  # Interface to shared memory
│   ├── atomic_trie.go     # Merkle trie for atomic ops
│   ├── atomic_repository.go  # Historical queries
│   └── atomic_state.go    # State tracking
├── sync/                  # State sync for atomic data
├── txpool/                # Atomic transaction mempool
│   └── mempool.go
└── vm/                    # ⭐ VM integration
    ├── vm.go              # Atomic VM extension
    ├── api.go             # ⭐ avax.* RPC methods
    ├── tx_semantic_verifier.go  # Verification
    └── block_extension.go
```

### 5.3 X2CRate Conversion

**File**: `graft/coreth/plugin/evm/atomic/tx.go`

```go
const X2CRateUint64 = 1_000_000_000  // 1 billion

var X2CRate = uint256.NewInt(X2CRateUint64)

// X/P chains: 1 AVAX = 10^9 nAVAX
// C-Chain:    1 AVAX = 10^18 Wei
//
// Conversion:
//   C_wei = X_nanoavax × 10^9
//   X_nanoavax = C_wei / 10^9
```

**Also**: `avalanchego/wallet/chain/c/builder.go` uses `avaxConversionRate = 1_000_000_000`

### 5.4 Import Transaction (UTXO → EVM)

**File**: `graft/coreth/plugin/evm/atomic/import_tx.go`

```go
type UnsignedImportTx struct {
    NetworkID      uint32
    BlockchainID   ids.ID
    SourceChain    ids.ID                      // P or X-Chain
    ImportedInputs []*avax.TransferableInput   // UTXOs from shared memory
    Outs           []EVMOutput                 // EVM addresses to credit
}

type EVMOutput struct {
    Address common.Address  // EVM address (0x...)
    Amount  uint64          // nAVAX amount
    AssetID ids.ID          // Asset (AVAX or ANT)
}
```

**Process**: `import_tx.go:EVMStateTransfer()`

```go
func (tx *UnsignedImportTx) EVMStateTransfer(ctx *snow.Context, state StateDB) error {
    for _, out := range tx.Outs {
        if out.AssetID == ctx.AVAXAssetID {
            // Convert nAVAX to Wei
            amount := new(uint256.Int).Mul(
                uint256.NewInt(out.Amount),  // nAVAX
                X2CRate,                      // × 1e9
            )
            // Credit EVM balance
            state.AddBalance(out.Address, amount)
        } else {
            // Multi-coin balance for ANTs
            amount := new(big.Int).SetUint64(out.Amount)
            state.AddBalanceMultiCoin(out.Address, common.Hash(out.AssetID), amount)
        }
    }
    return nil
}
```

**Verification**: Checks UTXOs exist in shared memory before acceptance.

### 5.5 Export Transaction (EVM → UTXO)

**File**: `graft/coreth/plugin/evm/atomic/export_tx.go`

```go
type UnsignedExportTx struct {
    NetworkID        uint32
    BlockchainID     ids.ID
    DestinationChain ids.ID                       // P or X-Chain
    Ins              []EVMInput                    // EVM addresses to debit
    ExportedOutputs  []*avax.TransferableOutput   // UTXOs to create
}

type EVMInput struct {
    Address common.Address  // EVM address
    Amount  uint64          // nAVAX amount
    AssetID ids.ID
    Nonce   uint64          // ⭐ EVM nonce required!
}
```

**Process**: `export_tx.go:EVMStateTransfer()`

```go
func (tx *UnsignedExportTx) EVMStateTransfer(ctx *snow.Context, state StateDB) error {
    for _, from := range tx.Ins {
        if from.AssetID == ctx.AVAXAssetID {
            // Convert nAVAX to Wei
            amount := new(uint256.Int).Mul(
                uint256.NewInt(from.Amount),  // nAVAX
                X2CRate,                       // × 1e9
            )
            
            // Check balance
            if state.GetBalance(from.Address).Cmp(amount) < 0 {
                return ErrInsufficientFunds
            }
            
            // Debit EVM balance
            state.SubBalance(from.Address, amount)
        }
        
        // ⭐ Verify and increment nonce
        if state.GetNonce(from.Address) != from.Nonce {
            return ErrInvalidNonce
        }
        state.SetNonce(from.Address, from.Nonce+1)
    }
    return nil
}
```

**Critical**: Export requires correct EVM nonce, just like regular EVM transactions.

### 5.6 Atomic State Management

**Location**: `graft/coreth/plugin/evm/atomic/state/`

**File**: `atomic_backend.go`

```go
type AtomicBackend interface {
    ApplyToSharedMemory(lastAcceptedBlock uint64) error
    // ...
}
```

**Purpose**: Batches atomic operations in a trie, then applies to shared memory in bulk. This allows C-Chain to sync atomic state efficiently.

**File**: `atomic_trie.go` - Merkle trie tracking atomic operations per block.

### 5.7 RPC API

**File**: `graft/coreth/plugin/evm/atomic/vm/api.go`

**Namespace**: `avax.*` (separate from `eth_*`)

**Endpoint**: `/ext/bc/C/avax`

```go
type AvaxAPI struct { vm *VM }

// GetUTXOs - ⭐ REQUIRES sourceChain parameter
func (api *AvaxAPI) GetUTXOs(args *api.GetUTXOsArgs, reply *api.GetUTXOsReply) error {
    if args.SourceChain == "" {
        return errNoSourceChain  // ⚠️ Not optional on C-Chain
    }
    
    sourceChainID, _ := api.vm.Ctx.BCLookup.Lookup(args.SourceChain)
    
    utxos, _, _, _ := avax.GetAtomicUTXOs(
        api.vm.Ctx.SharedMemory,
        atomic.Codec,           // ⭐ C-Chain atomic codec
        sourceChainID,
        addrs,
        ...
    )
    // Returns UTXOs in C-Chain's inbound from sourceChain
}

// IssueTx - Submit atomic transaction
func (api *AvaxAPI) IssueTx(args *api.FormattedTx, reply *api.JSONTxID) error

// GetAtomicTx - Get atomic transaction by ID
func (api *AvaxAPI) GetAtomicTx(args *api.GetTxArgs, reply *api.FormattedTx) error

// GetAtomicTxStatus - Check status
func (api *AvaxAPI) GetAtomicTxStatus(args *api.JSONTxID, reply *Status) error
```

**Methods**:
- `avax.getUTXOs` - ⭐ MUST specify `sourceChain`
- `avax.issueTx` - Submit ImportTx or ExportTx
- `avax.getAtomicTx` - Get transaction
- `avax.getAtomicTxStatus` - Processing/Accepted/Unknown

**Note**: No `avax.getBalance` - use standard `eth_getBalance` for EVM balances.

---

## 6. Atomic Transaction Flow

### 6.1 Complete Export → Import Sequence

#### Example: P-Chain → C-Chain (100 AVAX)

```
┌──────────────────────────────────────────────────────────────┐
│ STEP 1: Export on P-Chain                                    │
├──────────────────────────────────────────────────────────────┤
│ User creates ExportTx:                                       │
│   Ins:              [UTXO_A: 100 AVAX]  (P-Chain UTXO)      │
│   Outs:             [UTXO_B: 99 AVAX]   (Change, P-Chain)   │
│   DestinationChain: C-Chain_ID                              │
│   ExportedOutputs:  [UTXO_C: 1 AVAX]    (For C-Chain)       │
│                                                              │
│ P-Chain verifies:                                            │
│   ✓ Signatures valid                                         │
│   ✓ Same subnet (P-C are on Primary Network)                │
│   ✓ Flow: 100 = 99 + 1 + 0 (fees waived in example)         │
│                                                              │
│ On block acceptance:                                         │
│   1. Consume UTXO_A on P-Chain                               │
│   2. Produce UTXO_B (change) on P-Chain                      │
│   3. Create atomic.Element:                                  │
│        Key:    UTXO_C.ID()                                   │
│        Value:  serialize(UTXO_C)                             │
│        Traits: [recipient_address]                           │
│   4. SharedMemory.Apply({                                    │
│        C-Chain: {PutRequests: [Element(UTXO_C)]}             │
│      })                                                       │
│   5. UTXO_C now in C-Chain's INBOUND state                   │
└──────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────┐
│ STEP 2: Query Atomic UTXOs on C-Chain                       │
├──────────────────────────────────────────────────────────────┤
│ User calls:                                                  │
│   avax.getUTXOs(                                             │
│     sourceChain: "P",                                        │
│     addresses: ["C-avax1..."]                                │
│   )                                                          │
│                                                              │
│ C-Chain executes:                                            │
│   SharedMemory.Indexed(                                      │
│     peerChainID: P-Chain_ID,                                 │
│     traits: [address_bytes]                                  │
│   )                                                          │
│   → Returns [UTXO_C] from C-Chain's inbound from P-Chain    │
└──────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────┐
│ STEP 3: Import on C-Chain                                   │
├──────────────────────────────────────────────────────────────┤
│ User creates ImportTx:                                       │
│   SourceChain:     P-Chain_ID                                │
│   ImportedInputs:  [UTXO_C: 1 AVAX = 10^9 nAVAX]            │
│   Outs:            [EVMOutput{                               │
│                       Address: 0x1234...,                    │
│                       Amount: 10^9 nAVAX                     │
│                     }]                                       │
│                                                              │
│ C-Chain verifies:                                            │
│   1. Check UTXO_C exists in shared memory:                   │
│      SharedMemory.Get(P-Chain, [UTXO_C.ID()])                │
│   2. Verify signatures on ImportedInputs                     │
│   3. Check no conflicting blocks in processing               │
│                                                              │
│ On block acceptance:                                         │
│   1. SharedMemory.Apply({                                    │
│        P-Chain: {RemoveRequests: [UTXO_C.ID()]}              │
│      })                                                       │
│   2. UTXO_C removed from C-Chain's inbound                   │
│   3. EVMStateTransfer():                                     │
│        amount_wei = 10^9 × 10^9 = 10^18 wei (1 AVAX)        │
│        state.AddBalance(0x1234..., 10^18 wei)                │
│   4. EVM balance credited!                                   │
└──────────────────────────────────────────────────────────────┘
```

### 6.2 Complete Import → Export Sequence (Reverse)

#### Example: C-Chain → X-Chain

```
┌──────────────────────────────────────────────────────────────┐
│ STEP 1: Export from C-Chain                                 │
├──────────────────────────────────────────────────────────────┤
│ User creates ExportTx:                                       │
│   DestinationChain: X-Chain_ID                               │
│   Ins:              [EVMInput{                               │
│                       Address: 0x5678...,                    │
│                       Amount: 5×10^8 nAVAX (0.5 AVAX),       │
│                       Nonce: 42                              │
│                     }]                                       │
│   ExportedOutputs:  [TransferableOutput{                     │
│                       Amount: 5×10^8 nAVAX,                  │
│                       Owners: [X-avax1...]                   │
│                     }]                                       │
│                                                              │
│ C-Chain verifies:                                            │
│   ✓ Nonce matches: state.GetNonce(0x5678...) == 42          │
│   ✓ Balance: state.GetBalance(0x5678...) >= 5×10^17 wei     │
│                                                              │
│ On block acceptance:                                         │
│   1. EVMStateTransfer():                                     │
│        amount_wei = 5×10^8 × 10^9 = 5×10^17 wei              │
│        state.SubBalance(0x5678..., 5×10^17 wei)              │
│        state.SetNonce(0x5678..., 43)                         │
│   2. Create UTXO_D in shared memory                          │
│   3. SharedMemory.Apply({                                    │
│        X-Chain: {PutRequests: [Element(UTXO_D)]}             │
│      })                                                       │
└──────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────┐
│ STEP 2: Import on X-Chain                                   │
├──────────────────────────────────────────────────────────────┤
│ User calls: avm.getUTXOs(sourceChain: "C", ...)             │
│ → Returns [UTXO_D]                                           │
│                                                              │
│ User creates ImportTx on X-Chain:                            │
│   SourceChain:     C-Chain_ID                                │
│   ImportedInputs:  [UTXO_D]                                  │
│   Outs:            [X-Chain outputs]                         │
│                                                              │
│ On block acceptance:                                         │
│   SharedMemory.Apply({                                       │
│     C-Chain: {RemoveRequests: [UTXO_D.ID()]}                 │
│   })                                                         │
│   Produce UTXOs on X-Chain                                   │
└──────────────────────────────────────────────────────────────┘
```

### 6.3 Critical Verification Steps

#### On Export (Any Chain)

1. ✓ Verify transaction structure (well-formed)
2. ✓ Verify signatures on inputs
3. ✓ Verify destination chain is in same subnet
4. ✓ Flow check: `inputs >= outputs + exported + fees`
5. ✓ For C-Chain: Verify nonce, check EVM balance

#### On Import (Any Chain)

1. ✓ Verify UTXOs exist in shared memory: `SharedMemory.Get()`
2. ✓ Verify no processing ancestor conflicts (same UTXO)
3. ✓ Verify signatures on ImportedInputs
4. ✓ Verify source chain is in same subnet
5. ✓ Flow check: `imported + inputs >= outputs + fees`

**Critical Race Condition Check**:

```
Blocks:
  L  (last accepted)
  |
  B1 (processing, spends atomic UTXO_X)
  |
  B2 (verifying, also tries to spend UTXO_X)

If B2 only checks shared memory, it sees UTXO_X (B1 not accepted yet).
But B2 conflicts with B1 → B2 is invalid.

Solution: Check processing ancestors for conflicts.
```

---

## 7. RPC APIs & Endpoints

### 7.1 Common API Pattern

All three chains (X, P, C) support:

```json
{
  "jsonrpc": "2.0",
  "method": "CHAIN.getUTXOs",
  "params": {
    "addresses": ["CHAIN-hrp1..."],
    "sourceChain": "OTHER_CHAIN",  // Optional on X/P, required on C
    "limit": 1024,
    "startIndex": {
      "address": "CHAIN-hrp1...",
      "utxo": "UTXO_ID"
    },
    "encoding": "hex"
  }
}
```

### 7.2 X-Chain RPCs

**Endpoint**: `/ext/bc/X`  
**Service**: `vms/avm/service.go`  
**Docs**: `vms/avm/service.md`

#### Key Methods

| Method | Description | Notes |
|--------|-------------|-------|
| `avm.getUTXOs` | Get UTXOs for addresses | `sourceChain` optional |
| `avm.getBalance` | Get balance for asset | DEPRECATED |
| `avm.getAllBalances` | All asset balances | DEPRECATED |
| `avm.issueTx` | Submit transaction | Returns txID |
| `avm.getAtomicTx` | Get atomic transaction | By txID |
| `avm.getAtomicTxStatus` | Get tx status | Processing/Accepted/Unknown |
| `avm.getTx` | Get any transaction | By txID |
| `avm.getTxStatus` | Get tx status | For non-atomic too |
| `avm.getAssetDescription` | Get asset info | For ANTs |

### 7.3 P-Chain RPCs

**Endpoint**: `/ext/bc/P` or `/ext/P`  
**Service**: `vms/platformvm/service.go`  
**Docs**: `vms/platformvm/service.md`

#### Key Methods

| Method | Description | Notes |
|--------|-------------|-------|
| `platform.getUTXOs` | Get UTXOs for addresses | `sourceChain` optional |
| `platform.getBalance` | Get categorized balance | DEPRECATED |
| `platform.getStake` | Get staking info | locked/unlocked/staked |
| `platform.issueTx` | Submit transaction | Returns txID |
| `platform.getAtomicTx` | Get atomic transaction | By txID |
| `platform.getTx` | Get any transaction | By txID |
| `platform.getTxStatus` | Get tx status | Processing/Committed/etc |
| `platform.getCurrentValidators` | Get current validators | For primary or subnet |
| `platform.getPendingValidators` | Get pending validators | Not yet started |
| `platform.getCurrentSupply` | Total AVAX supply | Includes burned fees |
| `platform.getHeight` | Current block height | |
| `platform.getBlockchains` | List blockchains | In subnet |
| `platform.getSubnets` | List subnets | |
| Plus 40+ more methods | Validator/subnet management | |

### 7.4 C-Chain RPCs

**Two Namespaces**:

#### Standard EVM (`eth_*`)

**Endpoint**: `/ext/bc/C/rpc`

Standard Ethereum JSON-RPC:
- `eth_getBalance` - EVM account balance (in Wei)
- `eth_getTransactionCount` - Nonce
- `eth_sendRawTransaction` - Submit EVM transaction
- `eth_getTransactionByHash` - Get transaction
- `eth_getTransactionReceipt` - Get receipt
- Plus all standard Ethereum methods

#### Avalanche Atomic (`avax.*`)

**Endpoint**: `/ext/bc/C/avax`  
**Service**: `graft/coreth/plugin/evm/atomic/vm/api.go`

| Method | Description | Notes |
|--------|-------------|-------|
| `avax.getUTXOs` | Get atomic UTXOs | ⚠️ `sourceChain` REQUIRED |
| `avax.issueTx` | Submit atomic tx | ImportTx or ExportTx |
| `avax.getAtomicTx` | Get atomic transaction | Returns formatted tx |
| `avax.getAtomicTxStatus` | Get status | Processing/Accepted/Unknown |
| `avax.exportAVAX` | Helper to create ExportTx | Convenience method |
| `avax.importAVAX` | Helper to create ImportTx | Convenience method |
| `avax.export` | Export any asset | Multi-asset support |
| `avax.import` | Import any asset | Multi-asset support |

**Critical Difference**: C-Chain has NO `avax.getBalance` for atomic UTXOs. Use:
- `eth_getBalance` for on-chain EVM balance
- `avax.getUTXOs` + manual sum for importable balance

### 7.5 Common Parameters

#### `GetUTXOsArgs`

```go
type GetUTXOsArgs struct {
    Addresses  []string         // Bech32 addresses (e.g., "P-avax1...")
    SourceChain string          // Optional on X/P, required on C
    Limit      uint32           // Max UTXOs to return (default 1024)
    StartIndex struct {         // Pagination cursor
        Address string
        UTXO    string
    }
    Encoding   formatting.Encoding  // "hex", "hexnc", "cb58"
}
```

#### `GetUTXOsReply`

```go
type GetUTXOsReply struct {
    UTXOs    []string  // Encoded UTXO bytes
    EndIndex struct {  // Next page cursor
        Address string
        UTXO    string
    }
    Encoding formatting.Encoding
}
```

---

## 8. Balance Calculation

### 8.1 UTXO-Based Balance (X-Chain, P-Chain)

**Formula**: `Balance = Σ(UTXO.Amount)` for all UTXOs owned by address(es).

**Implementation**: `vms/components/avax/utxo_fetching.go`

```go
func GetBalance(db UTXOReader, addrs set.Set[ids.ShortID]) (uint64, error) {
    utxos, err := GetAllUTXOs(db, addrs)
    if err != nil {
        return 0, err
    }
    
    balance := uint64(0)
    for _, utxo := range utxos {
        if out, ok := utxo.Out.(Amounter); ok {
            balance, _ = safemath.Add(out.Amount(), balance)
        }
    }
    return balance, nil
}
```

**Notes**:
- Must iterate ALL UTXOs for accurate balance
- Time-locked UTXOs (locktime > now) may be excluded
- Staked UTXOs are separate category

### 8.2 P-Chain Balance Categories

**File**: `vms/platformvm/service.go:GetBalance()`

```go
type GetBalanceReply struct {
    Balance              uint64            // Total (DEPRECATED)
    Unlocked             uint64            // DEPRECATED
    LockedStakeable      uint64            // DEPRECATED
    LockedNotStakeable   uint64            // DEPRECATED
    Balances             map[ids.ID]uint64 // Per asset
    Unlockeds            map[ids.ID]uint64 // Available now
    LockedStakeables     map[ids.ID]uint64 // Locked, but stakeable
    LockedNotStakeables  map[ids.ID]uint64 // Locked, not stakeable
}
```

**Classification Logic**:

```go
for _, utxo := range utxos {
    switch out := utxo.Out.(type) {
    case *secp256k1fx.TransferOutput:
        if out.Locktime <= currentTime {
            unlockeds[assetID] += amount
        } else {
            lockedNotStakeables[assetID] += amount
        }
    
    case *stakeable.LockOut:
        innerOut := out.TransferableOut.(*secp256k1fx.TransferOutput)
        if innerOut.Locktime > currentTime {
            // Inner locktime not passed: can't use yet
            lockedNotStakeables[assetID] += amount
        } else if out.Locktime <= currentTime {
            // Both locks passed: fully unlocked
            unlockeds[assetID] += amount
        } else {
            // Inner lock passed, outer not: stakeable
            lockedStakeables[assetID] += amount
        }
    }
}
```

**Extended Categories** (used by indexers):

| Category | Description | Location |
|----------|-------------|----------|
| `unlockedUnstaked` | Available for transfer | Local chain |
| `unlockedStaked` | Currently validating/delegating | Local chain |
| `lockedStakeable` | Time-locked but stakeable | Local chain |
| `lockedStaked` | Staked AND time-locked | Local chain |
| `lockedNotStakeable` | Simple time-lock | Local chain |
| `pendingStaked` | Validator not started yet | Local chain |
| `atomicMemoryUnlocked` | Exported, not imported | Shared memory |
| `atomicMemoryLocked` | Exported, time-locked | Shared memory |

### 8.3 C-Chain Balance

**On-Chain EVM Balance**:

Use standard Ethereum RPC:

```bash
curl -X POST --data '{
  "jsonrpc": "2.0",
  "method": "eth_getBalance",
  "params": ["0x1234...", "latest"],
  "id": 1
}' http://localhost:9650/ext/bc/C/rpc
```

Returns: Balance in Wei (10^-18 AVAX)

**Importable Balance** (UTXOs in shared memory):

```bash
curl -X POST --data '{
  "jsonrpc": "2.0",
  "method": "avax.getUTXOs",
  "params": {
    "sourceChain": "P",
    "addresses": ["C-avax1..."]
  }
}' http://localhost:9650/ext/bc/C/avax
```

Then sum UTXO amounts client-side and convert to Wei:

```go
importableBalance := uint64(0)
for _, utxo := range atomicUTXOs {
    importableBalance += utxo.Out.Amount()  // nAVAX
}
importableBalanceWei := new(big.Int).Mul(
    new(big.Int).SetUint64(importableBalance),
    big.NewInt(1e9),  // X2CRate
)
```

---

## 9. Key Data Structures

### 9.1 UTXO

**File**: `vms/components/avax/utxo.go`

```go
type UTXO struct {
    UTXOID          // Unique identifier
    Asset  Asset    // Which asset
    Out    verify.State  // The output
}

type UTXOID struct {
    TxID        ids.ID  // Transaction that created it
    OutputIndex uint32  // Index in transaction outputs
}

func (utxo *UTXO) InputID() ids.ID {
    return utxo.UTXOID.InputID()
}
```

### 9.2 TransferableInput/Output

**File**: `vms/components/avax/transferables.go`

```go
type TransferableInput struct {
    UTXOID              // Which UTXO to spend
    Asset               // Which asset
    In TransferableIn   // How to spend it
}

type TransferableOutput struct {
    Asset                // Which asset
    Out TransferableOut  // How to own it
}
```

### 9.3 secp256k1fx Types

**Location**: `vms/secp256k1fx/`

```go
// TransferOutput defines ownership
type TransferOutput struct {
    Amt          uint64        // Amount
    OutputOwners OutputOwners  // Who can spend
}

// OutputOwners allows multisig + timelock
type OutputOwners struct {
    Locktime  uint64          // Unix timestamp
    Threshold uint32          // M-of-N threshold
    Addrs     []ids.ShortID   // N addresses (sorted)
}

// TransferInput spends a TransferOutput
type TransferInput struct {
    Amt               uint64    // Must match output
    SignatureIndices  []uint32  // Which addresses are signing (0-indexed)
}

// Credential provides signatures
type Credential struct {
    Sigs [][65]byte  // Secp256k1 signatures
}
```

**Example**: 2-of-3 multisig

```go
out := &secp256k1fx.TransferOutput{
    Amt: 100,
    OutputOwners: secp256k1fx.OutputOwners{
        Locktime:  0,
        Threshold: 2,  // Need 2 signatures
        Addrs:     []ids.ShortID{addr1, addr2, addr3},  // 3 possible signers
    },
}

// To spend, provide 2 signatures
in := &secp256k1fx.TransferInput{
    Amt:               100,
    SignatureIndices:  []uint32{0, 2},  // addr1 and addr3 signing
}

cred := &secp256k1fx.Credential{
    Sigs: [][65]byte{sig_addr1, sig_addr3},
}
```

### 9.4 Atomic Element

**File**: `chains/atomic/shared_memory.go`

```go
type Element struct {
    Key    []byte   // UTXO ID
    Value  []byte   // Serialized UTXO
    Traits [][]byte // Addresses for indexing
}

type Requests struct {
    RemoveRequests [][]byte    // Keys to remove
    PutRequests    []*Element  // Elements to add
}
```

**Usage in Export**:

```go
// Create UTXO
utxo := &avax.UTXO{
    UTXOID: avax.UTXOID{TxID: txID, OutputIndex: 0},
    Asset:  avax.Asset{ID: avaxAssetID},
    Out:    transferOutput,
}

// Serialize
utxoBytes, _ := codec.Marshal(utxo)

// Extract address traits
addresses := transferOutput.Owners.Addrs
traits := make([][]byte, len(addresses))
for i, addr := range addresses {
    traits[i] = addr[:]
}

// Create element
elem := &atomic.Element{
    Key:    utxo.InputID().Bytes(),
    Value:  utxoBytes,
    Traits: traits,
}

// Put to shared memory
atomicRequests = map[ids.ID]*atomic.Requests{
    destinationChain: {
        PutRequests: []*atomic.Element{elem},
    },
}
```

### 9.5 BaseTx

**File**: `vms/components/avax/base_tx.go`

```go
type BaseTx struct {
    Metadata         // Cached bytes for fast hashing
    NetworkID uint32
    BlockchainID ids.ID
    Ins  []*TransferableInput   // UTXOs to consume
    Outs []*TransferableOutput  // UTXOs to create
    Memo []byte                 // Optional memo
}
```

All transaction types embed `BaseTx`:
- X-Chain: `BaseTx`, `ImportTx`, `ExportTx`, `CreateAssetTx`, `OperationTx`
- P-Chain: `BaseTx`, `ImportTx`, `ExportTx`, `AddValidatorTx`, etc.

---

## 10. File System Navigation

### 10.1 Must-Read Files

#### Shared Memory

```
✅ chains/atomic/README.md                    - START HERE
✅ chains/atomic/shared_memory.go              - Interface
✅ chains/atomic/state.go                      - Inbound/outbound
✅ chains/atomic/memory.go                     - Locking
```

#### Core UTXO

```
✅ vms/components/avax/utxo.go                 - UTXO struct
✅ vms/components/avax/utxo_fetching.go        - GetBalance, GetPaginatedUTXOs
✅ vms/components/avax/atomic_utxos.go         - GetAtomicUTXOs
✅ vms/components/avax/transferables.go        - Inputs/Outputs
✅ vms/components/avax/base_tx.go              - BaseTx
✅ vms/components/avax/flow_checker.go         - Verify ins >= outs
```

#### X-Chain

```
✅ vms/avm/service.go                          - RPC implementation
✅ vms/avm/service.md                          - API docs
✅ vms/avm/txs/import_tx.go                    - Import structure
✅ vms/avm/txs/export_tx.go                    - Export structure
✅ vms/avm/txs/executor/executor.go            - Execution logic
```

#### P-Chain

```
✅ vms/platformvm/service.go                   - RPC implementation (2152 lines)
✅ vms/platformvm/service.md                   - API docs (2198 lines)
✅ vms/platformvm/txs/import_tx.go             - Import structure
✅ vms/platformvm/txs/export_tx.go             - Export structure
✅ vms/platformvm/txs/executor/standard_tx_executor.go  - Import/Export execution
✅ vms/platformvm/stakeable/lockout.go         - Staking locks
```

#### C-Chain Atomic

```
✅ graft/coreth/plugin/evm/atomic/tx.go        - X2CRate, EVMInput/Output
✅ graft/coreth/plugin/evm/atomic/import_tx.go - Import: UTXO → EVM
✅ graft/coreth/plugin/evm/atomic/export_tx.go - Export: EVM → UTXO
✅ graft/coreth/plugin/evm/atomic/vm/api.go    - avax.* RPCs
✅ graft/coreth/plugin/evm/atomic/vm/tx_semantic_verifier.go  - Verification
✅ graft/coreth/plugin/evm/atomic/state/atomic_backend.go  - State sync
```

#### secp256k1fx

```
✅ vms/secp256k1fx/transfer_output.go          - Outputs
✅ vms/secp256k1fx/transfer_input.go           - Inputs
✅ vms/secp256k1fx/output_owners.go            - Multisig + timelock
✅ vms/secp256k1fx/credential.go               - Signatures
```

### 10.2 Complete Directory Tree

```
avalanchego/
├── chains/
│   └── atomic/                    ⭐ SHARED MEMORY
│       ├── README.md
│       ├── shared_memory.go
│       ├── memory.go
│       ├── state.go
│       ├── codec.go
│       ├── prefixes.go
│       ├── writer.go
│       └── gsharedmemory/         # gRPC for remote VMs
│
├── vms/
│   ├── components/
│   │   └── avax/                  ⭐ CORE UTXO COMPONENTS
│   │       ├── utxo.go
│   │       ├── utxo_id.go
│   │       ├── utxo_state.go
│   │       ├── utxo_fetching.go
│   │       ├── atomic_utxos.go
│   │       ├── transferables.go
│   │       ├── base_tx.go
│   │       ├── flow_checker.go
│   │       ├── state.go
│   │       └── addresses.go
│   │
│   ├── secp256k1fx/               ⭐ SIGNATURE/OWNERSHIP
│   │   ├── transfer_output.go
│   │   ├── transfer_input.go
│   │   ├── output_owners.go
│   │   ├── credential.go
│   │   └── keychain.go
│   │
│   ├── avm/                       ⭐ X-CHAIN
│   │   ├── vm.go
│   │   ├── service.go
│   │   ├── service.md
│   │   ├── client.go
│   │   ├── txs/
│   │   │   ├── base_tx.go
│   │   │   ├── import_tx.go
│   │   │   ├── export_tx.go
│   │   │   ├── create_asset_tx.go
│   │   │   ├── operation_tx.go
│   │   │   ├── executor/
│   │   │   │   ├── executor.go
│   │   │   │   └── semantic_verifier.go
│   │   │   └── codec.go
│   │   ├── state/
│   │   │   ├── state.go
│   │   │   └── diff.go
│   │   ├── utxo/
│   │   │   └── spender.go
│   │   └── block/
│   │
│   └── platformvm/                ⭐ P-CHAIN
│       ├── vm.go
│       ├── service.go
│       ├── service.md
│       ├── client.go
│       ├── txs/
│       │   ├── base_tx.go
│       │   ├── import_tx.go
│       │   ├── export_tx.go
│       │   ├── add_validator_tx.go
│       │   ├── add_delegator_tx.go
│       │   ├── add_permissionless_validator_tx.go
│       │   ├── add_permissionless_delegator_tx.go
│       │   ├── create_subnet_tx.go
│       │   ├── create_chain_tx.go
│       │   ├── ... (15+ more)
│       │   ├── executor/
│       │   │   ├── standard_tx_executor.go
│       │   │   ├── atomic_tx_executor.go
│       │   │   ├── proposal_tx_executor.go
│       │   │   └── staker_tx_verification.go
│       │   └── fee/
│       ├── state/
│       │   ├── state.go
│       │   ├── diff.go
│       │   ├── staker.go
│       │   └── expiry.go
│       ├── stakeable/
│       │   ├── lockout.go
│       │   └── lockin.go
│       ├── utxo/
│       │   └── verifier.go
│       └── validators/
│
├── graft/coreth/                  ⭐ C-CHAIN (EVM)
│   └── plugin/evm/
│       ├── vm.go
│       └── atomic/                ⭐ ATOMIC TRANSACTIONS
│           ├── tx.go
│           ├── import_tx.go
│           ├── export_tx.go
│           ├── codec.go
│           ├── params.go
│           ├── metadata.go
│           ├── status.go
│           ├── state/
│           │   ├── atomic_backend.go
│           │   ├── atomic_trie.go
│           │   ├── atomic_repository.go
│           │   └── atomic_state.go
│           ├── sync/
│           ├── txpool/
│           │   └── mempool.go
│           └── vm/
│               ├── vm.go
│               ├── api.go
│               ├── tx_semantic_verifier.go
│               └── block_extension.go
│
├── wallet/                        ⭐ SDK / REFERENCE
│   ├── chain/
│   │   ├── x/
│   │   │   └── builder/
│   │   ├── p/
│   │   │   └── builder/
│   │   └── c/
│   │       ├── builder.go
│   │       └── backend.go         # Shows UTXO ↔ EVM conversion
│   └── subnet/primary/
│       ├── wallet.go
│       ├── common/
│       │   ├── utxos.go
│       │   └── spend.go
│       └── examples/              # 15+ working examples
│           ├── c-chain-import/
│           ├── c-chain-export/
│           ├── get-p-chain-balance/
│           └── get-x-chain-balance/
│
├── utils/
│   ├── constants/
│   │   ├── network_ids.go         # PlatformChainID = ids.Empty
│   │   └── vm_ids.go              # AVMID, PlatformVMID, EVMID
│   └── formatting/                # Bech32, CB58, Hex encoding
│
└── ids/                           # ID types (32-byte hashes, 20-byte addresses)
    ├── id.go
    ├── short_id.go
    └── aliases.go
```

---

## 11. Critical Constants & IDs

### 11.1 Chain IDs

#### Mainnet

```
P-Chain: 11111111111111111111111111111111LpoYY
X-Chain: 2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM
C-Chain: 2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5
```

#### Fuji Testnet

```
P-Chain: 11111111111111111111111111111111LpoYY  (same as mainnet!)
X-Chain: 2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm
C-Chain: yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp
```

**File**: `utils/constants/network_ids.go`

```go
const (
    MainnetID uint32 = 1
    FujiID    uint32 = 5
)

var (
    PlatformChainID = ids.Empty  // All zeros
)
```

**Note**: X-Chain and C-Chain IDs are network-specific. Fetch via `info.getBlockchainID` API or parse from genesis.

### 11.2 Asset IDs

#### AVAX Asset ID

- **Mainnet**: `FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z`
- **Fuji**: `U8iRqJoiJm8xZHAacmvYyZVwqQx6uDNtQeP3CQ6fcgQk3JqnK`

**File**: `genesis/genesis_*.go` for each network

**API**: `avm.getAssetDescription` or `info.getNetworkID` + hardcode

### 11.3 X2CRate (C-Chain Conversion)

**Value**: `1,000,000,000` (1 billion)

**Files**:
- `graft/coreth/plugin/evm/atomic/tx.go:48-61`
- `wallet/chain/c/builder.go:23-37`

```go
const X2CRateUint64 = 1_000_000_000

var X2CRate = uint256.NewInt(X2CRateUint64)
```

**Meaning**:
- 1 nAVAX (X/P-Chain) = 1 gWei (C-Chain)
- To convert: `C_wei = X_nanoavax × 10^9`

### 11.4 Codec Type IDs

#### C-Chain Atomic Codec

**File**: `graft/coreth/plugin/evm/atomic/codec.go`

```go
const (
    Version = 0
)

// Type IDs
typeID_UnsignedImportTx       = 0
typeID_UnsignedExportTx       = 1
typeID_secp256k1fx_TransferInput   = 5
typeID_secp256k1fx_TransferOutput  = 7
typeID_secp256k1fx_Credential      = 9
```

#### AVM/PlatformVM Codecs

Different codec managers, see:
- `vms/avm/txs/codec.go`
- `vms/platformvm/txs/codec.go`

### 11.5 Network HRP (Human-Readable Part)

**Bech32 Encoding**:

| Network | HRP | Example Address |
|---------|-----|-----------------|
| Mainnet | `avax` | `X-avax1...`, `P-avax1...`, `C-avax1...` |
| Fuji | `fuji` | `X-fuji1...`, `P-fuji1...`, `C-fuji1...` |
| Local | `local` | `X-local1...` |

**File**: `utils/constants/network_ids.go`

### 11.6 Gas Constants (C-Chain Atomic)

**File**: `graft/coreth/plugin/evm/atomic/tx.go`

```go
var (
    TxBytesGas   uint64 = 1
    EVMOutputGas uint64 = (20 + 8 + 32) * TxBytesGas = 60
    EVMInputGas  uint64 = (20 + 8 + 32 + 8) * TxBytesGas + 1000 = 1068
)

// Intrinsic gas for atomic tx (post-AP5)
const AtomicTxIntrinsicGas = 10_000
```

**Fee Calculation**:

```
TotalGas = IntrinsicGas + 
           len(txBytes) × TxBytesGas +
           len(Outs) × EVMOutputGas +
           len(Ins) × EVMInputGas

Fee = TotalGas × BaseFee
```

---

## 12. Common Patterns & Gotchas

### 12.1 Pattern: Fetching Atomic UTXOs

```go
import (
    "github.com/ava-labs/avalanchego/vms/components/avax"
    "github.com/ava-labs/avalanchego/ids"
)

func GetAtomicUTXOs(
    sharedMemory atomic.SharedMemory,
    codec codec.Manager,
    sourceChainID ids.ID,
    addresses []ids.ShortID,
) ([]*avax.UTXO, error) {
    addrSet := set.Of(addresses...)
    
    utxos, _, _, err := avax.GetAtomicUTXOs(
        sharedMemory,
        codec,
        sourceChainID,      // Where UTXOs came from
        addrSet,            // Filter by these addresses
        ids.ShortEmpty,     // Start from beginning
        ids.Empty,
        1024,               // Limit
    )
    return utxos, err
}
```

### 12.2 Pattern: Building Export Transaction (Simplified)

```go
// 1. Fetch UTXOs to spend
utxos, _ := GetAllUTXOs(state, myAddresses)

// 2. Select UTXOs to cover amount + fee
selectedUTXOs := SelectUTXOs(utxos, amount+fee)

// 3. Create inputs
ins := make([]*avax.TransferableInput, len(selectedUTXOs))
for i, utxo := range selectedUTXOs {
    ins[i] = &avax.TransferableInput{
        UTXOID: utxo.UTXOID,
        Asset:  utxo.Asset,
        In: &secp256k1fx.TransferInput{
            Amt: utxo.Out.Amount(),
            SignatureIndices: []uint32{0},  // First address
        },
    }
}

// 4. Create outputs
outs := []*avax.TransferableOutput{
    {  // Change
        Asset: avaxAsset,
        Out: &secp256k1fx.TransferOutput{
            Amt: totalInput - amount - fee,
            OutputOwners: secp256k1fx.OutputOwners{
                Threshold: 1,
                Addrs:     []ids.ShortID{myAddress},
            },
        },
    },
}

exportedOuts := []*avax.TransferableOutput{
    {  // Exported amount
        Asset: avaxAsset,
        Out: &secp256k1fx.TransferOutput{
            Amt: amount,
            OutputOwners: secp256k1fx.OutputOwners{
                Threshold: 1,
                Addrs:     []ids.ShortID{recipientAddress},
            },
        },
    },
}

// 5. Create transaction
tx := &txs.ExportTx{
    BaseTx: txs.BaseTx{
        NetworkID:    networkID,
        BlockchainID: xChainID,
        Ins:          ins,
        Outs:         outs,
    },
    DestinationChain: cChainID,
    ExportedOutputs:  exportedOuts,
}

// 6. Sign
// 7. Submit via RPC
```

### 12.3 Gotcha: C-Chain Balance vs Importable Balance

**Problem**: Users often confuse EVM balance with importable atomic UTXOs.

**Solution**:

```go
// On-chain EVM balance
evmBalance, _ := ethClient.BalanceAt(ctx, address, nil)  // Wei

// Importable balance from P-Chain
atomicUTXOs, _ := avaxAPI.GetUTXOs(ctx, "P", addresses)
importableNAVAX := uint64(0)
for _, utxo := range atomicUTXOs {
    importableNAVAX += utxo.Out.Amount()
}
importableWei := new(big.Int).Mul(
    new(big.Int).SetUint64(importableNAVAX),
    big.NewInt(1e9),
)

// Total available = evmBalance + importableWei
```

### 12.4 Gotcha: Shared Memory is Chain-Pair Specific

**Problem**: Misunderstanding that shared memory is global.

**Reality**: Each chain pair has isolated shared memory.

```
P↔X: Separate sharedID
P↔C: Separate sharedID
X↔C: Separate sharedID
```

**Implication**: To move X → P → C requires TWO atomic transactions:
1. Export X → P (Import on P)
2. Export P → C (Import on C)

Cannot go X → C directly if not in same subnet (Primary Network allows it).

### 12.5 Gotcha: C-Chain Export Requires Nonce

**Problem**: Forgetting that C-Chain exports are EVM-like.

```go
type EVMInput struct {
    Address common.Address
    Amount  uint64
    AssetID ids.ID
    Nonce   uint64  // ⚠️ Must be correct!
}
```

**Solution**: Query nonce before building ExportTx:

```go
nonce, _ := ethClient.NonceAt(ctx, address, nil)

evmInput := EVMInput{
    Address: address,
    Amount:  amount,
    AssetID: avaxAssetID,
    Nonce:   nonce,  // Use current nonce
}
```

### 12.6 Gotcha: Time-Locked UTXOs

**Problem**: Not checking locktime before spending.

```go
type TransferOutput struct {
    Amt          uint64
    OutputOwners OutputOwners
}

type OutputOwners struct {
    Locktime  uint64  // ⚠️ Unix timestamp
    Threshold uint32
    Addrs     []ids.ShortID
}
```

**Solution**: Filter by current time:

```go
currentTime := uint64(time.Now().Unix())

spendableUTXOs := []*avax.UTXO{}
for _, utxo := range allUTXOs {
    if out, ok := utxo.Out.(*secp256k1fx.TransferOutput); ok {
        if out.Locktime <= currentTime {
            spendableUTXOs = append(spendableUTXOs, utxo)
        }
    }
}
```

### 12.7 Gotcha: Processing Ancestor Conflicts

**Problem**: Accepting block that spends UTXO already spent by processing ancestor.

```
Chain:
  LastAccepted
  |
  B1 (processing, spends UTXO_X)
  |
  B2 (verifying, also spends UTXO_X)
```

**Solution**: Check processing ancestors, not just shared memory:

```go
// Pseudo-code
func VerifyImportTx(tx *ImportTx) error {
    // Check UTXO exists
    utxo, err := sharedMemory.Get(tx.SourceChain, utxoID)
    if err != nil {
        return err
    }
    
    // ⚠️ Check no processing block conflicts
    processingBlocks := GetProcessingBlocks()
    for _, block := range processingBlocks {
        if block.ConflictsWith(tx) {
            return ErrConflictingBlock
        }
    }
    
    return nil
}
```

---

## 13. Testing & Examples

### 13.1 Test Files

#### Shared Memory

- `chains/atomic/shared_memory_test.go`
- `chains/atomic/memory_test.go`

#### X-Chain

- `vms/avm/service_test.go` - RPC tests (2758 lines!)
- `vms/avm/txs/executor/executor_test.go`
- `vms/avm/vm_test.go`

#### P-Chain

- `vms/platformvm/service_test.go` - RPC tests (1631 lines)
- `vms/platformvm/txs/executor/standard_tx_executor_test.go`
- `vms/platformvm/vm_test.go`

#### C-Chain Atomic

- `graft/coreth/plugin/evm/atomic/vm/import_tx_test.go`
- `graft/coreth/plugin/evm/atomic/vm/export_tx_test.go`
- `graft/coreth/plugin/evm/atomic/state/atomic_backend_test.go`

### 13.2 Wallet Examples

**Location**: `wallet/subnet/primary/examples/`

#### C-Chain Import

**File**: `wallet/subnet/primary/examples/c-chain-import/main.go`

Shows complete flow:
1. Connect to node
2. Create wallet
3. Fetch atomic UTXOs from P-Chain
4. Build ImportTx
5. Sign and submit

#### C-Chain Export

**File**: `wallet/subnet/primary/examples/c-chain-export/main.go`

Shows:
1. Check EVM balance
2. Build ExportTx with nonce
3. Sign and submit
4. Wait for acceptance

#### Other Examples

- `get-p-chain-balance/` - Calculate P-Chain balance
- `get-x-chain-balance/` - Calculate X-Chain balance
- `add-validator/` - Stake on P-Chain
- `create-subnet/` - Create subnet
- Plus 10+ more

### 13.3 Running Tests

```bash
# Shared memory tests
cd chains/atomic
go test -v

# X-Chain tests
cd vms/avm
go test -v ./...

# P-Chain tests
cd vms/platformvm
go test -v ./...

# C-Chain atomic tests
cd graft/coreth/plugin/evm/atomic
go test -v ./...
```

### 13.4 Integration Test Network

**AvalancheGo** includes tools to spin up local test networks:

```bash
# Start 5-node local network
avalanchego --network-id=local --staking-enabled=false
```

**Alternative**: Use `avalanche-cli` for local Subnet-EVM testing.

---

## 14. FAQ

### Q1: Is Shared Memory a 4th chain?

**A**: **NO.** Shared Memory is a database primitive, not a blockchain. It creates pair-wise bidirectional channels between any two chains in the same subnet. Each pair (P↔X, P↔C, X↔C) has its own isolated shared database partition.

### Q2: How do UTXOs differ from EVM balances?

**A**:
- **UTXOs** (X/P-Chain): Discrete, unspent outputs. Balance = sum of UTXOs. Each UTXO is independent.
- **EVM Balance** (C-Chain): Account-based state. One balance per address. Modified by transactions.
- **Atomic transactions** bridge the two models via conversion.

### Q3: What is X2CRate and why does it exist?

**A**: X2CRate = 1,000,000,000 (10^9).

**Reason**: Different denominations:
- **X/P-Chain**: 1 AVAX = 10^9 nAVAX (9 decimals)
- **C-Chain**: 1 AVAX = 10^18 Wei (18 decimals, like Ethereum)

**Conversion**: `C_wei = X_nanoavax × 10^9`

This allows atomic transactions to convert between the two systems.

### Q4: Where are C-Chain atomic RPCs documented?

**A**:
- **RPC Implementation**: `graft/coreth/plugin/evm/atomic/vm/api.go`
- **Endpoint**: `/ext/bc/C/avax`
- **Namespace**: `avax.*` (e.g., `avax.getUTXOs`, `avax.issueTx`)
- **Separate from**: `eth_*` namespace (standard EVM RPCs)

### Q5: How to get block height from atomic tx on C-Chain?

**A**: Use standard EVM methods:
- `eth_getTransactionByHash` - Returns block number
- `avax.getAtomicTxStatus` - Returns status (but not block height directly)

For X/P-Chain: No direct RPC. Must index blocks or use Glacier API.

### Q6: Can I transfer X-Chain → C-Chain directly?

**A**: **YES**, on Primary Network. All three chains (X, P, C) are in the same subnet, so atomic transactions work between any pair:
- X ↔ P
- P ↔ C
- X ↔ C

### Q7: How do I calculate total P-Chain balance?

**A**: Sum all UTXO categories:
- Unlocked (spendable now)
- Locked stakeable (can stake, not spend)
- Locked not stakeable (time-locked)
- Currently staked
- Pending staked (validator not started)
- Atomic memory (exported, not imported)

See `platform.getBalance` (deprecated) or sum manually via `platform.getUTXOs`.

### Q8: Why is `getBalance` deprecated?

**A**: Performance. It requires iterating ALL UTXOs, which is expensive. Modern approach:
- Use `getUTXOs` with pagination
- Calculate balance client-side
- Cache results

### Q9: Do atomic transactions require fees on both chains?

**A**: **NO.** Only one transaction per chain:
1. **Export** on source chain (pays fee on source)
2. **Import** on destination chain (pays fee on destination)

Two separate transactions, two separate fees.

### Q10: Can atomic transactions fail after Export is accepted?

**A**: The **Export** is final (UTXOs moved to shared memory). But:
- **Import** can fail if malformed
- UTXOs will remain in shared memory until valid Import
- No funds are lost, just stuck until proper Import

**Best Practice**: Construct Import carefully, test on testnet first.

### Q11: How to handle multi-asset atomic transactions?

**A**: All chains support multiple assets:
- **X-Chain**: Native multi-asset (ANTs)
- **P-Chain**: AVAX only
- **C-Chain**: AVAX + ANTs (using MultiCoin extension)

When exporting/importing non-AVAX:
- Use same structures (`TransferableInput/Output`)
- Specify correct `AssetID`
- No X2CRate for non-AVAX assets (already same denomination)

### Q12: What's the maximum UTXO set size?

**A**: No hard limit, but practical limits:
- **RPC Limit**: 1024 UTXOs per `getUTXOs` call (paginate for more)
- **Transaction Size**: ~64KB limit (practical ~200 inputs)
- **State Size**: Depends on node hardware

**Best Practice**: Consolidate UTXOs periodically (sweep small UTXOs into one).

---

## Conclusion

This guide consolidates knowledge from:
- ✅ 4 AI-generated analyses
- ✅ Direct codebase exploration
- ✅ Official README and documentation
- ✅ Test files and examples

**Key Takeaways**:

1. **Shared Memory** is the foundation - a database primitive, not a 4th chain
2. **Common UTXO components** in `vms/components/avax/` are shared by X and P
3. **Each chain** (X, P, C) has unique atomic transaction handling
4. **C-Chain** bridges UTXO ↔ EVM via X2CRate conversion
5. **RPC APIs** follow similar patterns but with chain-specific quirks
6. **Balance calculation** requires iterating UTXOs (deprecated RPCs exist)
7. **Atomic transactions** are 2-phase: Export then Import

**Next Steps for Building an Expert System**:

Feed these files/folders:
1. ✅ `chains/atomic/` - Shared memory (start here!)
2. ✅ `vms/components/avax/` - Core UTXO types
3. ✅ `vms/avm/` - X-Chain (focus: service.go, txs/)
4. ✅ `vms/platformvm/` - P-Chain (focus: service.go, txs/, stakeable/)
5. ✅ `graft/coreth/plugin/evm/atomic/` - C-Chain atomic (all files)
6. ✅ `vms/secp256k1fx/` - UTXO semantics
7. ✅ `wallet/chain/c/` - Reference implementation

**Additional Context**:
- Service docs: `vms/avm/service.md`, `vms/platformvm/service.md`
- Test files: `*_test.go` (great for understanding behavior)
- Examples: `wallet/subnet/primary/examples/`

---

**Document Maintenance**: This guide reflects AvalancheGo codebase as of December 2025. For latest changes, refer to:
- Official docs: https://docs.avax.network
- GitHub: https://github.com/ava-labs/avalanchego

**Contributors**: Cross-referenced from analyses by Claude Opus, Claude Sonnet, Google Gemini, and codebase verification.

