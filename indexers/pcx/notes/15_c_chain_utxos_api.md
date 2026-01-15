# C-Chain UTXOs Implementation Plan

## SIMPLIFIED APPROACH

**Realization**: We're overcomplicating this. The current `StoredUTXO` already works for P-Chain.
For C-Chain, we just need:
1. Store C-Chain UTXOs separately (different prefix)
2. Different API serialization (different field names)
3. Same indexing logic, different output format

**NOT doing**: Separate struct types, double writes. That's premature optimization.

---

## What's Actually Broken

1. **API always returns P-Chain format** - `chainInfo: p-chain`, P-Chain field names
2. **API queries P-Chain storage** - returns P-Chain UTXOs even for `/c-chain/utxos`
3. **C-Chain ExportTx writes to P-Chain storage** - wrong bucket

---

## Simple Fix (3 Steps)

### Step 1: Separate Storage Prefixes
```go
const (
    prefixPChainUTXO = "p-utxo:"  // P-Chain UTXOs
    prefixCChainUTXO = "c-utxo:"  // C-Chain UTXOs (atomic memory)
    prefixPChainAddr = "p-addr:"  // P-Chain address index
    prefixCChainAddr = "c-addr:"  // C-Chain address index
)
```

### Step 2: Route API by Chain
```go
func (u *UTXOs) handleUTXOs(w http.ResponseWriter, r *http.Request) {
    chain := r.PathValue("blockchainId")  // "p-chain" or "c-chain"
    
    var utxos []*StoredUTXO
    var chainName string
    
    switch chain {
    case "p-chain", pChainID:
        utxos = u.getUTXOsForAddresses(addrs, "p-addr:", "p-utxo:", ...)
        chainName = "p-chain"
    case "c-chain", cChainIDFuji, cChainIDMainnet:
        utxos = u.getUTXOsForAddresses(addrs, "c-addr:", "c-utxo:", ...)
        chainName = "c-chain"
    }
    
    // Serialize based on chain
    var apiUTXOs []map[string]any
    for _, utxo := range utxos {
        if chainName == "c-chain" {
            apiUTXOs = append(apiUTXOs, u.toCChainResponse(utxo))
        } else {
            apiUTXOs = append(apiUTXOs, u.toPChainResponse(utxo))
        }
    }
}
```

### Step 3: C-Chain Serialization
```go
func (u *UTXOs) toCChainResponse(stored *StoredUTXO) map[string]any {
    return map[string]any{
        "utxoId":           stored.UTXOId,
        "creationTxHash":   stored.TxHash,           // Different name!
        "outputIndex":      fmt.Sprintf("%d", stored.OutputIndex),  // STRING!
        "timestamp":        stored.BlockTimestamp,   // Different name!
        "locktime":         getLocktime(stored),     // Not platformLocktime
        "utxoType":         strings.ToLower(stored.UTXOType),  // lowercase
        "addresses":        stored.Addresses,
        "threshold":        stored.Threshold,
        "createdOnChainId": stored.CreatedOnChainID,
        "consumedOnChainId": stored.ConsumedOnChainID,
        // ... consumption fields with C-Chain names
    }
}
```

---

## Credentials Implementation

Glacier returns `credentials` (publicKey, signature) for C-Chain UTXOs. This proves who signed the transaction.

### How it works:

1. **Signature** is stored in `atomicTx.Creds` as `secp256k1fx.Credential`
2. **Public key** is NOT stored - must be recovered via ECDSA recovery:
   ```go
   unsignedBytes, _ := atomicCodec.Marshal(atomicCodecVersion, tx.UnsignedAtomicTx)
   pubKey, _ := secp256k1.RecoverPublicKey(unsignedBytes, sig[:])
   ```
3. Both are base64-encoded in the response

### Implementation:

- `extractCredentials(tx *atomicTx) []Credential` in `atomic.go`
- `Credential{PublicKey, Signature}` struct in `store.go`
- Added to C-Chain storage in ExportTx processing
- Included in `toCChainResponse()` output

---

## Implementation Checklist

