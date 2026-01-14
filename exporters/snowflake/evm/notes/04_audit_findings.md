# Audit Findings: avalanche-data-producer Cross-Reference

**Date**: 2026-01-14  
**Status**: Pending Review  
**Source Repo**: `~/avalanche-data-producer/` (github.com/ava-labs/avalanche-data-producer)

---

## Overview

This document captures discrepancies and items requiring verification discovered during cross-reference of our design documents against the actual `avalanche-data-producer` source code.

---

## ⚠️ Discrepancies to Verify

### 1. TRACE_POSITION vs CallIndex Mismatch

**Location**: `C_INTERNAL_TRANSACTIONS` table

| Our Schema | Producer Field |
|------------|----------------|
| `TRACE_POSITION` (column 17) | Not present in `FlatCall` struct |
| `CALLINDEX` (column 16) | `CallIndex` field exists |

**Source Reference**: `utils/utilities.go:44-57`

```go
type FlatCall struct {
    Type         string         `json:"type"`
    From         common.Address `json:"from"`
    To           common.Address `json:"to"`
    Value        string         `json:"value"`
    Gas          string         `json:"gas"`
    GasUsed      string         `json:"gasUsed"`
    Revert       bool           `json:"revert"`
    Error        string         `json:"error"`
    RevertReason string         `json:"revertReason"`
    Input        string         `json:"input"`
    Output       string         `json:"output"`
    CallIndex    string         `json:"callIndex"`  // ← Only this exists
}
```

**Question**: Does the current Airflow pipeline derive `TRACE_POSITION` from `CallIndex`, or is there a separate source? Need to check `avalanche-data-airflow` for the mapping.

**Action**: [ ] Verify with existing pipeline code

---

### 2. Missing Fields in C_MESSAGES Schema

**Location**: `C_MESSAGES` table vs producer output

The producer generates **more fields** than our Snowflake schema captures:

| Field in Producer | In Our Schema? |
|-------------------|----------------|
| `transactionMessageFrom` | ✅ Yes |
| `transactionMessageTo` | ✅ Yes |
| `transactionMessageGasPrice` | ✅ Yes |
| `transactionMessageGas` | ❌ No |
| `transactionMessageGasFeeCap` | ❌ No |
| `transactionMessageValue` | ❌ No |

**Source Reference**: `test/testdata/evm_payload.json:45-54`

```json
"transactionMessage": {
  "blockTime": 1685589857,
  "transactionHash": "0x9dcc5e42f23de2cb5ba1db290753178e69e00e23ff6bd613b3d5089eb7cbddd2",
  "transactionMessageTo": "0x2F6F07CDcf3588944Bf4C42aC74ff24bF56e7590",
  "transactionMessageFrom": "0xbc6eb39d2fbF2414fEAE73d662C69E14354385f2",
  "transactionMessageGasPrice": 26500000000,
  "transactionMessageGas": 46206,           // ← Missing from schema
  "transactionMessageGasFeeCap": 35250000000, // ← Missing from schema
  "transactionMessageValue": 0               // ← Missing from schema
}
```

**Question**: Is this intentional (fields deemed unnecessary) or an oversight in the original pipeline?

**Action**: [ ] Decide whether to add these fields to our exporter

---

### 3. Empty EffectiveGasPrice in Receipts

**Location**: `C_RECEIPTS.TRANSACTIONRECEIPTEFFECTIVEGASPRICE`

**Observation**: In test data, this field is sometimes an empty string:

```json
"transactionReceiptEffectiveGasPrice": ""
```

**Source Reference**: `test/testdata/evm_payload.json:43`

**Implication**: Pre-London fork blocks (before EIP-1559) may have empty effective gas price. Our transformer must handle:
- Empty string `""` → `NULL` in Snowflake
- Not fail on missing data

**Action**: [ ] Ensure transformer converts empty strings to NULL

---

### 4. Address Case Inconsistency

**Location**: Multiple tables

The producer uses **inconsistent address casing**:

| Function | Casing Applied |
|----------|----------------|
| `evmTransaction()` | `strings.ToLower(trx.To().String())` — **lowercased** |
| `evmTransactionMessage()` | `msgFrom.From.String()` — **mixed case (checksummed)** |
| `evmTransactionLogs()` | `l.Address.String()` — **mixed case (checksummed)** |

**Source References**:
- `entities/cchain/entities.go:64` (lowercased)
- `entities/cchain/entities.go:120` (not lowercased)
- `entities/cchain/entities.go:135` (not lowercased)

**Risk**: JOINs between tables may fail due to case mismatch:
```sql
-- This could fail if addresses have different case
SELECT * FROM c_transactions t
JOIN c_messages m ON t.transactionhash = m.transactionhash
WHERE t.transactionto = m.transactionmessageto  -- Case mismatch!
```

**Action**: [ ] Normalize all addresses to lowercase in transformer

---

### 5. BlockRoot vs BlockReceiptsRoot Duplication

**Location**: `C_BLOCKS` schema

**Observation**: In the producer, both fields point to the same value:

```go
// entities/cchain/entities.go:47-49
BlockRoot:         evmBlock.Root().String(),      // receipts root
BlockStateRoot:    evmBlock.Root().String(),      // same value!
```

But our schema has:
- `BLOCKSTATEROOT` (column 11)
- `BLOCKRECEIPTSROOT` (column 9)

**Source Reference**: `entities/cchain/entities.go:47-49`

**Question**: Is `evmBlock.Root()` the receipts root or state root? The naming in the producer is confusing. Need to verify which is which.

**Action**: [ ] Verify correct mapping of block roots

---

## ✅ Verified Claims (No Action Needed)

For reference, these claims from our design documents were **confirmed correct**:

1. ✅ `core.TransactionToMessage()` is used to generate message data
2. ✅ Messages provide effective gas price (post-EIP-1559)
3. ✅ Producer writes to Kafka with proper batching
4. ✅ All 6 data types present: blocks, transactions, receipts, logs, internal_txs, messages
5. ✅ Trace flattening recursively handles nested calls with `CallIndex`
6. ✅ Logs support 4 topic pairs (hex + decimal)
7. ✅ Receipts include effective gas price field
8. ✅ C-Chain specific fields (`BlockExtDataGasUsed`, `BlockGasCost`) are captured

---

## Next Steps

1. [ ] Review each discrepancy with team
2. [ ] Check `avalanche-data-airflow` for how existing pipeline handles these
3. [ ] Update `02_data_structures.md` if schema changes needed
4. [ ] Document decisions for each item above

---

## References

| File | Purpose |
|------|---------|
| `~/avalanche-data-producer/types/chain_structs.go` | All data structure definitions |
| `~/avalanche-data-producer/entities/cchain/entities.go` | C-Chain transformation logic |
| `~/avalanche-data-producer/utils/utilities.go` | Trace flattening (`FlatCall`, `TransformTrace`) |
| `~/avalanche-data-producer/test/testdata/evm_payload.json` | Example payload structure |
| `~/avalanche-data-producer/stream/kafka_producer.go` | Kafka producer implementation |
