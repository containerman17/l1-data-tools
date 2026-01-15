# Precision Loss Analysis

**Date**: 2026-01-15
**Chain**: Gunzilla
**Issue**: TransactionCost precision differences between our implementation and Snowflake golden data

---

## Summary

- **Total transactions**: 9,887
- **Exact matches**: 8,956 (90.6%)
- **With differences**: 931 (9.4%)

---

## Difference Distribution

| Range | Count | % |
|-------|-------|---|
| < 10 wei | 17 | 1.8% |
| 10-99 wei | 15 | 1.6% |
| 100-999 wei | 894 | **96.0%** |
| 1000-9999 wei | 0 | 0% |
| >= 10000 wei | 5 | 0.5% |

---

## Two Distinct Bug Types Identified

After verifying against block explorer data, we identified **two separate bugs** in the Snowflake golden data:

### Bug Type 1: Incorrect Value Fields

Some transactions have completely wrong Value fields unrelated to precision truncation.

| TX | Golden Value | Explorer Value | Diff |
|----|--------------|----------------|------|
| #1 | 43.02900439 GUN | 43.02868939 GUN | +0.000315 GUN |

This is not a rounding error — the source Value is simply wrong.

### Bug Type 2: Fractional Gwei Truncation

When gas price has sub-Gwei precision (e.g., 1.000000001 Gwei), the golden data loses the `gasLimit × fractionalWei` component.

| TX | Gas Limit | Gas Price (wei) | Fractional Component | Lost |
|----|-----------|-----------------|---------------------|------|
| #2 | 193,309 | 1,000,000,001 | 193,309 wei | 193,309 wei (100%) |
| #3 | 250,000 | 1,000,000,001 | 250,000 wei | 50,000 wei (20%) |
| #4 | 31,500 | 1,200,000,001 | 31,500 wei | 31,500 wei (100%) |

---

## Block Explorer Verification

### TX #1: `0xd8b4547d...` (Incorrect Value)

Explorer data:
- **Value**: 43.02868939 GUN = 43,028,689,390,000,000,000 wei
- **Gas**: 21,000
- **Gas Price**: 3 Gwei = 3,000,000,000 wei

```
TransactionCost = (21,000 × 3,000,000,000) + 43,028,689,390,000,000,000
               = 63,000,000,000,000 + 43,028,689,390,000,000,000
               = 43,028,752,390,000,000,000 wei ✅ Matches our generated value
```

- **Golden**: 43,029,004,390,000,000,000 wei (implies Value of 43.02894139 GUN)
- **Generated**: 43,028,752,390,000,000,000 wei
- **Verdict**: Golden has wrong Value (+252,000,000,000,000 wei = ~$0.000008)

### TX #2: `0x8a9b120b...` (Fractional Gwei Truncation)

Explorer data:
- **Value**: 2,111 GUN = 2,111,000,000,000,000,000,000 wei
- **Gas Limit**: 193,309
- **Gas Price**: 1.000000001 Gwei = 1,000,000,001 wei

```
TransactionCost = (193,309 × 1,000,000,001) + 2,111,000,000,000,000,000,000
               = 193,309,000,193,309 + 2,111,000,000,000,000,000,000
               = 2,111,000,193,309,000,193,309 wei ✅ Matches our generated value
```

- **Golden**: 2,111,000,193,309,000,000,000 wei (missing 193,309 wei)
- **Generated**: 2,111,000,193,309,000,193,309 wei
- **Verdict**: Golden truncated fractional Gwei precision

### TX #3: `0x82da0f33...` (Fractional Gwei Truncation)

Explorer data:
- **Value**: 454 GUN = 454,000,000,000,000,000,000 wei
- **Gas Limit**: 250,000
- **Gas Price**: 1.000000001 Gwei = 1,000,000,001 wei

```
TransactionCost = (250,000 × 1,000,000,001) + 454,000,000,000,000,000,000
               = 250,000,000,250,000 + 454,000,000,000,000,000,000
               = 454,000,250,000,000,250,000 wei ✅ Matches our generated value
```

- **Golden**: 454,000,250,000,000,200,000 wei (shows 200,000 instead of 250,000)
- **Generated**: 454,000,250,000,000,250,000 wei
- **Verdict**: Golden lost 50,000 wei (partial truncation)

