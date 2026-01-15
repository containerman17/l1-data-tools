# Develoment status

> All endpoints prefixed with `GET /v1/networks/`

## Legend

**Status Indicators:**
- ✅ **Completed** - Fully implemented and compatible with Glacier
- 🚧 **WIP** - Work in progress
- 📋 **Todo** - Not yet implemented

**Complexity Levels:**
- 🟢 **Low** - Easy to reimplement against the current Glacier API, usually just transforming JSON or just displaying data indexed somewhere else. Takes a couple of hours to implement with AI and test extensively.
- 🟡 **Medium** - Around a whole day task
- 🔴 **High** - Multi-day effort requiring significant implementation work (e.g., UTXOs).

## Primary Network (Top-level)

| Endpoint | Name | Status | Complexity | Comments |
|----------|------|--------|------------|----------|
| `{network}/blockchains/{blockchainId}/assets/{assetId}` | Get asset details | ✅ Completed | 🟢 Low | |
| `{network}/addresses:listChainIds` | Get chain interactions for addresses | ✅ Completed | 🟢 Low | |
| `{network}` | Get network details | ✅ Completed | 🟢 Low | |
| `{network}/blockchains` | List blockchains | ✅ Completed | 🟢 Low | |
| `{network}/blockchains/{blockchainId}` | Get blockchain details by ID | ✅ Completed | 🟢 Low | |
| `{network}/subnets` | List subnets | ✅ Completed | 🟢 Low | Glacier may return stale ownerAddresses after TransferSubnetOwnershipTx |
| `{network}/subnets/{subnetId}` | Get Subnet details by ID | ✅ Completed | 🟢 Low | |
| `{network}/validators` | List validators | 🚧 WIP | 🟡 Medium | Requires uptime recording, therefore medium |
| `{network}/validators/{nodeId}` | Get single validator details | 🚧 WIP | 🟡 Medium |  |
| `{network}/delegators` | List delegators | 📋 Todo | 🟡 Medium | Might be low, marking medium just to be safe |
| `{network}/l1Validators` | List L1 validators | 📋 Todo | 🟡 Medium | Might be low, marking medium just to be safe |

## Primary Network Blocks

| Endpoint | Name | Status | Complexity | Comments |
|----------|------|--------|------------|----------|
| `{network}/blockchains/{blockchainId}/blocks/{blockId}` | Get block | 📋 Todo | 🟢 Low | |
| `{network}/blockchains/{blockchainId}/nodes/{nodeId}/blocks` | List blocks proposed by node | 📋 Todo | 🟢 Low | |
| `{network}/blockchains/{blockchainId}/blocks` | List latest blocks | 📋 Todo | 🟢 Low | |

## Primary Network Vertices

Pre-cortina vertices. Will never be changed or added. Timestamps are backed up as per glacier.

| Endpoint | Name | Status | Complexity | Comments |
|----------|------|--------|------------|----------|
| `{network}/blockchains/{blockchainId}/vertices` | List latest vertices | 📋 Todo | 🟡 Medium | Would require re-ingestion |
| `{network}/blockchains/{blockchainId}/vertices/{vertexHash}` | Get vertex | 📋 Todo | 🟢 Low | |
| `{network}/blockchains/{blockchainId}/vertices:listByHeight` | List vertices by height | 📋 Todo | 🟢 Low | |

## Primary Network Transactions

| Endpoint | Name | Status | Complexity | Comments |
|----------|------|--------|------------|----------|
| `{network}/blockchains/{blockchainId}/transactions/{txHash}` | Get transaction | 📋 Todo | 🟢 Low | |
| `{network}/blockchains/{blockchainId}/transactions` | List latest transactions | 📋 Todo | 🟢 Low | |
| `{network}/blockchains/{blockchainId}/transactions:listStaking` | List staking transactions | 📋 Todo | 🟢 Low | |
| `{network}/blockchains/{blockchainId}/assets/{assetId}/transactions` | List asset transactions | 📋 Todo | 🟢 Low |  |

## Primary Network Balances

| Endpoint | Name | Status | Complexity | Comments |
|----------|------|--------|------------|----------|
| `{network}/blockchains/{blockchainId}/balances` | Get balances | ✅ Completed | 🟢 Low | Marking low as it is basically a summ over utxos |

## Primary Network UTXOs

| Endpoint | Name | Status | Complexity | Comments |
|----------|------|--------|------------|----------|
| `{network}/blockchains/{blockchainId}/utxos` | List UTXOs | ✅ Completed | 🔴 High | v1 & v2; includes staked UTXOs; suppresses metadata for staked=true |
| `{network}/blockchains/{blockchainId}/lastActivityTimestampByAddresses` | Get last activity timestamp by addresses | 📋 Todo | 🟢 Low |  |

## Primary Network Rewards

| Endpoint | Name | Status | Complexity | Comments |
|----------|------|--------|------------|----------|
| `{network}/rewards:listPending` | List pending rewards | ✅ Completed | 🟡 Medium | Cached proxy with automatic invalidation on relevant transactions |
| `{network}/rewards` | List historical rewards | ✅ Completed | 🟡 Medium | Principal remains unspent UTXO after reward |
