// Indexing VM wrapper - wraps subnet-evm, indexes blocks with traces/receipts
// Based on avalanchego v1.14.0 / subnet-evm v0.8.0
package main

import (
	"context"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm"
	"github.com/ava-labs/subnet-evm/plugin/evm"
)

func main() {
	evm.RegisterAllLibEVMExtras()
	rpcchainvm.Serve(context.Background(), NewIndexingVM())
}