### TX #4: `0xddc05d4a...` (Fractional Gwei Truncation)

Explorer data:
- **Value**: 1,773 GUN = 1,773,000,000,000,000,000,000 wei
- **Gas Limit**: 31,500
- **Gas Price**: 1.200000001 Gwei = 1,200,000,001 wei

```
TransactionCost = (31,500 × 1,200,000,001) + 1,773,000,000,000,000,000,000
               = 37,800,000,031,500 + 1,773,000,000,000,000,000,000
               = 1,773,000,037,800,000,031,500 wei ✅ Matches our generated value
```

- **Golden**: 1,773,000,037,800,000,000,000 wei (missing 31,500 wei)
- **Generated**: 1,773,000,037,800,000,031,500 wei
- **Verdict**: Golden truncated fractional Gwei precision

---

## Dollar Value Context

While the wei numbers appear large, actual dollar impact is negligible:

| Difference | Wei | GUN | USD (@ $0.031/GUN) |
|------------|-----|-----|-------------------|
| TX #1 | 252,000,000,000,000 | 0.000252 | ~$0.000008 |
| TX #2 | 193,309 | 0.000000000193 | ~$0.000000006 |
| TX #4 | 31,500 | 0.000000000032 | ~$0.000000001 |

All differences are sub-cent, but TX #1's incorrect Value field is a different class of bug than the fractional Gwei truncation.

---

## Root Cause Theory

### Bug Type 1 (Incorrect Values)
Unknown source — possibly:
- Data corruption in pipeline
- Race condition during ingestion
- Source node returning stale/incorrect data

### Bug Type 2 (Fractional Gwei Truncation)
Likely occurs in:
1. **Snowflake NUMBER type** - Limited to 38 digits of precision
2. **Serialization** - Float conversion during JSON/Kafka transport
3. **avalanche-data-producer** - Unlikely since it uses `big.Int`

---

## Our Implementation

We use proper `math/big.Int` arithmetic:

```go
gasPrice := parseHexBigInt(tx.GasPrice)  // big.Int
gas := parseHexBigInt(tx.Gas)            // big.Int
value := parseHexBigInt(tx.Value)        // big.Int

gasCost := new(big.Int).Mul(gas, gasPrice)
cost := new(big.Int).Add(gasCost, value)
```

**No floating point, full uint256 precision.**

---

## Conclusion

Our implementation is mathematically correct. The golden data has two types of errors:

1. **Incorrect Value fields** — More concerning, source unknown
2. **Fractional Gwei truncation** — Systematic, negligible impact

### Recommended Actions

1. **Accept our calculated TransactionCost as correct** — Verified against block explorer
2. **Skip TransactionCost comparison in tests** — Or use tolerance-based comparison
3. **Investigate Bug Type 1** — Incorrect Values warrant further investigation in the Snowflake pipeline

---

## Analysis Tool

Location: `cmd/analyze_diff/main.go`

```bash
cd /home/ubuntu/l1-data-tools
go run ./exporters/snowflake/evm/cmd/analyze_diff/...
```

---

## Deep Dive Investigation (2026-01-15)

### Data Pipeline Architecture

The data flows through the following pipeline:

```
[Node RPC] → [avalanche-data-producer] → [Kafka/ORC] → [Spark/Airflow] → [Snowflake]
                    (Go)                                   (Scala)
```

### Source Code Locations

#### 1. avalanche-data-producer (Go)
- **Entity generation**: `/home/ubuntu/avalanche-data-producer/entities/subnet/entities.go`
  - Line 91: `TransactionCost: trx.Cost().String()`
  - Line 92: `TransactionValue: trx.Value().String()`
- **C-Chain entity**: `/home/ubuntu/avalanche-data-producer/entities/cchain/entities.go`
  - Same pattern at Line 80-81
- **go.mod dependencies**: Uses `github.com/ava-labs/libevm v1.13.15-...`

