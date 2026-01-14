# C_MESSAGES Table Clarification

**Date**: 2026-01-10  
**Status**: Resolved

---

## Initial Confusion

The original requirements mentioned "ICM Messages (Avalanche Interchain Messaging)" as the 6th table. After investigation, we discovered this was a misunderstanding of what `C_MESSAGES` actually contains.

---

## What C_MESSAGES Actually Is

**C_MESSAGES is NOT Interchain Messaging.** It contains the EVM `core.Message` representation of **every** transaction.

### The Name is Misleading

"Messages" here refers to `core.Message` from geth/coreth — the internal EVM representation of a transaction after signature recovery. It has nothing to do with cross-chain messaging protocols.

### 1:1 with Transactions

Every transaction has exactly one corresponding message record. There is no filtering — it's a complete denormalized view.

---

## Why It Exists (The Key Value)

The main difference from `C_TRANSACTIONS` is the **effective gas price** for EIP-1559 transactions:

| Field | C_TRANSACTIONS | C_MESSAGES |
|-------|----------------|------------|
| Gas Price | `transactionGasPrice` (raw, what tx specified) | `transactionMessageGasPrice` (**effective**, after base fee calculation) |

For pre-EIP-1559 transactions, these values are identical. For EIP-1559 transactions, C_MESSAGES provides the actual price paid.

---

## Schema

```sql
CREATE TABLE C_MESSAGES (
    BLOCKHASH                   VARCHAR,
    BLOCKNUMBER                 NUMBER(38,0),
    BLOCKTIMESTAMP              NUMBER(38,0),
    TRANSACTIONHASH             VARCHAR,
    TRANSACTIONMESSAGEFROM      VARCHAR,    -- Sender (recovered from signature)
    TRANSACTIONMESSAGETO        VARCHAR,    -- Recipient
    TRANSACTIONMESSAGEGASPRICE  NUMBER(38,0), -- Effective gas price
    PARTITION_DATE              DATE
);
```

---

## Data Flow

```
RPC Node (eth_getBlockByNumber)
        ↓
        ↓ coreth: core.TransactionToMessage(tx, signer, baseFee)
        ↓
[avalanche-data-producer]  ← External repo (github.com/ava-labs/avalanche-data-producer)
        ↓
        ↓ Produces Kafka messages with CChainPayload
        ↓ Each transaction includes "transactionMessage" object
        ↓
s3://avalanche-data-platform/.../stream/messages/  (ORC files)
        ↓
[avalanche-data-airflow]  ← Spark jobs read ORC, write to Snowflake
        ↓
AVALANCHE.PRIMARY.C_MESSAGES
```

---

## Research Sources

### Repository: `~/avalanche-data-airflow/`

| File | Purpose |
|------|---------|
| `src/dags/spark/prod/snowflake/snowflake_marketplace_cchain.scala` (lines 96-107) | Reads ORC from S3, writes to C_MESSAGES |
| `src/dags/sql/catalog/prod/avax_catalog.sql` (lines 134-153) | Hive DDL for external table |
| `src/dags/sql/snowflake/marketplace/cchain_ddl.sql` | Snowflake DDL |

### Repository: `~/analytics/`

| File | Purpose |
|------|---------|
| `pipelines/evm_shared/mapper.go` (lines 82-99) | Calls `core.TransactionToMessage()` |
| `pipelines/evm_shared/types.go` | Defines `IntTransaction` struct with `MsgFrom`, `MsgGasPrice` |
| `pipelines/dlvalidation/fixtures/kafka_fixture_block_32587066.json` | Example Kafka payload showing `transactionMessage` structure |
| `go.mod` (line 9) | References `avalanche-data-producer v0.2.59` |

### Repository: `~/avalanche-data-producer/`

- `github.com/ava-labs/avalanche-data-producer` — The actual producer that extracts transaction messages and writes to Kafka/S3

---

## What About Actual ICM/Teleporter?

If you need real Interchain Messaging data:

- **Teleporter messages** are stored in separate tables (`teleporter_messages_v1`, etc.)
- Handled by a different pipeline: `~/analytics/pipelines/teleporter_aurora/`
- Part of the "Britannica" system, not this exporter

---

## Conclusion

For this exporter, `C_MESSAGES` is:
- ✅ 1:1 with transactions (trivial to generate)
- ✅ Provides effective gas price (useful for EIP-1559)
- ❌ NOT Interchain Messaging
- ❌ NOT a filtered subset of transactions
