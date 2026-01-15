# Golden Data Tweaks

**Date**: 2026-01-15
**Purpose**: Fix known discrepancies in Snowflake golden data to enable accurate test validation

---

## Overview

The golden data exported from Snowflake contains known issues that cause test failures. This document describes the fixes applied to create corrected golden files for testing.

### File Naming Convention

- **Original files**: `original_*.csv.zst` (preserved for reference)
- **Fixed files**: `*.csv.zst` (used by tests)

---

## Tables Requiring Fixes

### 1. GUNZILLA_TRANSACTIONS.csv

**Issue**: `TRANSACTIONCOST` and `TRANSACTIONVALUE` fields have discrepancies between Snowflake data and correct RPC-derived values.

**Statistics**:
- Total transactions: 9,887
- TRANSACTIONCOST discrepancies: 931 (9.4%)
- TRANSACTIONVALUE discrepancies: 42 (0.4%)
- TRANSACTIONGASPRICE discrepancies: 1 (EIP-1559 maxFee vs effectiveGasPrice)
- Total fields fixed: 974

**TRANSACTIONCOST Discrepancy Distribution**:

| Range | Count | % of Discrepancies |
|-------|-------|-------------------|
| < 100 wei | 32 | 3.4% |
| 100-999 wei | 894 | 96.0% |
| 10K-1M wei | 4 | 0.4% |
| > 1T wei (EIP-1559) | 1 | 0.1% |

**TRANSACTIONVALUE Discrepancy Distribution**:

| Range | Count |
|-------|-------|
| < 100 wei | 42 |

All VALUE discrepancies are tiny precision issues (2-50 wei).

---

### 2. GUNZILLA_INTERNAL_TRANSACTIONS.csv

**Issue**: `VALUE` field has precision discrepancies between Snowflake data and correct RPC-derived values.

**Statistics**:
- Total internal transactions: 207,990
- VALUE discrepancies: 3,722 (1.8%)

**VALUE Discrepancy Distribution**:

| Range | Count | % of Discrepancies |
|-------|-------|-------------------|
| < 100 wei | 2,476 | 66.5% |
| 100-999 wei | 1,242 | 33.4% |
| 1K-10K wei | 1 | 0.03% |
| 100K-1M wei | 3 | 0.08% |

**Top 5 Largest VALUE Discrepancies**:

| TX | Diff | Snowflake | RPC |
|----|------|-----------|-----|
| 0x82cb8f1b... | 805,888 wei | 18025019214290000805888 | 18025019214290000000000 |
| 0x6f7d25bd... | 166,912 wei | 15979473870130000166912 | 15979473870130000000000 |
| 0x8a9b120b... | 131,072 wei | 1984339999999999868928 | 1984340000000000000000 |
| 0xd8b4547d... | 3,072 wei | 43028689389999996928 | 43028689390000000000 |
| 0x7559a8ac... | 960 wei | 17999999597668648960 | 17999999597668648000 |

---

## Top 5 Largest Discrepancies Fixed

### 1. TX `0xd8b4547d5e54f5ec5099b275c6ed78bfdb20f849c7d690b73d5759f3c832ee61`

**Root Cause**: EIP-1559 `maxFeePerGas` vs `effectiveGasPrice`

| Field | Snowflake | Correct | Diff |
|-------|-----------|---------|------|
| TRANSACTIONCOST | 43,029,004,390,000,000,000 | 43,028,752,390,000,000,000 | 252,000,000,000,000 wei |

- **TX Type**: 2 (EIP-1559)
- **maxFeePerGas**: 15 Gwei
- **effectiveGasPrice**: 3 Gwei
- **Gas Limit**: 21,000
- **Diff in GUN**: 0.000252 GUN

**Explanation**: Snowflake used `maxFeePerGas × gasLimit + value` instead of `effectiveGasPrice × gasLimit + value`.

---

### 2. TX `0x8a9b120b43170f9a...`

**Root Cause**: Fractional Gwei truncation

| Field | Snowflake | Correct | Diff |
|-------|-----------|---------|------|
| TRANSACTIONCOST | 2,111,000,193,309,000,000,000 | 2,111,000,193,309,000,193,309 | 193,309 wei |

- **TX Type**: 2 (EIP-1559)
- **Gas Price**: 1.000000001 Gwei (sub-Gwei precision)
- **Gas Limit**: 193,309 (matches the lost wei)