#### 2. libevm Transaction.Cost() Function
- **Source**: `/home/ubuntu/go/pkg/mod/github.com/ava-labs/libevm@v1.13.15-0.20251016142715-1bccf4f2ddb2/core/types/transaction.go`
- **Lines 312-320**:
```go
// Cost returns (gas * gasPrice) + (blobGas * blobGasPrice) + value.
func (tx *Transaction) Cost() *big.Int {
    total := new(big.Int).Mul(tx.GasPrice(), new(big.Int).SetUint64(tx.Gas()))
    if tx.Type() == BlobTxType {
        total.Add(total, new(big.Int).Mul(tx.BlobGasFeeCap(), new(big.Int).SetUint64(tx.BlobGas())))
    }
    total.Add(total, tx.Value())
    return total
}
```
- **Key insight**: Uses `tx.Gas()` (gas LIMIT, not GasUsed) and `tx.Value()`

#### 3. avalanche-data-airflow (Spark/Scala)
- **Gunzilla Spark job**: `/home/ubuntu/avalanche-data-airflow/src/dags/spark/prod/snowflake/snowflake_marketplace_gunzilla.scala`
  - Line 66: `$"transactioncost".cast("double")` ← **CAST TO DOUBLE!**
  - Line 67: `$"transactionvalue".cast("double")` ← **CAST TO DOUBLE!**
  - Line 134: `$"value".cast("double")` (internal transactions)
- **Same pattern in all chains**: cchain, nexon, dexalot, dfk

#### 4. Snowflake DDL
- **Location**: `/home/ubuntu/avalanche-data-airflow/src/dags/sql/snowflake/marketplace/cchain_ddl.sql`
  - Line 37: `transactioncost NUMBER(38, 0)`
- **Delta SQL**: `/home/ubuntu/avalanche-data-airflow/src/dags/sql/delta/transactions_delta.sql`
  - Line 13: `CAST(dset.transactioncost AS DECIMAL(38, 0))`

---

### Key Finding: Double Precision is NOT the Cause

Ran Scala test in Docker container:
```scala
val correctCost = BigInt("43028752390000000000")
val goldenCost = BigInt("43029004390000000000")

// Testing double precision
val d1 = correctCost.toDouble  // 4.302875239E19
val d2 = goldenCost.toDouble   // 4.302900439E19

// Are they equal as doubles? NO - they are distinguishable
println(s"Are they equal? ${d1 == d2}")  // false

// ULP (Unit in Last Place) at this magnitude
val ulp = java.lang.Math.ulp(4.3e19)  // = 8192
// Our diff is 252 trillion, which is ~30 billion ULPs - WAY larger than precision loss
```

**Conclusion**: The 0.000252 GUN difference (252,000,000,000,000 wei) is **much larger** than IEEE 754 double precision loss at this magnitude (ULP = 8192 wei).

---

### Root Cause Analysis: Incorrect Value Field

Mathematical proof:
```
Golden TransactionCost: 43,029,004,390,000,000,000 wei
Our TransactionCost:    43,028,752,390,000,000,000 wei
Difference:                     252,000,000,000,000 wei = 0.000252 GUN

Gas cost = 21,000 × 3,000,000,000 = 63,000,000,000,000 wei (same for both)

Implied Golden Value = 43,029,004,390,000,000,000 - 63,000,000,000,000
                     = 43,028,941,390,000,000,000 wei
                     = 43.02894139 GUN

Explorer Value       = 43,028,689,390,000,000,000 wei
                     = 43.02868939 GUN

Value Difference     = 43,028,941,390,000,000,000 - 43,028,689,390,000,000,000
                     = 252,000,000,000,000 wei = 0.000252 GUN ✓
```

The Golden data has the **wrong Value field** embedded in the TransactionCost calculation. The Cost() function in libevm uses `tx.Value()`, so if Value is wrong at the producer level, TransactionCost will be wrong.

---

### Potential Causes (Still Under Investigation)

1. **Race condition in producer**: Transaction Value could have been overwritten/corrupted
2. **Node returning stale data**: During a reorg or sync issue
3. **Serialization bug**: Somewhere between RPC and Kafka
4. **Historical producer bug**: May have been fixed since, but old data persists

---

### Why This is NOT Precision Loss

| Issue Type | Typical Magnitude | Our Magnitude |
|------------|------------------|---------------|
| Double precision loss | < 10,000 wei | 252 trillion wei |
| Fractional Gwei truncation | < 1M wei | 252 trillion wei |
| TX #1 discrepancy | - | 252 trillion wei |

