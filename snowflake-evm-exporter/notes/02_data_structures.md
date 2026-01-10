# Avalanche Snowflake Data Structures

This document describes the schema for Avalanche blockchain data tables in Snowflake.

---

## C_BLOCKS
**Description:** Avalanche C-Chain Blocks: Each record represents a block on the Avalanche C-Chain

| # | Column | Type | Nullable | Description |
|---|--------|------|----------|-------------|
| 1 | BLOCKTIMESTAMP | NUMBER(38,0) | Y | Block timestamp |
| 2 | BLOCKHASH | VARCHAR | Y | Unique identifier of C-chain blocks, the hash of the block header |
| 3 | BLOCKNUMBER | NUMBER(38,0) | Y | Block number (also known as block height). Represents the length of the blockchain |
| 4 | BLOCKBASEFEEPERGAS | NUMBER(38,0) | Y | Base fee per gas unit included in the block |
| 5 | BLOCKGASLIMIT | NUMBER(38,0) | Y | Maximum gas allowed in this block |
| 6 | BLOCKGASUSED | NUMBER(38,0) | Y | Total gas used in the block |
| 7 | BLOCKPARENTHASH | VARCHAR | Y | Hash of the parent block (previous block in the chain) |
| 8 | BLOCKRECEIPTHASH | VARCHAR | Y | Hash representing the receipt of the block's transactions |
| 9 | BLOCKRECEIPTSROOT | VARCHAR | Y | Root hash of the receipts trie for the block |
| 10 | BLOCKSIZE | NUMBER(38,0) | Y | Size of the block |
| 11 | BLOCKSTATEROOT | VARCHAR | Y | State root hash after block execution |
| 12 | BLOCKTRANSACTIONLEN | NUMBER(38,0) | Y | Number of transactions included in the block |
| 13 | BLOCKEXTRADATA | VARCHAR | Y | Extra data field in the block header |
| 14 | BLOCKEXTDATAGASUSED | NUMBER(38,0) | Y | Gas used by atomic (cross-chain) transactions included via ExtData |
| 15 | BLOCKGASCOST | NUMBER(38,0) | Y | Additional gas cost metric unique to Avalanche block production |
| 16 | PARTITION_DATE | DATE | Y | Derived from the block timestamp |

---

## C_TRANSACTIONS
**Description:** Avalanche C-Chain Transactions: Each record represents a transaction on the C-Chain, capturing sender/recipient addresses, value transferred, gas details, and transaction metadata

| # | Column | Type | Nullable | Description |
|---|--------|------|----------|-------------|
| 1 | BLOCKHASH | VARCHAR | Y | Blockhash of the block containing this transaction |
| 2 | BLOCKNUMBER | NUMBER(38,0) | Y | Block number |
| 3 | BLOCKTIMESTAMP | NUMBER(38,0) | Y | Block timestamp |
| 4 | TRANSACTIONHASH | VARCHAR | Y | Unique hash of the transaction |
| 5 | TRANSACTIONFROM | VARCHAR | Y | Sender address initiating the transaction |
| 6 | TRANSACTIONTO | VARCHAR | Y | Recipient address of the transaction; this can be a contract address |
| 7 | TRANSACTIONGAS | NUMBER(38,0) | Y | Gas used by the transaction |
| 8 | TRANSACTIONGASPRICE | NUMBER(38,0) | Y | Cost per unit of gas in nAVAX |
| 9 | TRANSACTIONMAXFEEPERGAS | NUMBER(38,0) | Y | Maximum fee per gas unit specified |
| 10 | TRANSACTIONMAXPRIORITYFEEPERGAS | NUMBER(38,0) | Y | Maximum priority fee per gas unit specified |
| 11 | TRANSACTIONINPUT | VARCHAR | Y | Transaction input data, commonly used for contract calls |
| 12 | TRANSACTIONNONCE | NUMBER(38,0) | Y | Nonce value indicating the sender's transaction count |
| 13 | TRANSACTIONINDEX | NUMBER(38,0) | Y | Position of the transaction within the block (0-based) |
| 14 | TRANSACTIONCOST | NUMBER(38,0) | Y | Total cost of the transaction (gas used × gas price) |
| 15 | TRANSACTIONVALUE | NUMBER(38,0) | Y | Amount of AVAX transferred in the transaction |
| 16 | TRANSACTIONTYPE | VARCHAR | Y | Type of transaction |
| 17 | PARTITION_DATE | DATE | Y | Derived from the block timestamp |

---

## C_RECEIPTS
**Description:** Avalanche C-Chain Transaction Receipts

| # | Column | Type | Nullable | Description |
|---|--------|------|----------|-------------|
| 1 | BLOCKHASH | VARCHAR | Y | Block hash of the block containing the transaction |
| 2 | BLOCKNUMBER | NUMBER(38,0) | Y | Block number |
| 3 | BLOCKTIMESTAMP | NUMBER(38,0) | Y | Block timestamp |
| 4 | TRANSACTIONHASH | VARCHAR | Y | Hash of the transaction corresponding to this receipt |
| 5 | TRANSACTIONRECEIPTGASUSED | NUMBER(38,0) | Y | Gas used by the transaction during execution |
| 6 | TRANSACTIONRECEIPTCUMULATIVEGASUSED | NUMBER(38,0) | Y | Cumulative gas used in the block up to and including this transaction |
| 7 | TRANSACTIONRECEIPTSTATUS | NUMBER(38,0) | Y | Status code of the transaction (1 for success, 0 for failure) |
| 8 | TRANSACTIONRECEIPTCONTRACTADDRESS | VARCHAR | Y | Address of the contract created (if the transaction was a contract creation) |
| 9 | TRANSACTIONRECEIPTPOSTSTATE | VARCHAR | Y | Post-execution state (hash) after the transaction was applied |
| 10 | TRANSACTIONRECEIPTEFFECTIVEGASPRICE | NUMBER(38,0) | Y | Effective gas price used for the transaction |
| 11 | PARTITION_DATE | DATE | Y | Derived from the block timestamp |

