create or replace TABLE C_BLOCKS (
	BLOCKTIMESTAMP NUMBER(38,0) COMMENT 'Block timestamp',
	BLOCKHASH VARCHAR(16777216) COMMENT 'Unique identifier of C-chain blocks, the hash of the block header.',
	BLOCKNUMBER NUMBER(38,0) COMMENT 'Block number as a string, also known as block height. Represents the length of the blockchain',
	BLOCKBASEFEEPERGAS NUMBER(38,0) COMMENT 'Base fee per gas unit included in the block.',
	BLOCKGASLIMIT NUMBER(38,0) COMMENT 'Maximum gas allowed in this block.',
	BLOCKGASUSED NUMBER(38,0) COMMENT 'Total gas used in the block.',
	BLOCKPARENTHASH VARCHAR(16777216) COMMENT 'Hash of the parent block (previous block in the chain).',
	BLOCKRECEIPTHASH VARCHAR(16777216) COMMENT 'Hash representing the receipt of the block’s transactions.',
	BLOCKRECEIPTSROOT VARCHAR(16777216) COMMENT 'Root hash of the receipts trie for the block.',
	BLOCKSIZE NUMBER(38,0) COMMENT 'Size of the block',
	BLOCKSTATEROOT VARCHAR(16777216) COMMENT 'State root hash after block execution.',
	BLOCKTRANSACTIONLEN NUMBER(38,0) COMMENT 'Number of transactions included in the block.',
	BLOCKEXTRADATA VARCHAR(16777216) COMMENT 'Extra data field in the block header',
	BLOCKEXTDATAGASUSED NUMBER(38,0) COMMENT 'Gas used by atomic (cross-chain) transactions included via ExtData.',
	BLOCKGASCOST NUMBER(38,0) COMMENT 'Additional gas cost metric unique to Avalanche block production.',
	PARTITION_DATE DATE COMMENT 'This date is dervied from the block timestamp'
)COMMENT='Avalanche C-Chain Blocks: Each record represents a block on the Avalanche C-Chain'
;

create or replace TABLE C_INTERNAL_TRANSACTIONS (
	BLOCKHASH VARCHAR(16777216) COMMENT 'Hash of the block containing this internal transaction.',
	BLOCKNUMBER NUMBER(38,0),
	BLOCKTIMESTAMP NUMBER(38,0) COMMENT 'Block timestamp',
	TRANSACTIONHASH VARCHAR(16777216) COMMENT 'Hash of the originating external transaction that triggered this internal call.',
	TYPE VARCHAR(16777216) COMMENT 'Type of internal transaction (e.g., call, create).',
	"`FROM`" VARCHAR(16777216) COMMENT 'Address initiating the internal transaction.',
	"`TO`" VARCHAR(16777216) COMMENT 'Recipient address of the internal transaction.',
	VALUE NUMBER(38,0) COMMENT 'Amount of AVAX transferred in the internal transaction.',
	GAS NUMBER(38,0) COMMENT 'Gas provided for the internal transaction execution.',
	GASUSED NUMBER(38,0) COMMENT 'Gas actually used by the internal transaction.',
	REVERT VARCHAR(16777216) COMMENT 'Indicator if the internal transaction was reverted.',
	ERROR VARCHAR(16777216) COMMENT 'Error message or code if an error occurred during execution.',
	REVERTREASON VARCHAR(16777216) COMMENT 'Detailed reason for a transaction revert, if applicable.',
	INPUT VARCHAR(16777216) COMMENT 'Hex-encoded input data provided to the internal call.',
	OUTPUT VARCHAR(16777216) COMMENT 'Hex-encoded output data returned from the internal call.',
	CALLINDEX VARCHAR(16777216) COMMENT 'Index representing the order of nested calls within the transaction.',
	TRACE_POSITION VARCHAR(16777216) COMMENT 'Position indicator for the call within the transaction trace.',
	PARTITION_DATE DATE COMMENT 'This date is derived from the block timestamp'
)COMMENT='Avalanche C-Chain Internal Transactions'
;

create or replace TABLE C_LOGS (
	BLOCKHASH VARCHAR(16777216) COMMENT 'blockhash of the block containing this transaction.',
	BLOCKNUMBER NUMBER(38,0),
	BLOCKTIMESTAMP NUMBER(38,0) COMMENT 'Block timestamp',
	TRANSACTIONHASH VARCHAR(16777216) COMMENT 'Hash of the transaction that generated this log event.',
	LOGADDRESS VARCHAR(16777216) COMMENT 'Contract address that emitted the log event.',
	TOPICHEX_0 VARCHAR(16777216) COMMENT 'First topic in hexadecimal (commonly the event signature hash).',
	TOPICHEX_1 VARCHAR(16777216) COMMENT 'Second topic in hexadecimal (if available).',
	TOPICHEX_2 VARCHAR(16777216) COMMENT 'Third topic in hexadecimal (if available).',
	TOPICHEX_3 VARCHAR(16777216) COMMENT 'Fourth topic in hexadecimal (if available).',
	TOPICDEC_0 VARCHAR(16777216) COMMENT 'First topic decoded into a human-readable format (if available).',
	TOPICDEC_1 VARCHAR(16777216) COMMENT 'Second topic decoded into a human-readable format (if available).',
	TOPICDEC_2 VARCHAR(16777216) COMMENT 'Third topic decoded into a human-readable format (if available).',
	TOPICDEC_3 VARCHAR(16777216) COMMENT 'Fourth topic decoded into a human-readable format (if available).',
	LOGDATA VARCHAR(16777216) COMMENT 'Hex-encoded non-indexed data of the log event.',
	LOGINDEX NUMBER(38,0) COMMENT 'Index of this log within the transaction (0-based).',
	TRANSACTIONINDEX NUMBER(38,0) COMMENT 'Index of the transaction within the block.',
	REMOVED BOOLEAN COMMENT 'Flag indicating whether the log was removed due to a chain reorganization.',
	PARTITION_DATE DATE COMMENT 'This date is dervied from the block timestamp'
)COMMENT='Avalanche C-Chain Logs: Each record represents a log (event) emitted by a smart contract during transaction execution, with multiple topics for event filtering.'
;

