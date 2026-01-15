# AvalancheGo UTXO & Atomic Transactions Complete Reference

## Executive Summary

This document is a cross-referenced, verified guide to UTXO and atomic transaction handling in the AvalancheGo codebase. It consolidates findings from multiple AI analyses and validates them against the actual source code.

**Key Chains:**
- **P-Chain** (Platform): UTXO-based, handles staking/validation/subnets. AVAX only.
- **X-Chain** (Exchange/AVM): UTXO-based, multi-asset (AVAX + custom ANTs).
- **C-Chain** (Contract/EVM): Account-based EVM, but has atomic transaction layer for cross-chain UTXO operations.

**Critical Insight:** Shared Memory is **NOT a 4th chain**. It's a per-chain-pair database partition enabling bidirectional cross-chain communication.

---

## 1. SHARED MEMORY - The Foundation

### Location
📂 **`/avalanchego/chains/atomic/`**

### Key Files
| File | Purpose |
|------|---------|
| `README.md` | **START HERE** - Excellent design documentation |
| `shared_memory.go` | `SharedMemory` interface + `Requests`/`Element` structs |
| `memory.go` | `Memory` struct - creates unique `sharedID` per chain pair |
| `state.go` | State management (valueDB + indexDB for inbound/outbound) |
| `prefixes.go` | Database prefix constants |
| `codec.go` | Serialization codec |
| `gsharedmemory/` | gRPC implementation for remote VMs |

### Architecture

**Shared Memory is NOT a separate chain.** It's a database primitive using `prefixdb` on the same LevelDB that all chains use.

```
Base Database (LevelDB)
├── ChainA prefixdb (ChainA's own state)
├── ChainB prefixdb (ChainB's own state)
└── Shared Memory prefixdb
    └── sharedID(ChainA, ChainB) prefixdb
        ├── inbound state  (messages TO ChainA FROM ChainB)
        └── outbound state (messages TO ChainB FROM ChainA)
```

**Each chain pair has its own isolated shared database:**
- P-Chain ↔ X-Chain: unique `sharedID`
- P-Chain ↔ C-Chain: unique `sharedID`
- X-Chain ↔ C-Chain: unique `sharedID`

### How sharedID is Computed

```go
// memory.go:104-115
func sharedID(id1, id2 ids.ID) ids.ID {
    // Swap IDs locally to ensure id1 <= id2 (ordering for consistency)
    if bytes.Compare(id1[:], id2[:]) == 1 {
        id1, id2 = id2, id1
    }
    combinedBytes, _ := Codec.Marshal(CodecVersion, [2]ids.ID{id1, id2})
    return hashing.ComputeHash256Array(combinedBytes)
}
```

### Interface (Verified from source)

```go
// shared_memory.go:28-56
type SharedMemory interface {
    // Get fetches values from peer chain by exact keys
    Get(peerChainID ids.ID, keys [][]byte) (values [][]byte, err error)
    
    // Indexed returns paginated values by traits (addresses)
    Indexed(peerChainID ids.ID, traits [][]byte, startTrait, startKey []byte, limit int) (
        values [][]byte, lastTrait, lastKey []byte, err error)
    
    // Apply atomically applies operations to shared memory + chain's own DB
    Apply(requests map[ids.ID]*Requests, batches ...database.Batch) error
}

// shared_memory.go:15-26
type Requests struct {
    RemoveRequests [][]byte   `serialize:"true"` // Keys to remove
    PutRequests    []*Element `serialize:"true"` // Elements to add
    peerChainID    ids.ID
}

type Element struct {
    Key    []byte   `serialize:"true"` // UTXO ID
    Value  []byte   `serialize:"true"` // Serialized UTXO
    Traits [][]byte `serialize:"true"` // Addresses (for indexing)
}
```

### Concurrency Control
- Each `sharedID` has its own lock (`memory.go:71-101`)
- `GetSharedDatabase()` acquires lock, `ReleaseSharedDatabase()` releases it
- Reference counting ensures proper cleanup

---