---

## C_LOGS
**Description:** Avalanche C-Chain Logs: Each record represents a log (event) emitted by a smart contract during transaction execution, with multiple topics for event filtering

| # | Column | Type | Nullable | Description |
|---|--------|------|----------|-------------|
| 1 | BLOCKHASH | VARCHAR | Y | Blockhash of the block containing this transaction |
| 2 | BLOCKNUMBER | NUMBER(38,0) | Y | Block number |
| 3 | BLOCKTIMESTAMP | NUMBER(38,0) | Y | Block timestamp |
| 4 | TRANSACTIONHASH | VARCHAR | Y | Hash of the transaction that generated this log event |
| 5 | LOGADDRESS | VARCHAR | Y | Contract address that emitted the log event |
| 6 | TOPICHEX_0 | VARCHAR | Y | First topic in hexadecimal (commonly the event signature hash) |
| 7 | TOPICHEX_1 | VARCHAR | Y | Second topic in hexadecimal (if available) |
| 8 | TOPICHEX_2 | VARCHAR | Y | Third topic in hexadecimal (if available) |
| 9 | TOPICHEX_3 | VARCHAR | Y | Fourth topic in hexadecimal (if available) |
| 10 | TOPICDEC_0 | VARCHAR | Y | First topic decoded into a human-readable format (if available) |
| 11 | TOPICDEC_1 | VARCHAR | Y | Second topic decoded into a human-readable format (if available) |
| 12 | TOPICDEC_2 | VARCHAR | Y | Third topic decoded into a human-readable format (if available) |
| 13 | TOPICDEC_3 | VARCHAR | Y | Fourth topic decoded into a human-readable format (if available) |
| 14 | LOGDATA | VARCHAR | Y | Hex-encoded non-indexed data of the log event |
| 15 | LOGINDEX | NUMBER(38,0) | Y | Index of this log within the transaction (0-based) |
| 16 | TRANSACTIONINDEX | NUMBER(38,0) | Y | Index of the transaction within the block |
| 17 | REMOVED | BOOLEAN | Y | Flag indicating whether the log was removed due to a chain reorganization |
| 18 | PARTITION_DATE | DATE | Y | Derived from the block timestamp |

---

## C_INTERNAL_TRANSACTIONS
**Description:** Avalanche C-Chain Internal Transactions

| # | Column | Type | Nullable | Description |
|---|--------|------|----------|-------------|
| 1 | BLOCKHASH | VARCHAR | Y | Hash of the block containing this internal transaction |
| 2 | BLOCKNUMBER | NUMBER(38,0) | Y | Block number |
| 3 | BLOCKTIMESTAMP | NUMBER(38,0) | Y | Block timestamp |
| 4 | TRANSACTIONHASH | VARCHAR | Y | Hash of the originating external transaction that triggered this internal call |
| 5 | TYPE | VARCHAR | Y | Type of internal transaction (e.g., call, create) |
| 6 | `FROM` | VARCHAR | Y | Address initiating the internal transaction |
| 7 | `TO` | VARCHAR | Y | Recipient address of the internal transaction |
| 8 | VALUE | NUMBER(38,0) | Y | Amount of AVAX transferred in the internal transaction |
| 9 | GAS | NUMBER(38,0) | Y | Gas provided for the internal transaction execution |
| 10 | GASUSED | NUMBER(38,0) | Y | Gas actually used by the internal transaction |
| 11 | REVERT | VARCHAR | Y | Indicator if the internal transaction was reverted |
| 12 | ERROR | VARCHAR | Y | Error message or code if an error occurred during execution |
| 13 | REVERTREASON | VARCHAR | Y | Detailed reason for a transaction revert, if applicable |
| 14 | INPUT | VARCHAR | Y | Hex-encoded input data provided to the internal call |
| 15 | OUTPUT | VARCHAR | Y | Hex-encoded output data returned from the internal call |
| 16 | CALLINDEX | VARCHAR | Y | Index representing the order of nested calls within the transaction |
| 17 | TRACE_POSITION | VARCHAR | Y | Position indicator for the call within the transaction trace |
| 18 | PARTITION_DATE | DATE | Y | Derived from the block timestamp |

---

## C_MESSAGES
**Description:** Avalanche C-Chain Messages

| # | Column | Type | Nullable | Description |
|---|--------|------|----------|-------------|
| 1 | BLOCKHASH | VARCHAR | Y | Hash of the block containing this message |
| 2 | BLOCKNUMBER | NUMBER(38,0) | Y | Block number |
| 3 | BLOCKTIMESTAMP | NUMBER(38,0) | Y | Block timestamp |
| 4 | TRANSACTIONHASH | VARCHAR | Y | Hash of the transaction that included this message |
| 5 | TRANSACTIONMESSAGEFROM | VARCHAR | Y | Sender address for the message |
| 6 | TRANSACTIONMESSAGETO | VARCHAR | Y | Recipient address for the message |
| 7 | TRANSACTIONMESSAGEGASPRICE | NUMBER(38,0) | Y | Gas price applied to the message transaction |
| 8 | PARTITION_DATE | DATE | Y | Derived from the block timestamp |

