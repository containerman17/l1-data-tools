```
ubuntu@tokyo:~/devrel-experiments/03_data_api/p-chain-indexer$ go run ./cmd/test/ utxos --fresh
2025/12/19 05:49:10 Connected to fuji (network ID: 5)
2025/12/19 05:49:10 Dropping utxos data: data/5/utxos
2025/12/19 05:49:10 Loaded 10 test cases for utxos
2025/12/19 05:49:10 [p-fetcher] starting...
2025/12/19 05:49:10 [x-runner] starting pre-Cortina...
2025/12/19 05:49:10 [p-runner] starting...
2025/12/19 05:49:10 [c-runner] starting...
....
2025/12/19 05:52:47 [x-runner] pre-Cortina tx 460000/508608 | 2121 tx/s | remaining 48608 | read=6ms parse=16ms write=2849ms
2025/12/19 05:52:54 [x-runner] pre-Cortina tx 470000/508608 | 2105 tx/s | remaining 38608 | read=9ms parse=28ms write=6383ms
2025/12/19 05:52:57 [x-runner] pre-Cortina tx 480000/508608 | 2121 tx/s | remaining 28608 | read=10ms parse=17ms write=3102ms
2025/12/19 05:52:59 [x-runner] pre-Cortina tx 490000/508608 | 2141 tx/s | remaining 18608 | read=10ms parse=30ms write=2555ms
2025/12/19 05:53:02 [x-runner] pre-Cortina tx 500000/508608 | 2157 tx/s | remaining 8608 | read=8ms parse=19ms write=2881ms
2025/12/19 05:53:02 [c-runner] indexed 49255857 blocks in 231.9s
2025/12/19 05:53:05 [x-runner] pre-Cortina complete: 508608 transactions
2025/12/19 05:53:05 [x-runner] starting blocks...
2025/12/19 05:53:05 [c-runner] indexed 136 blocks in 2.5s
2025/12/19 05:53:08 [c-runner] indexed 1 blocks in 2.2s
2025/12/19 05:53:11 [c-runner] indexed 1 blocks in 1.1s
2025/12/19 05:53:14 [c-runner] indexed 1 blocks in 2.4s
2025/12/19 05:53:16 [x-runner] indexed 36140 blocks in 11.1s
```

After sync, tests do not start. That happened after adding x chain indxing. My guess would be some problem with Cortina/Pre-cortina. Wait group stucks somewhwre or something. 