## 2. UTXO CORE STRUCTURES

### Location
📂 **`/avalanchego/vms/components/avax/`**

### Key Files
| File | Purpose |
|------|---------|
| `utxo.go` | `UTXO` struct definition |
| `utxo_id.go` | `UTXOID` (TxID + OutputIndex) |
| `utxo_state.go` | `UTXOState` interface (Get/Put/Delete with caching) |
| `utxo_fetching.go` | `GetPaginatedUTXOs()`, `GetAllUTXOs()`, `GetBalance()` |
| `atomic_utxos.go` | `GetAtomicUTXOs()` - fetches from shared memory |
| `transferables.go` | `TransferableInput`/`TransferableOutput` |
| `base_tx.go` | `BaseTx` - common transaction structure |
| `flow_checker.go` | Verifies inputs >= outputs (no minting) |

### UTXO Structure (Verified)

```go
// utxo.go:19-24
type UTXO struct {
    UTXOID `serialize:"true"`        // TxID + OutputIndex → unique ID
    Asset  `serialize:"true"`        // AssetID
    Out    verify.State `serialize:"true" json:"output"` // The output (TransferOutput, etc.)
}

// utxo_id.go:29-38
type UTXOID struct {
    TxID        ids.ID `serialize:"true" json:"txID"`
    OutputIndex uint32 `serialize:"true" json:"outputIndex"`
    Symbol      bool   `json:"-"`       // False if UTXO should be in DB
    id          ids.ID                  // Cached unique ID
}

// InputID() computes: TxID.Prefix(OutputIndex)
```

### Transferable Types (Verified)

```go
// transferables.go:64-69
type TransferableOutput struct {
    Asset `serialize:"true"`
    FxID  ids.ID          `serialize:"false" json:"fxID"`
    Out   TransferableOut `serialize:"true"  json:"output"`
}

// transferables.go:140-146
type TransferableInput struct {
    UTXOID `serialize:"true"`
    Asset  `serialize:"true"`
    FxID   ids.ID         `serialize:"false" json:"fxID"`
    In     TransferableIn `serialize:"true"  json:"input"`
}
```

### UTXO State Management (Verified)

```go
// utxo_state.go:31-37
type UTXOState interface {
    UTXOReader
    UTXOWriter
    Checksum() ids.ID  // For state verification
}

type UTXOReader interface {
    UTXOGetter
    UTXOIDs(addr []byte, previous ids.ID, limit int) ([]ids.ID, error)
}

type UTXOGetter interface {
    GetUTXO(utxoID ids.ID) (*UTXO, error)
}
```

**Database Layout:**
- `utxo/` prefix: `UTXO_ID → serialized UTXO`
- `index/` prefix: `Address → linked list of UTXO_IDs`

### Balance Calculation (Verified)

```go
// utxo_fetching.go:18-34
func GetBalance(db UTXOReader, addrs set.Set[ids.ShortID]) (uint64, error) {
    utxos, err := GetAllUTXOs(db, addrs)
    if err != nil {
        return 0, fmt.Errorf("couldn't get UTXOs: %w", err)
    }
    balance := uint64(0)
    for _, utxo := range utxos {
        if out, ok := utxo.Out.(Amounter); ok {
            balance, err = safemath.Add(out.Amount(), balance)
            if err != nil {
                return 0, err
            }
        }
    }
    return balance, nil
}
```

### Atomic UTXO Fetching (Verified)

```go
// atomic_utxos.go:25-71
func GetAtomicUTXOs(
    sharedMemory atomic.SharedMemory,
    codec codec.Manager,
    chainID ids.ID,           // Source chain
    addrs set.Set[ids.ShortID],
    startAddr ids.ShortID,
    startUTXOID ids.ID,
    limit int,
) ([]*UTXO, ids.ShortID, ids.ID, error) {
    // Converts addresses to trait bytes
    // Calls sharedMemory.Indexed() with traits
    // Unmarshals returned bytes to UTXO structs
}
```

---

## 3. SECP256K1FX - Feature Extension

