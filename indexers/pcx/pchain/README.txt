P-Chain RPC Client - Two-Tier Architecture
==========================================

This package provides two client types for P-Chain RPC calls:

1. Client (client.go)
---------------------
Standard RPC client for dynamic data that can change over time.

Methods:
  - GetHeight()              Current chain height
  - GetBlockByHeight()       Raw block bytes
  - GetUTXOs()               Current UTXOs for addresses
  - GetCurrentValidators()   Active validators
  - GetPendingValidators()   Pending validators
  - GetStake()               Staked amounts
  - GetTx()                  Transaction bytes
  - GetAtomicUTXOs()         Cross-chain UTXOs

Features:
  - Connection pooling (1000 max connections)
  - Singleflight deduplication for concurrent identical requests


2. CachedClient (cached_client.go)
----------------------------------
Wraps Client and adds persistent caching for IMMUTABLE data.
Use this for RPC responses that never change once written.

Methods:
  - GetRewardUTXOs()    Reward UTXOs for completed staking tx (cached forever)
  - (all Client methods via embedding)

Cache storage:
  - Location: data/{networkID}/rpc_cache/
  - Format: PebbleDB
  - Key: "reward:{txID}" -> gob-encoded [][]byte

Why cache rewards?
  Once a staking period ends, the reward UTXOs are fixed forever.
  Caching avoids repeated RPC calls during re-indexing.


Usage
-----
  // Create base client
  client := pchain.NewClient(rpcURL)
  client.SetNetworkID(networkID)

  // Wrap with caching layer
  cached, _ := pchain.NewCachedClient(client, "/path/to/cache")
  defer cached.Close()

  // Dynamic calls (pass through to Client)
  height, _ := cached.GetHeight(ctx)

  // Cached calls (check cache first, fetch on miss)
  rewards, _ := cached.GetRewardUTXOs(ctx, stakingTxID)


Cache Management
----------------
The cache is self-healing: if deleted, it repopulates on demand.

To clear cache:
  rm -rf data/{networkID}/rpc_cache/

When to clear:
  - Cache format changed (code update)
  - Suspected corruption
  - Testing fresh indexing


Future Candidates for Caching
-----------------------------
Other immutable RPC responses that could be added to CachedClient:

  - GetTx(txID)         Transactions are immutable once confirmed
  - GetBlock(height)    Blocks are immutable (but we store raw bytes already)