**Explanation**: The fractional wei portion (193,309 × 1 wei) was truncated.

---

### 3. TX `0x82da0f330ffd63bd...`

**Root Cause**: Fractional Gwei truncation

| Field | Snowflake | Correct | Diff |
|-------|-----------|---------|------|
| TRANSACTIONCOST | 454,000,250,000,000,200,000 | 454,000,250,000,000,250,000 | 50,000 wei |

- **TX Type**: 0 (Legacy)
- **Partial truncation**: 200,000 stored instead of 250,000

---

### 4. TX `0xddc05d4a6c86a537...`

**Root Cause**: Fractional Gwei truncation

| Field | Snowflake | Correct | Diff |
|-------|-----------|---------|------|
| TRANSACTIONCOST | 1,773,000,037,800,000,000,000 | 1,773,000,037,800,000,031,500 | 31,500 wei |

- **TX Type**: 0 (Legacy)
- **Gas Limit**: 31,500 (matches the lost wei)

---

### 5. TX `0xf24f9278d2277601...`

**Root Cause**: Fractional Gwei truncation

| Field | Snowflake | Correct | Diff |
|-------|-----------|---------|------|
| TRANSACTIONCOST | 1,400,000,021,000,000,000,000 | 1,400,000,021,000,000,021,000 | 21,000 wei |

- **TX Type**: 2 (EIP-1559)
- **Gas Price**: 1.000000001 Gwei
- **Gas Limit**: 21,000 (matches the lost wei)

---

## Fix Applied

The `TRANSACTIONCOST` field is recalculated using:

```
TRANSACTIONCOST = effectiveGasPrice × gasLimit + value
```

Where `effectiveGasPrice` comes from the transaction receipt (actual price paid, not maximum authorized).

---

## Files

| File | Description |
|------|-------------|
| `original_GUNZILLA_TRANSACTIONS.csv.zst` | Original Snowflake export (preserved) |
| `GUNZILLA_TRANSACTIONS.csv.zst` | Fixed version (used by tests) |

---

## Verification

After applying fixes:
- All 9,887 transactions should match RPC-derived values
- No TRANSACTIONCOST discrepancies expected

### Verification Example (TX #1 - Largest Fix)

```
TX: 0xd8b4547d5e54f5ec5099b275c6ed78bfdb20f849c7d690b73d5759f3c832ee61

ORIGINAL (Snowflake):  43,029,004,390,000,000,000 wei (43.02900439 GUN)
FIXED:                 43,028,752,390,000,000,000 wei (43.02875239 GUN)
Difference fixed:              252,000,000,000,000 wei (0.000252 GUN)
```

---

## Applied: 2026-01-15

### GUNZILLA_TRANSACTIONS.csv
- ✅ `original_GUNZILLA_TRANSACTIONS.csv.zst` created (preserved original)
- ✅ `GUNZILLA_TRANSACTIONS.csv.zst` replaced with fixed version
- ✅ 931 TRANSACTIONCOST values corrected
- ✅ 42 TRANSACTIONVALUE values corrected
- ✅ 1 TRANSACTIONGASPRICE value corrected
- ✅ Largest fix: 0.000252 GUN (EIP-1559 maxFee vs effectiveGasPrice)

### GUNZILLA_INTERNAL_TRANSACTIONS.csv
- ✅ `original_GUNZILLA_INTERNAL_TRANSACTIONS.csv.zst` created (preserved original)
- ✅ `GUNZILLA_INTERNAL_TRANSACTIONS.csv.zst` replaced with fixed version
- ✅ 3,722 VALUE values corrected
- ✅ Largest fix: 805,888 wei (fractional precision)

---

## Test Results After Fixes

```
=== RUN   TestTransformBlocks
--- PASS: TestTransformBlocks (1.62s)
=== RUN   TestTransformTransactions
--- PASS: TestTransformTransactions (1.30s)
=== RUN   TestTransformReceipts
--- PASS: TestTransformReceipts (1.24s)
=== RUN   TestTransformLogs
--- PASS: TestTransformLogs (1.34s)
=== RUN   TestTransformInternalTxs
--- PASS: TestTransformInternalTxs (2.35s)
=== RUN   TestTransformMessages
--- PASS: TestTransformMessages (1.27s)
PASS
```

**All 6 tests PASS after golden data fixes!**