- [x] Rename `prefixUTXO` → `prefixPChainUTXO`, `prefixAddr` → `prefixPChainAddr`
- [x] Add `prefixCChainUTXO`, `prefixCChainAddr` 
- [x] Update P-Chain indexing to use new prefixes
- [x] Update C-Chain ExportTx to write to C-Chain storage
- [x] Add `toCChainResponse()` with correct field names
- [x] Update API handler to route by chain
- [x] Enable C-Chain test
- [x] Fix P→C exports to also write to C-Chain storage
- [x] Implement credentials extraction (publicKey recovery from signature)

**STATUS: ✅ ALL 10 TESTS PASSING (including C-Chain with credentials)**

---

## Old Plan (Reference Only)

## Key Findings from Glacier Source Code

### 1. Single UTXO Table for All Chains

Glacier uses **one** `utxos` table for P, X, and C chains:

```sql
CREATE TABLE utxos (
    utxo_id text NOT NULL,
    transaction_hash text NOT NULL,  -- creation tx
    block_index bigint NOT NULL,
    output_index smallint NOT NULL,
    timestamp timestamp NOT NULL,     -- creation timestamp
    address text NOT NULL,
    consuming_transaction_hash text,
    asset_id text NOT NULL,
    locktime bigint,                  -- UTXO locktime (X/C)
    threshold smallint NOT NULL,
    public_key text,                  -- Credentials (comma-separated)
    signature text,                   -- Credentials (comma-separated)
    amount bigint NOT NULL,
    created_on text NOT NULL,         -- Chain ID where created
    consumed_on text NOT NULL,        -- Chain ID where consumed
    network_id smallint NOT NULL,
    platform_locktime timestamp,      -- P-Chain specific
    staked boolean,                   -- P-Chain specific
    stakeable_locktime timestamp,     -- P-Chain specific
    reward boolean,                   -- P-Chain specific
    ...
    PRIMARY KEY (utxo_id, block_index, address)
)
```

**Key points:**
- One row per **(utxo_id, block_index, address)** - supports multi-sig
- Both `locktime` (X/C) and `platform_locktime` (P) in same table
- Credentials stored as comma-separated strings in `public_key` and `signature`

### 2. Chain Filtering Pattern

```sql
WHERE created_on = :blockchainId! OR consumed_on = :blockchainId!
```

Same pattern for all chains. The API layer decides which response schema to use based on `blockchainId`.

### 3. Response Serialization

From `c-chain.service.ts`:
- **P-Chain**: Uses SQL query `getPChainUtxos` → returns `PChainUtxo` schema
- **C-Chain**: Uses SQL query `getCChainUtxosByAddresses` (same as X-Chain) → returns `Utxo` schema

The service layer distinguishes P-Chain from X/C-Chain, not the storage layer.

---

## Root Cause Analysis

### What C-Chain UTXOs Actually Are

C-Chain UTXOs are **atomic memory UTXOs** - created by C-Chain ExportTx transactions:

1. User initiates ExportTx on C-Chain (debits EVM balance)
2. ExportTx creates UTXOs in shared memory for destination chain (P or X)
3. These UTXOs have `createdOnChainId = C-Chain`
4. Later, P-Chain or X-Chain ImportTx consumes them

**NOT the same as P-Chain UTXOs!** Different:
- Source: C-Chain `blockExtraData` (atomic txs), not P-Chain blocks
- Fields: `creationTxHash` vs `txHash`, `timestamp` vs `blockTimestamp`
- No `staked`, `platformLocktime`, `blockNumber`
- Has `credentials` with signature data
- `outputIndex` is STRING, not number
- `utxoType` is lowercase `transfer`

### Current Implementation Issues

1. **Not indexing C-Chain exports at all** - Current C-Chain indexer only handles:
   - ExportTx TO P-Chain (to fill cross-chain UTXO data when P-Chain imports)
   - ImportTx FROM P-Chain (to mark consumption)
   
   It doesn't create "C-Chain UTXOs" - it creates P-Chain UTXOs with C-Chain metadata.

2. **Wrong data source** - For C-Chain UTXOs endpoint:
   - Should query UTXOs created by C-Chain ExportTx
   - Currently querying P-Chain UTXOs (completely wrong)

3. **Different response format** - C-Chain UTXOs have different schema than P-Chain

---

## What `/c-chain/utxos` Should Return