### Location
📂 **`/avalanchego/vms/secp256k1fx/`**

This provides the Bitcoin-like UTXO semantics.

### Key Files
| File | Purpose |
|------|---------|
| `transfer_output.go` | `TransferOutput` - amount + owners |
| `transfer_input.go` | `TransferInput` - amount + sig indices |
| `output_owners.go` | `OutputOwners` - locktime + threshold + addresses |
| `credential.go` | `Credential` - signatures for spending |
| `keychain.go` | Key management and signing |

### Output Structure (Verified)

```go
// transfer_output.go:19-25
type TransferOutput struct {
    verify.IsState `json:"-"`
    Amt uint64 `serialize:"true" json:"amount"`
    OutputOwners `serialize:"true"`
}

// output_owners.go:26-37
type OutputOwners struct {
    verify.IsNotState `json:"-"`
    Locktime  uint64        `serialize:"true" json:"locktime"`  // Unix timestamp
    Threshold uint32        `serialize:"true" json:"threshold"` // M-of-N
    Addrs     []ids.ShortID `serialize:"true" json:"addresses"` // N addresses
}
```

### Input Structure (Verified)

```go
// transfer_input.go:14-17
type TransferInput struct {
    Amt   uint64 `serialize:"true" json:"amount"`
    Input `serialize:"true"`  // Contains SigIndices
}

// input.go (SigIndices indicates which addresses are signing)
type Input struct {
    SigIndices []uint32 `serialize:"true" json:"signatureIndices"`
}
```

**Key Features:**
- **M-of-N Multisig**: `Threshold` signatures required from `len(Addrs)` addresses
- **Timelocks**: UTXOs locked until `Locktime` (Unix timestamp)

---

## 4. X-CHAIN (AVM)

### Location
📂 **`/avalanchego/vms/avm/`**

### Key Files
| File | Purpose |
|------|---------|
| `vm.go` | VM implementation |
| `service.go` | RPC API handlers |
| `service.md` | API documentation |
| `client.go` | Go client for X-chain API |
| `txs/import_tx.go` | ImportTx structure |
| `txs/export_tx.go` | ExportTx structure |
| `txs/base_tx.go` | BaseTx with Ins/Outs |
| `txs/executor/executor.go` | Transaction execution |
| `state/state.go` | X-Chain state |
| `utxo/spender.go` | UTXO selection logic |

### Transaction Types (Verified)

```go
// txs/import_tx.go:19-27
type ImportTx struct {
    BaseTx      `serialize:"true"`
    SourceChain ids.ID                    `serialize:"true" json:"sourceChain"`
    ImportedIns []*avax.TransferableInput `serialize:"true" json:"importedInputs"`
}

// txs/export_tx.go:19-27
type ExportTx struct {
    BaseTx           `serialize:"true"`
    DestinationChain ids.ID                     `serialize:"true" json:"destinationChain"`
    ExportedOuts     []*avax.TransferableOutput `serialize:"true" json:"exportedOutputs"`
}
```

### Key RPCs (service.go)

| RPC | Line | Description |
|-----|------|-------------|
| `avm.getUTXOs` | 285-387 | Get UTXOs; uses `sourceChain` to fetch local or atomic |
| `avm.getBalance` | 453+ | **DEPRECATED** - Returns balance for address+asset |
| `avm.getAllBalances` | 530+ | **DEPRECATED** - All balances per address |
| `avm.getTx` | 244+ | Get transaction by ID |
| `avm.issueTx` | - | Submit signed transaction |
| `avm.getAssetDescription` | 403+ | Get asset metadata |

**GetUTXOs Logic (Verified):**
```go
// service.go:341-358
if sourceChain == s.vm.ctx.ChainID {
    utxos, endAddr, endUTXOID, err = avax.GetPaginatedUTXOs(...)  // Local
} else {
    utxos, endAddr, endUTXOID, err = avax.GetAtomicUTXOs(...)     // From shared memory
}
```

---

## 5. P-CHAIN (PlatformVM)

