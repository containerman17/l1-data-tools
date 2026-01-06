#!/usr/bin/env bash
set -exu

cd $(dirname $0)

if [ -f ../.env ]; then
  source ../.env
fi

CANONICAL_VM_ID=srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy

BLOCKCHAIN_ID="2pGcTLh9Z5YCjmU871k2Z5waC8cCXYJnafYz1h7RJjz7u9Nmg2"
VM_ID="Vpkm9F6REMHamNkuDJPGMc6BUivQGdzWQJ2nVyL1iappBxXyy"
SUBNET_ID="VgJ6r7jQzMeXLTDaRNzBiMesZmTpioT8fntEZMvYkCW1bJyAj"


# Clean chain data for fresh bootstrap
rm -rf /home/ubuntu/.avalanchego/chainData/$BLOCKCHAIN_ID

# Ensure plugins directory exists and is writable
sudo mkdir -p $HOME/.avalanchego/plugins
sudo chown -R $USER:$USER $HOME/.avalanchego
go build -o $HOME/.avalanchego/plugins/$CANONICAL_VM_ID ../cmd/subnet-evm-plugin

cp ~/.avalanchego/plugins/$CANONICAL_VM_ID ~/.avalanchego/plugins/$VM_ID

# Configure chain (subnet-evm config)
mkdir -p ~/.avalanchego/configs/chains/$BLOCKCHAIN_ID
cat > ~/.avalanchego/configs/chains/$BLOCKCHAIN_ID/config.json << EOF
{
  "pruning-enabled": true,
  "state-sync-enabled": false,
  "eth-apis": [
    "eth",
    "eth-filter",
    "net",
    "admin",
    "web3",
    "internal-eth",
    "internal-blockchain",
    "internal-transaction",
    "internal-debug",
    "internal-account",
    "internal-personal",
    "debug",
    "debug-tracer",
    "debug-file-tracer",
    "debug-handler"
  ],
  "allow-unfinalized-queries": true,
  "state-history": 128
}
EOF

# Download and extract avalanchego if not present
AVALANCHEGO_TAR="/tmp/avalanchego-linux-amd64-v1.14.0.tar.gz"
AVALANCHEGO_DIR="/tmp/avalanchego-v1.14.0"
AVALANCHEGO_BIN="$AVALANCHEGO_DIR/avalanchego"

if [ ! -f "$AVALANCHEGO_BIN" ]; then
  if [ ! -f "$AVALANCHEGO_TAR" ]; then
    echo "Downloading avalanchego v1.14.0..."
    curl -L -o "$AVALANCHEGO_TAR" https://github.com/ava-labs/avalanchego/releases/download/v1.14.0/avalanchego-linux-amd64-v1.14.0.tar.gz
  fi
  echo "Extracting avalanchego..."
  tar -xzf "$AVALANCHEGO_TAR" -C /tmp/
fi

"$AVALANCHEGO_BIN" --partial-sync-primary-network --network-id=fuji --track-subnets=$SUBNET_ID