UTXOs where:
- `createdOnChainId = C-Chain` (exported FROM C-Chain), OR
- `consumedOnChainId = C-Chain` (imported TO C-Chain, i.e., UTXOs that were exports from P/X TO C)

But the **dominant use case** is:
- UTXOs exported FROM C-Chain TO P-Chain or X-Chain
- These are "in atomic memory" until imported on destination chain

---

## Solution Architecture

### ~~Option A: Separate C-Chain UTXO Storage~~ → **CHOSEN APPROACH**

Store each chain's UTXOs in its native format with **double writes for cross-chain UTXOs**:

```
p-utxo:{utxoID}      → P-Chain UTXO (PChainUtxo schema, JSON-ready)
c-utxo:{utxoID}      → C-Chain UTXO (Utxo schema, JSON-ready)
x-utxo:{utxoID}      → X-Chain UTXO (Utxo schema, future)
p-addr:{address}:{utxoID} → P-Chain address index
c-addr:{address}:{utxoID} → C-Chain address index
```

**Key insight**: Cross-chain UTXOs belong to BOTH chains semantically. A C→P export shows up in:
- C-Chain UTXOs (created there)
- P-Chain UTXOs (consumed there)

So we write to **both** tables. This isn't duplication - it's **precomputed API responses**.

### ~~Option B: Unified Storage with Chain-Specific Serialization~~ → Rejected

Single UTXO storage with runtime chain detection and response transformation.

**Why rejected**:
- Adds complexity at query time
- Response transformation logic spreads across codebase
- Field naming conflicts (storing both `txHash` and `creationTxHash`?)

---

## Chosen Architecture: Separate Storage + Double Writes

### Storage Schema

**P-Chain Storage** (`p-utxo:{utxoID}`):
```go
type PChainStoredUTXO struct {
    // Exactly matches PChainUtxo API schema
    Addresses               []string `json:"addresses"`
    Amount                  string   `json:"amount"`
    AssetID                 string   `json:"assetId"`
    BlockNumber             string   `json:"blockNumber"`      // P-Chain specific
    BlockTimestamp          int64    `json:"blockTimestamp"`   // P-Chain specific
    CreatedOnChainID        string   `json:"createdOnChainId"`
    ConsumedOnChainID       string   `json:"consumedOnChainId"`
    OutputIndex             int      `json:"outputIndex"`      // NUMBER for P-Chain
    TxHash                  string   `json:"txHash"`           // P-Chain naming
    UTXOId                  string   `json:"utxoId"`
    UTXOType                string   `json:"utxoType"`         // "TRANSFER" uppercase
    
    // P-Chain optional
    ConsumingTxHash         *string  `json:"consumingTxHash,omitempty"`
    ConsumingBlockNumber    *string  `json:"consumingBlockNumber,omitempty"`
    ConsumingBlockTimestamp *int64   `json:"consumingBlockTimestamp,omitempty"`
    PlatformLocktime        *int64   `json:"platformLocktime,omitempty"`
    Staked                  *bool    `json:"staked,omitempty"`
    UTXOStartTimestamp      *int64   `json:"utxoStartTimestamp,omitempty"`
    UTXOEndTimestamp        *int64   `json:"utxoEndTimestamp,omitempty"`
    UTXOBytes               string   `json:"utxoBytes,omitempty"`
    Threshold               uint32   `json:"threshold,omitempty"`
    
    // Asset metadata (enriched at query time)
    Asset                   *AssetInfo `json:"asset,omitempty"`
}
```

**C-Chain Storage** (`c-utxo:{utxoID}`):
```go
type CChainStoredUTXO struct {
    // Exactly matches Utxo API schema (X/C-Chain)
    Addresses           []string     `json:"addresses"`
    CreatedOnChainID    string       `json:"createdOnChainId"`
    ConsumedOnChainID   string       `json:"consumedOnChainId"`
    CreationTxHash      string       `json:"creationTxHash"`     // Different name!
    Locktime            int64        `json:"locktime"`           // Not platformLocktime
    OutputIndex         string       `json:"outputIndex"`        // STRING for C-Chain!
    Threshold           uint32       `json:"threshold"`
    Timestamp           int64        `json:"timestamp"`          // Not blockTimestamp
    UTXOId              string       `json:"utxoId"`
    UTXOType            string       `json:"utxoType"`           // "transfer" lowercase
    
    // C-Chain optional
    ConsumingTxHash      *string      `json:"consumingTxHash,omitempty"`
    ConsumingTxTimestamp *int64       `json:"consumingTxTimestamp,omitempty"`  // Not consumingBlockTimestamp
    Credentials          []Credential `json:"credentials,omitempty"`
    UTXOBytes            string       `json:"utxoBytes,omitempty"`
    
    // Asset (required but stored separately)
    Asset                AssetInfo    `json:"asset"`
}

type Credential struct {
    PublicKey  string `json:"publicKey"`
    Signature  string `json:"signature"`
}
```