create or replace TABLE C_MESSAGES (
	BLOCKHASH VARCHAR(16777216) COMMENT 'Hash of the block containing this message.',
	BLOCKNUMBER NUMBER(38,0) COMMENT 'Block number as a string',
	BLOCKTIMESTAMP NUMBER(38,0) COMMENT 'Block timestamp',
	TRANSACTIONHASH VARCHAR(16777216) COMMENT 'Hash of the transaction that included this message.',
	TRANSACTIONMESSAGEFROM VARCHAR(16777216) COMMENT 'Sender address for the message.',
	TRANSACTIONMESSAGETO VARCHAR(16777216) COMMENT 'Recipient address for the message.',
	TRANSACTIONMESSAGEGASPRICE NUMBER(38,0) COMMENT 'Gas price applied to the message transaction.',
	PARTITION_DATE DATE COMMENT 'This date is derived from the block timestamp'
)COMMENT='Avalanche C-Chain Messages.'
;

create or replace TABLE C_RECEIPTS (
	BLOCKHASH VARCHAR(16777216) COMMENT 'block hash of the block containing the transaction.',
	BLOCKNUMBER NUMBER(38,0),
	BLOCKTIMESTAMP NUMBER(38,0) COMMENT 'Block timestamp',
	TRANSACTIONHASH VARCHAR(16777216) COMMENT 'Hash of the transaction corresponding to this receipt.',
	TRANSACTIONRECEIPTGASUSED NUMBER(38,0) COMMENT 'Gas used by the transaction during execution.',
	TRANSACTIONRECEIPTCUMULATIVEGASUSED NUMBER(38,0) COMMENT 'Cumulative gas used in the block up to and including this transaction.',
	TRANSACTIONRECEIPTSTATUS NUMBER(38,0) COMMENT 'Status code of the transaction (1 for success, 0 for failure).',
	TRANSACTIONRECEIPTCONTRACTADDRESS VARCHAR(16777216) COMMENT 'Address of the contract created (if the transaction was a contract creation).',
	TRANSACTIONRECEIPTPOSTSTATE VARCHAR(16777216) COMMENT 'Post-execution state (hash) after the transaction was applied.',
	TRANSACTIONRECEIPTEFFECTIVEGASPRICE NUMBER(38,0) COMMENT 'Effective gas price used for the transaction.',
	PARTITION_DATE DATE COMMENT 'This date is derived from the block timestamp'
)COMMENT='Avalanche C-Chain Transaction Receipts'
;

create or replace TABLE C_TRANSACTIONS (
	BLOCKHASH VARCHAR(16777216) COMMENT 'blockhash of the block containing this transaction.',
	BLOCKNUMBER NUMBER(38,0),
	BLOCKTIMESTAMP NUMBER(38,0) COMMENT 'Block timestamp',
	TRANSACTIONHASH VARCHAR(16777216) COMMENT 'Unique hash of the transaction.',
	TRANSACTIONFROM VARCHAR(16777216) COMMENT 'Sender address initiating the transaction.',
	TRANSACTIONTO VARCHAR(16777216) COMMENT 'Recipient address of the transaction; this can be a contract address',
	TRANSACTIONGAS NUMBER(38,0) COMMENT 'Gas used by the transaction',
	TRANSACTIONGASPRICE NUMBER(38,0) COMMENT 'Cost per unit of gas in nAVAX',
	TRANSACTIONMAXFEEPERGAS NUMBER(38,0) COMMENT 'Maximum fee per gas unit specified.',
	TRANSACTIONMAXPRIORITYFEEPERGAS NUMBER(38,0) COMMENT 'Maximum priority fee per gas unit specified.',
	TRANSACTIONINPUT VARCHAR(16777216) COMMENT 'Transaction input data, commonly used for contract calls',
	TRANSACTIONNONCE NUMBER(38,0) COMMENT 'Nonce value indicating the sender’s transaction count.',
	TRANSACTIONINDEX NUMBER(38,0) COMMENT 'Position of the transaction within the block (0-based).',
	TRANSACTIONCOST NUMBER(38,0) COMMENT 'Total cost of the transaction (gas used multiplied by gas price).',
	TRANSACTIONVALUE NUMBER(38,0) COMMENT 'Amount of AVAX transferred in the transaction.',
	TRANSACTIONTYPE VARCHAR(16777216) COMMENT 'Type of transaction',
	PARTITION_DATE DATE COMMENT 'This date is dervied from the block timestamp'
)COMMENT='Avalanche C-Chain Transactions: Each record represents a transaction on the C-Chain, capturing sender/recipient addresses, value transferred, gas details, and transaction metadata.'
;