### Location
📂 **`/avalanchego/vms/platformvm/`**

### Key Files
| File | Purpose |
|------|---------|
| `vm.go` | VM implementation |
| `service.go` | RPC API (2152 lines!) |
| `service.md` | API documentation (2198 lines!) |
| `client.go` | Go client |
| `txs/import_tx.go` | ImportTx |
| `txs/export_tx.go` | ExportTx |
| `txs/add_validator_tx.go` | Add validator |
| `txs/executor/standard_tx_executor.go` | Standard tx execution |
| `state/state.go` | P-Chain state (UTXOs + validators + subnets) |
| `utxo/verifier.go` | Spend verification |
| `stakeable/stakeable_lock.go` | `LockOut`/`LockIn` for staking |

### Stakeable Lock Types (Verified)

```go
// stakeable/stakeable_lock.go:17-20
type LockOut struct {
    Locktime             uint64 `serialize:"true" json:"locktime"`
    avax.TransferableOut `serialize:"true" json:"output"`
}

type LockIn struct {
    Locktime            uint64 `serialize:"true" json:"locktime"`
    avax.TransferableIn `serialize:"true" json:"input"`
}
```

### Balance Categories (Verified from service.go:138-248)

| Category | Condition |
|----------|-----------|
| `unlocked` | `TransferOutput` with `locktime <= now` |
| `lockedStakeable` | `LockOut` where inner `locktime <= now` but outer `Locktime > now` |
| `lockedNotStakeable` | `TransferOutput` with `locktime > now` OR `LockOut` with inner `locktime > now` |

```go
// service.go:167-216 (simplified)
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
        lockedNotStakeables[assetID] += amount
    } else if out.Locktime <= currentTime {
        unlockeds[assetID] += amount
    } else {
        lockedStakeables[assetID] += amount
    }
}
```

### Key RPCs (service.go)

| RPC | Line | Description |
|-----|------|-------------|
| `platform.getUTXOs` | 266-366 | Get UTXOs; supports `sourceChain` |
| `platform.getBalance` | 138-248 | **DEPRECATED** - Returns categorized balances |
| `platform.getStake` | 1556+ | Get staking info for addresses |
| `platform.getCurrentValidators` | 708+ | Current validator set |
| `platform.getTx` | 1432+ | Get transaction by ID |
| `platform.issueTx` | - | Submit signed transaction |

---

## 6. C-CHAIN ATOMIC TRANSACTIONS

### Location
📂 **`/avalanchego/graft/coreth/plugin/evm/atomic/`**

### Key Files
| File | Purpose |
|------|---------|
| `tx.go` | `UnsignedAtomicTx` interface, `EVMInput`/`EVMOutput`, X2CRate |
| `import_tx.go` | `UnsignedImportTx` - UTXO → EVM balance |
| `export_tx.go` | `UnsignedExportTx` - EVM balance → UTXO |
| `codec.go` | Serialization for atomic txs |
| `params.go` | Constants |
| `vm/api.go` | `avax.*` RPC namespace |
| `vm/vm.go` | VM integration |
| `vm/tx_semantic_verifier.go` | Atomic tx verification |
| `state/atomic_trie.go` | Atomic state tracking |

### X2CRate - Critical Conversion (Verified)

```go
// tx.go:33-61
const X2CRateUint64 uint64 = 1_000_000_000  // 10^9

// X2CRate is the conversion rate between:
// - X/P-Chain: nanoAVAX (10^-9 AVAX)
// - C-Chain: Wei (10^-18 AVAX)
// 1 nAVAX = 1 gWei = 10^9 Wei

var X2CRate = uint256.NewInt(X2CRateUint64)
```

### C-Chain Specific Types (Verified)

```go
// tx.go:64-84
type EVMOutput struct {
    Address common.Address `serialize:"true" json:"address"` // Ethereum address!
    Amount  uint64         `serialize:"true" json:"amount"`  // In nAVAX
    AssetID ids.ID         `serialize:"true" json:"assetID"`
}

type EVMInput struct {
    Address common.Address `serialize:"true" json:"address"`
    Amount  uint64         `serialize:"true" json:"amount"`
    AssetID ids.ID         `serialize:"true" json:"assetID"`
    Nonce   uint64         `serialize:"true" json:"nonce"`  // EVM nonce required!
}
```