### Double Write Flow

**C→P Export (processing C-Chain block):**

```go
func (u *UTXOs) processCChainExportTx(batch *pebble.Batch, tx *atomicTx, utx *UnsignedExportTx, blk CBlock) {
    txID := tx.ID()
    cChainID := u.getCChainID(blk.NetworkID)
    pChainID := utx.DestinationChain.String()  // where it's going
    
    credentials := extractCredentials(tx.Creds, utx.Ins)
    
    for i, out := range utx.ExportedOutputs {
        utxoID := avax.UTXOID{TxID: txID, OutputIndex: uint32(i)}.InputID().String()
        
        // 1. Write to C-Chain storage (Utxo schema)
        cUtxo := &CChainStoredUTXO{
            UTXOId:           utxoID,
            CreationTxHash:   txID.String(),       // C-Chain field name
            OutputIndex:      fmt.Sprintf("%d", i), // STRING
            Timestamp:        blk.Timestamp,        // C-Chain field name
            CreatedOnChainID: cChainID,
            ConsumedOnChainID: pChainID,
            UTXOType:         "transfer",           // lowercase
            Locktime:         extractLocktime(out),
            Credentials:      credentials[i],
            // ... other fields
        }
        saveCChainUTXO(batch, cUtxo)
        
        // 2. Write to P-Chain storage (PChainUtxo schema)
        pUtxo := &PChainStoredUTXO{
            UTXOId:           utxoID,
            TxHash:           txID.String(),        // P-Chain field name
            OutputIndex:      i,                    // NUMBER
            BlockTimestamp:   blk.Timestamp,        // P-Chain field name (approximate)
            CreatedOnChainID: cChainID,
            ConsumedOnChainID: pChainID,
            UTXOType:         "TRANSFER",           // uppercase
            PlatformLocktime: ptr(int64(extractLocktime(out))),
            // No BlockNumber - cross-chain UTXOs don't have a P-Chain creation block
            // ... other fields
        }
        savePChainUTXO(batch, pUtxo)
        
        // Index both
        batch.Set([]byte("c-addr:"+addr+":"+utxoID), nil, nil)
        batch.Set([]byte("p-addr:"+addr+":"+utxoID), nil, nil)
    }
}
```

**P-Chain ImportTx (processing P-Chain block, consumes C-Chain exported UTXO):**

```go
func (u *UTXOs) processPChainImportTx(batch *pebble.Batch, tx *txs.ImportTx, blk PBlock) {
    for _, input := range tx.ImportedInputs {
        utxoID := input.UTXOID.InputID().String()
        
        // Update BOTH stores
        
        // 1. Update C-Chain entry
        u.upsertCChainUTXO(batch, utxoID, map[string]any{
            "consumingTxHash":      tx.ID().String(),
            "consumingTxTimestamp": blk.Timestamp,  // C-Chain uses timestamp
        })
        
        // 2. Update P-Chain entry
        u.upsertPChainUTXO(batch, utxoID, map[string]any{
            "consumingTxHash":         tx.ID().String(),
            "consumingBlockNumber":    fmt.Sprintf("%d", blk.Height),
            "consumingBlockTimestamp": blk.Timestamp,
        })
    }
}
```

### API Handler (Trivial)

