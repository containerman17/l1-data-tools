package main

// RPC-compatible marshaling - copied from subnet-evm/internal/ethapi
// to ensure format parity with external RPC

import (
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/plugin/evm/customtypes"
)

// RPCTransaction represents a transaction in RPC format
type RPCTransaction struct {
	BlockHash           *common.Hash      `json:"blockHash"`
	BlockNumber         *hexutil.Big      `json:"blockNumber"`
	From                common.Address    `json:"from"`
	Gas                 hexutil.Uint64    `json:"gas"`
	GasPrice            *hexutil.Big      `json:"gasPrice"`
	GasFeeCap           *hexutil.Big      `json:"maxFeePerGas,omitempty"`
	GasTipCap           *hexutil.Big      `json:"maxPriorityFeePerGas,omitempty"`
	Hash                common.Hash       `json:"hash"`
	Input               hexutil.Bytes     `json:"input"`
	Nonce               hexutil.Uint64    `json:"nonce"`
	To                  *common.Address   `json:"to"`
	TransactionIndex    *hexutil.Uint64   `json:"transactionIndex"`
	Value               *hexutil.Big      `json:"value"`
	Type                hexutil.Uint64    `json:"type"`
	Accesses            *types.AccessList `json:"accessList,omitempty"`
	ChainID             *hexutil.Big      `json:"chainId,omitempty"`
	V                   *hexutil.Big      `json:"v"`
	R                   *hexutil.Big      `json:"r"`
	S                   *hexutil.Big      `json:"s"`
	YParity             *hexutil.Uint64   `json:"yParity,omitempty"`
	BlobVersionedHashes []common.Hash     `json:"blobVersionedHashes,omitempty"`
}

// RPCMarshalBlock converts a types.Block to RPC format
func RPCMarshalBlock(block *types.Block, inclTx bool, fullTx bool, config *params.ChainConfig) map[string]interface{} {
	fields := RPCMarshalHeader(block.Header())
	fields["size"] = hexutil.Uint64(block.Size())

	if inclTx {
		if fullTx {
			txs := block.Transactions()
			transactions := make([]interface{}, len(txs))
			for i, tx := range txs {
				transactions[i] = newRPCTransactionFromBlockIndex(block, uint64(i), config)
				_ = tx // silence unused
			}
			fields["transactions"] = transactions
		} else {
			fields["transactions"] = block.Transactions()
		}
	}

	uncles := block.Uncles()
	uncleHashes := make([]common.Hash, len(uncles))
	for i, uncle := range uncles {
		uncleHashes[i] = uncle.Hash()
	}
	fields["uncles"] = uncleHashes

	return fields
}

// RPCMarshalHeader converts a types.Header to RPC format
func RPCMarshalHeader(head *types.Header) map[string]interface{} {
	headExtra := customtypes.GetHeaderExtra(head)
	result := map[string]interface{}{
		"number":           (*hexutil.Big)(head.Number),
		"hash":             head.Hash(),
		"parentHash":       head.ParentHash,
		"nonce":            head.Nonce,
		"mixHash":          head.MixDigest,
		"sha3Uncles":       head.UncleHash,
		"logsBloom":        head.Bloom,
		"stateRoot":        head.Root,
		"miner":            head.Coinbase,
		"difficulty":       (*hexutil.Big)(head.Difficulty),
		"extraData":        hexutil.Bytes(head.Extra),
		"gasLimit":         hexutil.Uint64(head.GasLimit),
		"gasUsed":          hexutil.Uint64(head.GasUsed),
		"timestamp":        hexutil.Uint64(head.Time),
		"transactionsRoot": head.TxHash,
		"receiptsRoot":     head.ReceiptHash,
	}

	if head.BaseFee != nil {
		result["baseFeePerGas"] = (*hexutil.Big)(head.BaseFee)
	}

	// Subnet-EVM specific
	if headExtra.BlockGasCost != nil {
		result["blockGasCost"] = (*hexutil.Big)(headExtra.BlockGasCost)
	}

	if head.BlobGasUsed != nil {
		result["blobGasUsed"] = hexutil.Uint64(*head.BlobGasUsed)
	}

	if head.ExcessBlobGas != nil {
		result["excessBlobGas"] = hexutil.Uint64(*head.ExcessBlobGas)
	}

	if head.WithdrawalsHash != nil {
		result["withdrawalsRoot"] = head.WithdrawalsHash
	}

	if head.ParentBeaconRoot != nil {
		result["parentBeaconBlockRoot"] = head.ParentBeaconRoot
	}

	return result
}

// newRPCTransactionFromBlockIndex creates an RPC transaction from a block and index
func newRPCTransactionFromBlockIndex(b *types.Block, index uint64, config *params.ChainConfig) *RPCTransaction {
	txs := b.Transactions()
	if index >= uint64(len(txs)) {
		return nil
	}
	return newRPCTransaction(txs[index], b.Hash(), b.NumberU64(), b.Time(), index, b.BaseFee(), config)
}