### Import TX (UTXO → EVM Balance) (Verified)

```go
// import_tx.go:48-60
type UnsignedImportTx struct {
    Metadata
    NetworkID      uint32                    `serialize:"true" json:"networkID"`
    BlockchainID   ids.ID                    `serialize:"true" json:"blockchainID"`
    SourceChain    ids.ID                    `serialize:"true" json:"sourceChain"`
    ImportedInputs []*avax.TransferableInput `serialize:"true" json:"importedInputs"`
    Outs           []EVMOutput               `serialize:"true" json:"outputs"`
}

// EVMStateTransfer (import_tx.go:335-350)
func (utx *UnsignedImportTx) EVMStateTransfer(ctx *snow.Context, state StateDB) error {
    for _, to := range utx.Outs {
        if to.AssetID == ctx.AVAXAssetID {
            // Convert nAVAX to Wei by multiplying by X2CRate
            amount := new(uint256.Int).Mul(uint256.NewInt(to.Amount), X2CRate)
            state.AddBalance(to.Address, amount)
        } else {
            // Multi-coin (non-AVAX assets)
            amount := new(big.Int).SetUint64(to.Amount)
            state.AddBalanceMultiCoin(to.Address, common.Hash(to.AssetID), amount)
        }
    }
    return nil
}
```

### Export TX (EVM Balance → UTXO) (Verified)

```go
// export_tx.go:46-58
type UnsignedExportTx struct {
    Metadata
    NetworkID        uint32                     `serialize:"true" json:"networkID"`
    BlockchainID     ids.ID                     `serialize:"true" json:"blockchainID"`
    DestinationChain ids.ID                     `serialize:"true" json:"destinationChain"`
    Ins              []EVMInput                 `serialize:"true" json:"inputs"`
    ExportedOutputs  []*avax.TransferableOutput `serialize:"true" json:"exportedOutputs"`
}

// EVMStateTransfer (export_tx.go:314-345)
func (utx *UnsignedExportTx) EVMStateTransfer(ctx *snow.Context, state StateDB) error {
    for _, from := range utx.Ins {
        if from.AssetID == ctx.AVAXAssetID {
            // Convert nAVAX amount to Wei for subtraction
            amount := new(uint256.Int).Mul(uint256.NewInt(from.Amount), X2CRate)
            state.SubBalance(from.Address, amount)
        }
        // Verify and increment nonce
        if state.GetNonce(from.Address) != from.Nonce {
            return ErrInvalidNonce
        }
    }
    // Update nonces
    for addr, nonce := range addrs {
        state.SetNonce(addr, nonce+1)
    }
    return nil
}
```

### AtomicOps (How UTXOs move to shared memory) (Verified)

```go
// export_tx.go:186-217
func (utx *UnsignedExportTx) AtomicOps() (ids.ID, *atomic.Requests, error) {
    txID := utx.ID()
    elems := make([]*atomic.Element, len(utx.ExportedOutputs))
    for i, out := range utx.ExportedOutputs {
        utxo := &avax.UTXO{
            UTXOID: avax.UTXOID{TxID: txID, OutputIndex: uint32(i)},
            Asset:  avax.Asset{ID: out.AssetID()},
            Out:    out.Out,
        }
        utxoBytes, _ := Codec.Marshal(CodecVersion, utxo)
        utxoID := utxo.InputID()
        elem := &atomic.Element{
            Key:   utxoID[:],
            Value: utxoBytes,
        }
        if out, ok := utxo.Out.(avax.Addressable); ok {
            elem.Traits = out.Addresses()  // For indexing by address
        }
        elems[i] = elem
    }
    return utx.DestinationChain, &atomic.Requests{PutRequests: elems}, nil
}
```