```go
func (u *UTXOs) handleUTXOs(w http.ResponseWriter, r *http.Request) {
    chainAlias := r.PathValue("blockchainId")  // "p-chain", "c-chain", "x-chain"
    
    var utxos []any
    switch chainAlias {
    case "p-chain":
        utxos = u.getPChainUTXOs(addresses, params)  // Returns PChainStoredUTXO
    case "c-chain":
        utxos = u.getCChainUTXOs(addresses, params)  // Returns CChainStoredUTXO
    case "x-chain":
        utxos = u.getXChainUTXOs(addresses, params)  // Returns same as C-Chain
    }
    
    // No transformation needed - storage IS the API format
    json.NewEncoder(w).Encode(map[string]any{
        "chainInfo": map[string]any{"chainName": chainAlias, "network": network},
        "utxos": utxos,
    })
}
```

### Advantages

1. **API is trivial** - fetch and return, zero transformation
2. **Schema correctness guaranteed** - Go compiler enforces field names/types
3. **No OR conditions** - each chain's query is simple prefix scan
4. **No runtime chain detection** - path determines which storage to query
5. **Semantic correctness** - cross-chain UTXOs ARE on both chains
6. **Storage = JSON** - matches your goal of "close to response format"

### Tradeoffs

1. **Double writes** for cross-chain UTXOs (~5-10% of all UTXOs)
2. **Double updates** when consumed cross-chain
3. **Storage overhead** - minimal, disk is cheap
4. **Two upsert functions** - but each is simpler than a unified one

### Why This Is Better Than Glacier's Approach

Glacier's single table with runtime transformation:
- Works for PostgreSQL with complex SQL projections
- Requires service layer to detect chain and transform

Our approach:
- Works for key-value stores (Pebble)
- No transformation layer
- Each chain's data is self-contained and API-ready

---

## ~~Old Implementation Plan~~ (Superseded)

*The unified storage approach below was replaced by the Separate Storage + Double Writes architecture. Kept for reference.*

<details>
<summary>Click to expand old plan</summary>

### Phase 1-5: Unified Storage Approach

This approach used a single `StoredUTXO` struct with all fields and runtime chain detection for response formatting.

**Why rejected**: 
- Field naming conflicts (`txHash` vs `creationTxHash`)
- Type conflicts (`outputIndex` int vs string)
- Runtime transformation adds complexity
- Doesn't match "store close to JSON response" goal

</details>

---

## Official Swagger Schema Comparison

From `00_glacier_swagger.json`, the API returns **different schemas**:
- P-Chain: `ListPChainUtxosResponse` with `PChainUtxo[]`
- X/C-Chain: `ListUtxosResponse` with `Utxo[]`

### `PChainUtxo` Schema (P-Chain)

**Required fields:**
```
addresses, asset, consumedOnChainId, createdOnChainId, utxoId,
amount*, assetId*, blockNumber, blockTimestamp, outputIndex, txHash, utxoType
(* deprecated)
```

**Optional fields:**
```
consumingTxHash, consumingBlockNumber, consumingBlockTimestamp,
platformLocktime, stakeableLocktime, staked, threshold,
utxoStartTimestamp, utxoEndTimestamp, utxoBytes, rewardType
```

**Key P-Chain specific:**
- `txHash` - creation transaction
- `blockNumber` (string), `blockTimestamp` (number)
- `consumingBlockNumber`, `consumingBlockTimestamp`
- `platformLocktime`, `stakeableLocktime`
- `staked`, `utxoStartTimestamp`, `utxoEndTimestamp`
- `outputIndex` is **NUMBER**
- `rewardType`

### `Utxo` Schema (X-Chain & C-Chain)

**Required fields:**
```
addresses, asset, consumedOnChainId, createdOnChainId, utxoId,
creationTxHash, locktime, outputIndex, threshold, timestamp, utxoType
```

**Optional fields:**
```
consumingTxHash, consumingTxTimestamp,
credentials, groupId, payload, utxoBytes
```

**Key X/C-Chain specific:**
- `creationTxHash` - NOT `txHash`!
- `timestamp` - NOT `blockTimestamp`!
- `locktime` - NOT `platformLocktime`!
- `consumingTxTimestamp` - NOT `consumingBlockTimestamp`!
- `outputIndex` is **STRING**
- `credentials` array
- `groupId`, `payload` (for NFTs)

### Side-by-Side Comparison