// newRPCTransaction creates an RPC transaction from a types.Transaction
func newRPCTransaction(tx *types.Transaction, blockHash common.Hash, blockNumber uint64, blockTime uint64, index uint64, baseFee *big.Int, config *params.ChainConfig) *RPCTransaction {
	signer := types.MakeSigner(config, new(big.Int).SetUint64(blockNumber), blockTime)
	from, _ := types.Sender(signer, tx)
	v, r, s := tx.RawSignatureValues()
	result := &RPCTransaction{
		Type:     hexutil.Uint64(tx.Type()),
		From:     from,
		Gas:      hexutil.Uint64(tx.Gas()),
		GasPrice: (*hexutil.Big)(tx.GasPrice()),
		Hash:     tx.Hash(),
		Input:    tx.Data(),
		Nonce:    hexutil.Uint64(tx.Nonce()),
		To:       tx.To(),
		Value:    (*hexutil.Big)(tx.Value()),
		V:        (*hexutil.Big)(v),
		R:        (*hexutil.Big)(r),
		S:        (*hexutil.Big)(s),
	}

	if blockHash != (common.Hash{}) {
		result.BlockHash = &blockHash
		result.BlockNumber = (*hexutil.Big)(new(big.Int).SetUint64(blockNumber))
		idx := hexutil.Uint64(index)
		result.TransactionIndex = &idx
	}

	switch tx.Type() {
	case types.LegacyTxType:
		// nothing special
	case types.AccessListTxType:
		al := tx.AccessList()
		yparity := hexutil.Uint64(v.Sign())
		result.Accesses = &al
		result.ChainID = (*hexutil.Big)(tx.ChainId())
		result.YParity = &yparity
	case types.DynamicFeeTxType:
		al := tx.AccessList()
		yparity := hexutil.Uint64(v.Sign())
		result.Accesses = &al
		result.ChainID = (*hexutil.Big)(tx.ChainId())
		result.YParity = &yparity
		result.GasFeeCap = (*hexutil.Big)(tx.GasFeeCap())
		result.GasTipCap = (*hexutil.Big)(tx.GasTipCap())
		result.GasPrice = (*hexutil.Big)(effectiveGasPrice(tx, baseFee))
	case types.BlobTxType:
		al := tx.AccessList()
		yparity := hexutil.Uint64(v.Sign())
		result.Accesses = &al
		result.ChainID = (*hexutil.Big)(tx.ChainId())
		result.YParity = &yparity
		result.GasFeeCap = (*hexutil.Big)(tx.GasFeeCap())
		result.GasTipCap = (*hexutil.Big)(tx.GasTipCap())
		result.GasPrice = (*hexutil.Big)(effectiveGasPrice(tx, baseFee))
		result.BlobVersionedHashes = tx.BlobHashes()
	}

	return result
}

func effectiveGasPrice(tx *types.Transaction, baseFee *big.Int) *big.Int {
	if baseFee == nil {
		return tx.GasPrice()
	}
	tip := tx.GasTipCap()
	fee := tx.GasFeeCap()
	effectiveTip := new(big.Int).Sub(fee, baseFee)
	if effectiveTip.Cmp(tip) > 0 {
		effectiveTip = tip
	}
	return new(big.Int).Add(baseFee, effectiveTip)
}

// marshalReceipt converts a types.Receipt to RPC format
func marshalReceipt(receipt *types.Receipt, blockHash common.Hash, blockNumber uint64, index uint64, tx *types.Transaction, config *params.ChainConfig) map[string]interface{} {
	fields := map[string]interface{}{
		"blockHash":         blockHash,
		"blockNumber":       hexutil.Uint64(blockNumber),
		"transactionHash":   tx.Hash(),
		"transactionIndex":  hexutil.Uint64(index),
		"from":              getFrom(tx, blockNumber, config),
		"to":                tx.To(),
		"gasUsed":           hexutil.Uint64(receipt.GasUsed),
		"cumulativeGasUsed": hexutil.Uint64(receipt.CumulativeGasUsed),
		"contractAddress":   nil,
		"logs":              receipt.Logs,
		"logsBloom":         receipt.Bloom,
		"type":              hexutil.Uint64(receipt.Type),
		"status":            hexutil.Uint64(receipt.Status),
	}

	if receipt.ContractAddress != (common.Address{}) {
		fields["contractAddress"] = receipt.ContractAddress
	}

	if receipt.EffectiveGasPrice != nil {
		fields["effectiveGasPrice"] = hexutil.Uint64(receipt.EffectiveGasPrice.Uint64())
	}

	return fields
}

func getFrom(tx *types.Transaction, blockNumber uint64, config *params.ChainConfig) common.Address {
	signer := types.MakeSigner(config, new(big.Int).SetUint64(blockNumber), 0)
	from, _ := types.Sender(signer, tx)
	return from
}
