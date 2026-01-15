# Snowflake EVM Exporter

Long-running daemon that exports EVM blocks from ingestion service to Snowflake in 1,000-block batches. Uses smart rate limiting to catch up quickly when behind while avoiding trickle inserts when at chain tip.

## Behavior

- **Catching up**: When behind by 1000+ blocks, runs batches continuously until caught up
- **Steady state**: When at chain tip (partial batch), waits 1 hour before next batch
- **Error recovery**: On any error, logs and retries after 5-minute backoff

## Authentication

This exporter uses **Key Pair Authentication** (RSA), the industry standard for Snowflake service accounts.

### Step 1: Generate RSA Key Pair

```bash
cd exporters/snowflake/evm

# Generate private key as base64 and add to .env (one line, no files)
echo -e "\nSNOWFLAKE_PRIVATE_KEY=$(openssl genrsa 2048 2>/dev/null | openssl pkcs8 -topk8 -nocrypt | base64 -w0)" >> .env

# Extract public key (copy the MII... part, without BEGIN/END lines)
source .env && echo "$SNOWFLAKE_PRIVATE_KEY" | base64 -d | openssl rsa -pubout 2>/dev/null
```

### Step 2: Find Your Account Identifier

Your account identifier is in the Snowflake URL. Look for:
- `https://app.snowflake.com/us-east-1/abc12345/...` → Account is `abc12345.us-east-1`
- Or use org-account format: `ORGNAME-ACCOUNTNAME` (visible in bottom-left menu)

### Step 3: Create User and Register Key (Snowflake Admin)

Run these in Snowflake with admin privileges:

```sql
-- Create the service user
CREATE USER IF NOT EXISTS importer;

-- Register the public key (paste MII... part, no headers/newlines)
ALTER USER importer SET RSA_PUBLIC_KEY='MIIBIjANBgkq...your-key-here...';

-- Grant permissions
GRANT USAGE ON DATABASE your_database TO USER importer;
GRANT USAGE ON SCHEMA your_database.PUBLIC TO USER importer;
GRANT SELECT, INSERT ON ALL TABLES IN SCHEMA your_database.PUBLIC TO USER importer;
GRANT USAGE ON WAREHOUSE your_warehouse TO USER importer;
```

### Step 4: Configure .env

```bash
SNOWFLAKE_ACCOUNT=abc12345.us-east-1    # From Step 2
SNOWFLAKE_USER=importer
SNOWFLAKE_PRIVATE_KEY=LS0tLS1CRUdJ...   # Generated in Step 1
SNOWFLAKE_DATABASE=L1_DEV
SNOWFLAKE_SCHEMA=PUBLIC
SNOWFLAKE_WAREHOUSE=COMPUTE_WH
SNOWFLAKE_TABLE_PREFIX=C_
INGESTION_URL=http://your-indexer:9090/ws
```

## Testing Credentials

The `testcreds` tool auto-loads `.env` - no need to source it:

```bash
cd exporters/snowflake/evm
go run ./cmd/testcreds/
```

**Expected output:**
```
🔐 Testing Snowflake credentials...
✅ Connected successfully
✅ Query successful
📊 Last block in C_BLOCKS: -1

✅ All checks passed! Credentials are valid.
```

**Common errors:**
| Error | Solution |
|-------|----------|
| HTTP 404 | Account identifier is wrong (check Step 2) |
| JWT token is invalid | Public key not registered (check Step 3) |
| Database does not exist | Missing GRANT permissions (check Step 3) |

## Usage

Once `.env` is configured (see Step 4 above), run the exporter:

```bash
cd exporters/snowflake/evm
go run ./cmd/exporter/
```

Or build and run:

```bash
go build -o /tmp/snowflake-evm-exporter ./cmd/exporter/
/tmp/snowflake-evm-exporter
```

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `SNOWFLAKE_ACCOUNT` | Yes | - | Snowflake account identifier |
| `SNOWFLAKE_USER` | Yes | - | Snowflake service account username |
| `SNOWFLAKE_PRIVATE_KEY` | Yes | - | Base64-encoded RSA private key (PEM format) |
| `SNOWFLAKE_DATABASE` | Yes | - | Target database |
| `SNOWFLAKE_SCHEMA` | Yes | - | Target schema |
| `SNOWFLAKE_WAREHOUSE` | Yes | - | Compute warehouse |
| `SNOWFLAKE_ROLE` | No | - | Role to use (uses default if not set) |
| `SNOWFLAKE_TABLE_PREFIX` | Yes | - | Table prefix (e.g., "cchain_", "dfk_") |
| `INGESTION_URL` | Yes | - | Address of ingestion service (e.g., "localhost:9090") |
| `BATCH_SIZE` | No | 1000 | Blocks per transaction |
| `PARTIAL_BATCH_WAIT` | No | 1h | Wait time after partial batch (at chain tip) |
| `ERROR_BACKOFF` | No | 5m | Wait time after error before retry |

## Tables

The exporter writes to 6 tables (must exist before running):

`${prefix}BLOCKS`, `${prefix}TRANSACTIONS`, `${prefix}RECEIPTS`, `${prefix}LOGS`, `${prefix}INTERNAL_TRANSACTIONS`, `${prefix}MESSAGES`

## Deployment Example (systemd)

```ini
[Unit]
Description=Snowflake EVM Exporter
After=network.target

[Service]
Type=simple
ExecStart=/path/to/snowflake-evm-exporter
EnvironmentFile=/etc/snowflake-exporter/env
Restart=on-failure
RestartSec=30

[Install]
WantedBy=multi-user.target
```