| Purpose | P-Chain (`PChainUtxo`) | X/C-Chain (`Utxo`) |
|---------|------------------------|-------------------|
| Creation tx | `txHash` | `creationTxHash` |
| Creation time | `blockTimestamp` | `timestamp` |
| Creation block | `blockNumber` | *(none)* |
| Locktime | `platformLocktime` | `locktime` |
| Consumption time | `consumingBlockTimestamp` | `consumingTxTimestamp` |
| Consumption block | `consumingBlockNumber` | *(none)* |
| Output index type | `number` | `string` |
| Staking | `staked`, `utxoStartTimestamp`, `utxoEndTimestamp` | *(none)* |
| Signatures | *(none)* | `credentials[]` |
| NFT data | *(none)* | `groupId`, `payload` |

---

## Credentials Implementation (from Glacier Source)

### Database Schema

Glacier's `utxos` table has:
```sql
public_key text,
signature text
```

Both are **comma-separated strings** (using `AGG_DELIMITER`) for multi-sig support.

### How Glacier Extracts Credentials

From `c-chain/parsers.ts`:

```typescript
export function parseCredentials(tx: IGetCChainTxByHashResult): Map<string, UtxoCredential[]> {
  const utxoCredentials = new Map<string, UtxoCredential[]>();
  
  tx.io_id.forEach((ioId: string, i: number) => {
    if (tx.consuming_transaction_hash[i] || tx.from_bool[i]) {
      // Split comma-separated values
      const outputPublicKeys = tx.public_key[i].toString().split(AGG_DELIMITER);
      const outputSignatures = tx.signature[i].toString().split(AGG_DELIMITER);
      
      const currentOutputCredential: UtxoCredential[] = outputPublicKeys.map(
        (_publicKey: string, idx: number) => ({
          publicKey: outputPublicKeys[idx],
          signature: outputSignatures[idx],
        })
      );
      
      utxoCredentials.set(ioId, currentOutputCredential);
    }
  });
  
  return utxoCredentials;
}
```

**Key insight**: Credentials are extracted from `tx.Creds` field of atomic transactions.

### Our Implementation Strategy

Extract credentials from `atomicTx.Creds` during indexing:

```go
type atomicTx struct {
    UnsignedAtomicTx `serialize:"true"`
    Creds            []verify.Verifiable `serialize:"true"`
}
```

Extract and store as base64-encoded strings:

```go
type Credential struct {
    PublicKey string `json:"publicKey"`  // Base64
    Signature string `json:"signature"`  // Base64
}

func extractCredentials(creds []verify.Verifiable) []Credential {
    var result []Credential
    for _, cred := range creds {
        if secpCred, ok := cred.(*secp256k1fx.Credential); ok {
            for _, sig := range secpCred.Sigs {
                // sig is [65]byte
                sigBase64 := base64.StdEncoding.EncodeToString(sig[:])
                
                // Public key recovery from signature + message hash
                pubKey := recoverPublicKey(unsignedTxBytes, sig)
                pubKeyBase64 := base64.StdEncoding.EncodeToString(pubKey)
                
                result = append(result, Credential{
                    PublicKey: pubKeyBase64,
                    Signature: sigBase64,
                })
            }
        }
    }
    return result
}
```

**Note**: Public key can be recovered from secp256k1 signature using ECDSA recovery with the unsigned tx bytes as the message.

---

## Data Flow Summary

### C-Chain ExportTx (creates C-Chain UTXO)

```
C-Chain Block → blockExtraData → atomicTx
  → ExportTx { DestinationChain: P, ExportedOutputs: [...] }
  → For each output:
      Create StoredUTXO {
        CreatedOnChainID: C-Chain
        ConsumedOnChainID: P-Chain (destination)
        CreationTxHash: ExportTx ID
        Timestamp: block timestamp
        Credentials: from tx.Creds
        ...
      }
```

### P-Chain ImportTx (consumes C-Chain UTXO)

```
P-Chain Block → txs
  → ImportTx { SourceChain: C, ImportedInputs: [...] }
  → For each input:
      Upsert StoredUTXO {
        ConsumingTxHash: ImportTx ID
        ConsumingTxTimestamp: block timestamp
      }
```

---

## Test Expectations

After implementation:

1. **C-Chain test**: Returns UTXOs with:
   - `chainName: c-chain`
   - `createdOnChainId: yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp` (C-Chain Fuji)
   - `creationTxHash`, `timestamp`, `locktime`, `credentials`
   - `utxoType: transfer` (lowercase)
   - `outputIndex: "0"` (string)