### C-Chain Atomic RPCs (vm/api.go)

| RPC | Line | Description |
|-----|------|-------------|
| `avax.getUTXOs` | 40-126 | **REQUIRES** `sourceChain` - only atomic UTXOs |
| `avax.issueTx` | 128-160 | Submit atomic transaction |
| `avax.getAtomicTxStatus` | 162-190 | Status: Unknown/Processing/Accepted |
| `avax.getAtomicTx` | 197-236 | Get atomic tx by ID with block height |

**Critical:** C-Chain's `avax.getUTXOs` REQUIRES `sourceChain` parameter. It cannot fetch local UTXOs (there are none - C-Chain is account-based).

---

## 7. WALLET SDK - Reference Implementation

### Location
📂 **`/avalanchego/wallet/`**

### Structure
```
wallet/
├── chain/
│   ├── c/
│   │   ├── backend.go    # UTXO tracking + EVM balance conversion
│   │   ├── builder.go    # NewImportTx, NewExportTx
│   │   ├── signer.go     # Sign atomic txs
│   │   └── context.go    # C-chain context
│   ├── p/
│   │   └── builder/
│   │       └── builder.go # P-chain tx building
│   └── x/
│       └── builder/
│           └── builder.go # X-chain tx building
└── subnet/primary/
    ├── api.go            # Combined API for all chains
    ├── common/
    │   ├── utxos.go      # UTXO tracking across chains
    │   └── spend.go      # UTXO selection logic
    └── examples/         # Working code examples
```

### C-Chain Backend (Verified)

```go
// wallet/chain/c/backend.go:59-128
func (b *backend) AcceptAtomicTx(ctx context.Context, tx *atomic.Tx) error {
    switch tx := tx.UnsignedAtomicTx.(type) {
    case *atomic.UnsignedImportTx:
        // Remove UTXOs from tracking
        for _, input := range tx.ImportedInputs {
            b.RemoveUTXO(ctx, tx.SourceChain, input.InputID())
        }
        // Add balance to EVM accounts
        for _, output := range tx.Outs {
            balance := new(big.Int).SetUint64(output.Amount)
            balance.Mul(balance, avaxConversionRate)  // × 10^9
            account.Balance.Add(account.Balance, balance)
        }
    case *atomic.UnsignedExportTx:
        // Add UTXOs to tracking
        for i, out := range tx.ExportedOutputs {
            b.AddUTXO(ctx, tx.DestinationChain, utxo)
        }
        // Subtract balance from EVM accounts + update nonce
        for _, input := range tx.Ins {
            balance.Mul(balance, avaxConversionRate)
            account.Balance.Sub(account.Balance, balance)
            account.Nonce = input.Nonce + 1
        }
    }
}
```

---

## 8. ATOMIC TRANSACTION FLOW

### Export Flow (e.g., P-Chain → C-Chain)

```
1. BUILD ExportTx on P-Chain
   ├─ Consume P-Chain UTXOs (Ins)
   ├─ Create ExportedOutputs destined for C-Chain
   └─ Specify DestinationChain = C-Chain ID

2. VERIFY ExportTx
   ├─ Check UTXO ownership (signatures match OutputOwners)
   ├─ Check destination chain is in same subnet
   └─ Check flow (inputs >= outputs + fee)

3. ACCEPT ExportTx → executor runs
   ├─ Delete consumed UTXOs from P-Chain state
   ├─ Create atomic.Element for each ExportedOutput
   │   └─ Element{Key: UTXO_ID, Value: serialized_UTXO, Traits: addresses}
   └─ Call SharedMemory.Apply() with PutRequests
       └─ UTXOs added to C-Chain's inbound state

4. QUERY atomic UTXOs from C-Chain
   └─ avax.getUTXOs(sourceChain="P", addresses=[...])
       └─ SharedMemory.Indexed() returns UTXOs

5. BUILD ImportTx on C-Chain
   ├─ ImportedInputs = atomic UTXOs to consume
   ├─ Outs = EVMOutputs (address + amount)
   └─ SourceChain = P-Chain ID

6. VERIFY ImportTx
   ├─ Check UTXOs exist in shared memory (SharedMemory.Get())
   ├─ Check no conflicting blocks in processing
   └─ Check signatures

7. ACCEPT ImportTx
   ├─ Call EVMStateTransfer() → AddBalance(wei = nAVAX × 10^9)
   └─ Call SharedMemory.Apply() with RemoveRequests
       └─ UTXOs removed from C-Chain's inbound state
```

