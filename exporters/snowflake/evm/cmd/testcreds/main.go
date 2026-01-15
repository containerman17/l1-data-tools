package main

import (
	"context"
	"log"
	"os"

	"github.com/containerman17/l1-data-tools/exporters/snowflake/evm/pkg/snowflake"
	"github.com/joho/godotenv"
)

func main() {
	log.SetFlags(0)

	// Auto-load .env if present
	_ = godotenv.Load()

	cfg := snowflake.Config{
		Account:          getEnv("SNOWFLAKE_ACCOUNT"),
		User:             getEnv("SNOWFLAKE_USER"),
		PrivateKeyBase64: getEnv("SNOWFLAKE_PRIVATE_KEY"),
		Database:         getEnv("SNOWFLAKE_DATABASE"),
		Schema:           getEnv("SNOWFLAKE_SCHEMA"),
		Warehouse:        getEnv("SNOWFLAKE_WAREHOUSE"),
		Role:             os.Getenv("SNOWFLAKE_ROLE"),
		TablePrefix:      getEnv("SNOWFLAKE_TABLE_PREFIX"),
	}

	// Validate required vars
	required := map[string]string{
		"SNOWFLAKE_ACCOUNT":      cfg.Account,
		"SNOWFLAKE_USER":         cfg.User,
		"SNOWFLAKE_PRIVATE_KEY":  cfg.PrivateKeyBase64,
		"SNOWFLAKE_DATABASE":     cfg.Database,
		"SNOWFLAKE_SCHEMA":       cfg.Schema,
		"SNOWFLAKE_WAREHOUSE":    cfg.Warehouse,
		"SNOWFLAKE_TABLE_PREFIX": cfg.TablePrefix,
	}

	for name, value := range required {
		if value == "" {
			log.Fatalf("❌ Missing required env var: %s", name)
		}
	}

	log.Println("🔐 Testing Snowflake credentials...")

	// Attempt connection
	client, err := snowflake.New(cfg)
	if err != nil {
		log.Fatalf("❌ Connection failed: %v", err)
	}
	defer client.Close()

	log.Println("✅ Connected successfully")

	// Test query
	ctx := context.Background()
	lastBlock, err := client.GetLastBlock(ctx)
	if err != nil {
		log.Fatalf("❌ Query failed: %v", err)
	}

	log.Printf("✅ Query successful")
	log.Printf("📊 Last block in %sBLOCKS: %d", cfg.TablePrefix, lastBlock)

	log.Println("\n✅ All checks passed! Credentials are valid.")
}

func getEnv(key string) string {
	return os.Getenv(key)
}