TX #1 is **25 billion times larger** than typical precision loss. It's a **data corruption bug**, not precision loss.

---

### Recommendations

1. **Do not dismiss as precision loss**: This is a different class of bug
2. **Investigate producer logs**: Look for the specific transaction hash around ingestion time
3. **Compare with Kafka raw data**: To isolate where corruption occurred (producer vs Spark)
4. **Our implementation is correct**: Verified against block explorer

---

### Snowflake Schema Analysis

From `02_data_structures.md`, Snowflake stores:
- `TRANSACTIONCOST | NUMBER(38,0)` - Full precision, no loss
- `TRANSACTIONVALUE | NUMBER(38,0)` - Full precision, no loss

The pipeline flow:
```
Producer (big.Int.String()) → Kafka (string) → Spark (.cast("double")) → Snowflake (NUMBER)
                                                     ↑
                                            SUSPECTED LOSS POINT
```

**BUT**: Scala testing proves Double precision loss (ULP = 8192 wei) is too small:
```scala
val d1 = 43028689390000000000.0  // Correct Value
val d2 = 43028941390000000000.0  // Implied Golden Value
// These are DISTINGUISHABLE as doubles - diff is 30 billion ULPs
println(d2 - d1)  // 252000000000000 - preserved correctly!
```

---

### Final Conclusion: EIP-1559 MaxFee vs EffectiveGasPrice

**ROOT CAUSE IDENTIFIED!**

For TX `0xd8b4547d...` (Type 2 / EIP-1559):

| Field | Value | Source |
|-------|-------|--------|
| `maxFeePerGas` | 15 Gwei | RPC: `0x37e11d600` |
| `effectiveGasPrice` | 3 Gwei | Receipt: `0xb2d05e00` |
| Gas Limit | 21,000 | |
| Value | 43.02868939 GUN | Correct in both |

**libevm's `tx.Cost()` uses `maxFeePerGas` for Type 2 transactions:**
```
ProducerCost = 21,000 × 15 Gwei + Value = 43.02900439 GUN  ← What Snowflake has
ExplorerCost = 21,000 × 3 Gwei + Value  = 43.02875239 GUN  ← What explorer shows
Difference   =          12 Gwei × 21,000 = 0.000252 GUN    ✓ MATCHES!
```

**This is NOT a bug!** It's a semantic difference:
- Snowflake stores **maximum possible cost** (what could have been charged)
- Explorer shows **actual cost** (what was paid)

For Type 2 transactions, the user authorizes spending up to `maxFeePerGas`, but only `baseFee + priorityFee` is actually charged. The difference is refunded.

**Our implementation choice:**
- If we want to match Snowflake: use `GasPrice × GasLimit + Value` (max cost)
- If we want actual cost: use `effectiveGasPrice × GasUsed + Value`

This is a **data model decision**, not a precision loss issue.

---

### Verification from Raw RPC Data

```json
// From GUNZILLA_BLOCKS.jsonl.zst - raw RPC response
{
  "hash": "0xd8b4547d5e54f5ec5099b275c6ed78bfdb20f849c7d690b73d5759f3c832ee61",
  "value": "0x255249d064191cc00",  // = 43028689390000000000 = 43.02868939 GUN ✓
  "gas": "0x5208",                    // = 21000
  "gasPrice": "0xb2d05e00",           // = 3000000000 (effectiveGasPrice for type 2)
  "maxFeePerGas": "0x37e11d600",      // = 15000000000 (15 Gwei)
  "type": "0x2"                       // EIP-1559
}

// From receipt
"effectiveGasPrice": "0xb2d05e00"     // = 3 Gwei (actual price paid)
```

Value field is CORRECT in RPC. The TransactionCost difference is due to gasPrice semantics.

---

### Summary of All Discrepancy Types

1. **0.000252 GUN type (TX #1)**: EIP-1559 `maxFeePerGas` vs `effectiveGasPrice` - **NOT A BUG**
2. **193,309 wei type (TX #2-4)**: Fractional Gwei truncation - legacy precision issue
3. **< 10,000 wei types**: Sub-Gwei precision loss in historical pipeline

This is **data corruption**, not precision loss. Further investigation requires:
1. Access to raw Kafka messages for this transaction
2. Producer logs from ingestion time
3. Direct RPC query to node for the transaction
