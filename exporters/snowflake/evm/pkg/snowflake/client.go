package snowflake

import (
	"context"
	"crypto/rsa"
	"database/sql"
	"encoding/base64"
	"fmt"

	"crypto/x509"
	"encoding/pem"

	sf "github.com/snowflakedb/gosnowflake"
)

// Config holds Snowflake connection configuration.
type Config struct {
	Account          string
	User             string
	PrivateKeyBase64 string // Base64-encoded RSA private key (PEM format)
	Database         string
	Schema           string
	Warehouse        string
	Role             string
	TablePrefix      string
}

// Client wraps a Snowflake database connection.
type Client struct {
	db     *sql.DB
	prefix string
}

// parsePrivateKey decodes a base64-encoded PEM private key and returns the RSA key.
func parsePrivateKey(base64Key string) (*rsa.PrivateKey, error) {
	// Decode base64
	pemBytes, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}

	// Parse PEM block
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block")
	}

	// Parse PKCS8 private key
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKCS8 key: %w", err)
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not RSA")
	}

	return rsaKey, nil
}

// New creates a new Snowflake client and establishes a connection using Key Pair Authentication.
func New(cfg Config) (*Client, error) {
	privateKey, err := parsePrivateKey(cfg.PrivateKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	dsn, err := sf.DSN(&sf.Config{
		Account:       cfg.Account,
		User:          cfg.User,
		Authenticator: sf.AuthTypeJwt,
		PrivateKey:    privateKey,
		Database:      cfg.Database,
		Schema:        cfg.Schema,
		Warehouse:     cfg.Warehouse,
		Role:          cfg.Role,
	})
	if err != nil {
		return nil, fmt.Errorf("build DSN: %w", err)
	}

	db, err := sql.Open("snowflake", dsn)
	if err != nil {
		return nil, fmt.Errorf("open connection: %w", err)
	}

	// Verify connection
	if err := db.PingContext(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	return &Client{
		db:     db,
		prefix: cfg.TablePrefix,
	}, nil
}

// GetLastBlock returns the highest block number in the blocks table.
// Returns -1 if the table is empty.
func (c *Client) GetLastBlock(ctx context.Context) (int64, error) {
	query := fmt.Sprintf("SELECT COALESCE(MAX(BLOCKNUMBER), -1) FROM %sBLOCKS", c.prefix)

	var lastBlock int64
	if err := c.db.QueryRowContext(ctx, query).Scan(&lastBlock); err != nil {
		return 0, fmt.Errorf("query last block: %w", err)
	}

	return lastBlock, nil
}

// DB returns the underlying database connection for transactions.
func (c *Client) DB() *sql.DB {
	return c.db
}

// Prefix returns the table prefix.
func (c *Client) Prefix() string {
	return c.prefix
}

// Close closes the database connection.
func (c *Client) Close() error {
	return c.db.Close()
}