### Import Flow (e.g., C-Chain → P-Chain)

Reverse of above:
1. ExportTx on C-Chain: `SubBalance()` → create UTXOs
2. ImportTx on P-Chain: consume UTXOs → create P-Chain UTXOs

---

## 9. CHAIN IDs & CONSTANTS

### P-Chain ID (Verified)

```go
// utils/constants/network_ids.go:50
PlatformChainID = ids.Empty  // [32]byte{0,0...0} - same on ALL networks
```

### X-Chain & C-Chain IDs
**Network-specific** - derived from genesis. Fetch via:
- `info.getBlockchainID(alias="X")` 
- `info.getBlockchainID(alias="C")`

### Mainnet IDs
```
P-Chain: 11111111111111111111111111111111LpoYY (always ids.Empty)
X-Chain: 2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM
C-Chain: 2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5
```

### Fuji Testnet IDs
```
P-Chain: 11111111111111111111111111111111LpoYY (same)
X-Chain: 2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm
C-Chain: yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp
```

---

## 10. ADDRESS FORMATS

- **P/X-Chain**: Bech32 format `P-avax1...` / `X-avax1...`
- **C-Chain**: 
  - EVM: Hex `0x...`
  - Atomic operations: Bech32 `C-avax1...` (for UTXO addresses)
- **Same private key** derives both formats

---

## 11. GOTCHAS & EDGE CASES

### C-Chain Balance is NOT UTXO Sum
C-Chain uses EVM account balances. UTXOs only exist temporarily during atomic operations.

### Shared Memory is Chain-Pair Specific
- P↔X, P↔C, X↔C each have **separate** shared memory databases
- Query from the **destination chain's perspective**
- Example: To see P-Chain UTXOs exported to C-Chain, query C-Chain with `sourceChain="P"`

### Nonces Required for C-Chain Exports
- `ExportTx.Ins[].Nonce` must match current EVM nonce
- Nonce incremented on acceptance
- ImportTx does NOT require nonce (UTXOs have no nonce concept)

### Atomic UTXO Removal Timing
- UTXOs removed from shared memory when ImportTx is **accepted** (not verified)
- Must check no conflicting block in processing chain before verification

### Banff Upgrade Restrictions
After Banff upgrade:
- Import/Export can only use AVAX (no ANTs in atomic txs)
- See `ErrImportNonAVAXInputBanff`, `ErrExportNonAVAXOutputBanff`

---

## 12. FILE REFERENCE SUMMARY

### Must-Read Core Files
| File | Priority | Purpose |
|------|----------|---------|
| `chains/atomic/README.md` | **1** | Shared memory design doc |
| `chains/atomic/shared_memory.go` | **1** | Interface definition |
| `vms/components/avax/utxo.go` | **1** | UTXO structure |
| `vms/components/avax/atomic_utxos.go` | **1** | GetAtomicUTXOs |
| `graft/coreth/plugin/evm/atomic/tx.go` | **2** | X2CRate, EVMInput/Output |
| `graft/coreth/plugin/evm/atomic/import_tx.go` | **2** | UTXO → EVM |
| `graft/coreth/plugin/evm/atomic/export_tx.go` | **2** | EVM → UTXO |

### RPC Implementations
| Chain | Service File | API Docs |
|-------|--------------|----------|
| X-Chain | `vms/avm/service.go` | `vms/avm/service.md` |
| P-Chain | `vms/platformvm/service.go` | `vms/platformvm/service.md` |
| C-Chain (atomic) | `graft/coreth/plugin/evm/atomic/vm/api.go` | `graft/coreth/plugin/evm/api.md` |