2. **P-Chain tests**: Continue to pass with current format

---

## Checklist

### Storage (Separate per chain)
- [ ] Create `PChainStoredUTXO` struct matching PChainUtxo API schema
- [ ] Create `CChainStoredUTXO` struct matching Utxo API schema
- [ ] Create `Credential` struct
- [ ] Add `savePChainUTXO()` and `saveCChainUTXO()` functions
- [ ] Add `upsertPChainUTXO()` and `upsertCChainUTXO()` functions
- [ ] Update key prefixes: `p-utxo:`, `c-utxo:`, `p-addr:`, `c-addr:`

### C-Chain Indexing (Double Writes)
- [ ] Update `processCChainExportTx()` to write to BOTH P and C storage
- [ ] Extract credentials from `atomicTx.Creds`
- [ ] Write C-Chain format: `creationTxHash`, `timestamp`, `outputIndex` as string
- [ ] Write P-Chain format: `txHash`, `blockTimestamp`, `outputIndex` as number

### P-Chain Indexing (Double Updates)
- [ ] Update `processPChainImportTx()` to update BOTH storages
- [ ] C-Chain update: `consumingTxHash`, `consumingTxTimestamp`
- [ ] P-Chain update: `consumingTxHash`, `consumingBlockNumber`, `consumingBlockTimestamp`

### API Handler
- [ ] Route to correct storage based on `blockchainId` path param
- [ ] `p-chain` → query `p-utxo:*` → return as-is
- [ ] `c-chain` → query `c-utxo:*` → return as-is
- [ ] Fix `chainInfo()` to return requested chain name

### Testing
- [ ] Re-run `go run ./cmd/test utxos`
- [ ] Verify C-Chain UTXOs match Glacier format
- [ ] Verify P-Chain UTXOs still work
- [ ] Verify cross-chain UTXOs appear in both chains

---

## Complexity Assessment

**Medium complexity** - Architecture is simpler than runtime transformation:
- Two storage structs instead of one unified struct
- Double writes add ~20 lines of code
- API handler becomes trivial (no transformation)
- Trade write complexity for read simplicity

Estimated time: 3-4 hours

---

## Summary: Final Architecture Decision

### Approach: Separate Storage + Double Writes

**NOT** following Glacier's single-table approach. Instead:

1. **Separate storage per chain** - P-Chain, C-Chain, X-Chain each have their own key space
2. **Store in API-ready format** - JSON fields match API response exactly
3. **Double writes for cross-chain** - Export writes to both source and destination chain storage
4. **Double updates on consumption** - Update both entries when UTXO is consumed

### Why This Is Better For Us

| Glacier (PostgreSQL) | Our Approach (Pebble KV) |
|---------------------|--------------------------|
| Single table, runtime transformation | Separate storage, no transformation |
| Complex SQL projections | Simple prefix scans |
| Service layer maps fields | Storage IS the API |
| `WHERE created_on = X OR consumed_on = X` | Just query `{chain}-utxo:*` |

### Implementation Steps

1. **Create `PChainStoredUTXO` struct** matching `PChainUtxo` API schema
2. **Create `CChainStoredUTXO` struct** matching `Utxo` API schema  
3. **Update C-Chain ExportTx indexing** to write to BOTH storages
4. **Update P-Chain ImportTx indexing** to update BOTH storages
5. **Simplify API handler** - just fetch and return, no transformation

### Key Differences in Storage

| Field | P-Chain Storage | C-Chain Storage |
|-------|-----------------|-----------------|
| Creation tx | `txHash` | `creationTxHash` |
| Creation time | `blockTimestamp` | `timestamp` |
| Output index | `int` | `string` |
| Locktime | `platformLocktime` | `locktime` |
| UTXO type | `"TRANSFER"` | `"transfer"` |
| Credentials | ❌ | ✅ |
| Block number | ✅ | ❌ |

### Estimated Effort

- Storage structs: 30 min
- Double write logic: 1 hour
- Update consumption logic: 30 min
- API handler simplification: 30 min
- Testing: 1 hour

**Total: ~3-4 hours**
