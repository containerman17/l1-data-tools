# EVM Ingestion Client

Go client library for streaming blocks from ingestion/evm/rpc server.

## Usage

```go
import "github.com/containerman17/l1-data-tools/ingestion/evm/client"

c := client.NewClient("localhost:9090")
err := c.Stream(ctx, 1, func(blocks []client.Block) error {
    for _, b := range blocks {
        fmt.Printf("Block %d: %d txs\n", b.Number, len(b.Data.Block.Transactions))
    }
    return nil
})
```

## Example

```bash
go build -o example-client ./ingestion/evm/client/example
./example-client -addr localhost:9090 -from 1
```