### Transaction Structures
| Chain | Import | Export |
|-------|--------|--------|
| X-Chain | `vms/avm/txs/import_tx.go` | `vms/avm/txs/export_tx.go` |
| P-Chain | `vms/platformvm/txs/import_tx.go` | `vms/platformvm/txs/export_tx.go` |
| C-Chain | `graft/coreth/plugin/evm/atomic/import_tx.go` | `graft/coreth/plugin/evm/atomic/export_tx.go` |

### Execution Logic
| Chain | Executor |
|-------|----------|
| X-Chain | `vms/avm/txs/executor/executor.go` |
| P-Chain | `vms/platformvm/txs/executor/standard_tx_executor.go` |
| C-Chain | `graft/coreth/plugin/evm/atomic/vm/tx_semantic_verifier.go` |

---

## 13. FOLDERS TO FEED TO EXPERT SYSTEM

### Essential (Include All Files)
1. `chains/atomic/` - Shared memory
2. `vms/components/avax/` - UTXO primitives
3. `vms/secp256k1fx/` - Feature extension
4. `graft/coreth/plugin/evm/atomic/` - C-Chain atomic layer

### Recommended
5. `vms/avm/txs/` - X-Chain transactions
6. `vms/platformvm/txs/` - P-Chain transactions
7. `wallet/chain/c/` - C-Chain wallet (shows full flow)

### Optional but Useful
8. `vms/avm/service.go` - X-Chain RPC
9. `vms/platformvm/service.go` - P-Chain RPC (large file!)
10. `wallet/subnet/primary/examples/` - Working code examples

---

## 14. QUICK REFERENCE DIAGRAM

```
┌─────────────────────────────────────────────────────────────────┐
│                          NODE DATABASE                          │
├─────────────────┬─────────────────┬─────────────────────────────┤
│   P-Chain DB    │   X-Chain DB    │     Shared Memory DB        │
│   (prefixdb)    │   (prefixdb)    │       (prefixdb)            │
│                 │                 │                              │
│  ┌───────────┐  │  ┌───────────┐  │  ┌────────────────────────┐ │
│  │ UTXOState │  │  │ UTXOState │  │  │ sharedID(P,X)          │ │
│  │ Validators│  │  │ Assets    │  │  │  ├─ P inbound/outbound │ │
│  │ Subnets   │  │  │ Blocks    │  │  │  └─ X inbound/outbound │ │
│  └───────────┘  │  └───────────┘  │  ├────────────────────────┤ │
│                 │                 │  │ sharedID(P,C)          │ │
│                 │                 │  │  ├─ P inbound/outbound │ │
│                 │                 │  │  └─ C inbound/outbound │ │
│                 │                 │  ├────────────────────────┤ │
│                 │                 │  │ sharedID(X,C)          │ │
│                 │                 │  │  ├─ X inbound/outbound │ │
│                 │                 │  │  └─ C inbound/outbound │ │
│                 │                 │  └────────────────────────┘ │
└─────────────────┴─────────────────┴─────────────────────────────┘
         │                 │                     │
         ▼                 ▼                     ▼
┌─────────────────────────────────────────────────────────────────┐
│                        C-CHAIN (EVM)                            │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │                 Atomic Transaction Layer                 │  │
│   │  Import: UTXO → AddBalance(address, nAVAX × 10^9)       │  │
│   │  Export: SubBalance(address, nAVAX × 10^9) → UTXO       │  │
│   └─────────────────────────────────────────────────────────┘  │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │                      EVM State                           │  │
│   │            (Account-based balance: Wei)                  │  │
│   └─────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

---

## 15. VERSION INFO

- **Based on**: AvalancheGo codebase as of 2025
- **Verified against**: Source files at paths listed above
- **Cross-referenced**: 4 AI-generated analyses + manual source verification

---

*This document consolidates information from multiple AI analyses (Opus, Composer, Sonnet, Gemini) and validates all claims against the actual AvalancheGo source code.*

