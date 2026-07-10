package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-api/internal/migrationrunner"
	"github.com/kapu/hololive-api/scripts/migrations"
)

const commandTimeout = 15 * time.Minute

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Printf("db-migrate failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	baselineThrough := flag.String("baseline-through", os.Getenv("MIGRATION_BASELINE_THROUGH"), "baseline watermark")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	if err := bootstrapScraperRole(ctx); err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, postgresConnString())
	if err != nil {
		return fmt.Errorf("open postgres pool: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	stdout := log.New(os.Stdout, "", 0)
	result, err := migrationrunner.Run(ctx, pool, migrations.FS, migrationrunner.Config{
		BaselineThrough: *baselineThrough,
		Logf:            stdout.Printf,
	})
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(os.Stdout, "==> hololive migrations applied (applied=%d skipped=%d total=%d)\n", result.Applied, result.Skipped, result.Total); err != nil {
		log.Printf("db-migrate: final stdout write failed after successful migration: %v", err)
	}
	return nil
}

func postgresConnString() string {
	parts := []string{
		connPart("host", envDefault("PGHOST", "postgres")),
		connPart("port", envDefault("PGPORT", "5432")),
		connPart("dbname", envDefault("PGDATABASE", "hololive")),
		connPart("user", envDefault("PGUSER", "hololive_migrator")),
	}

	if password := os.Getenv("PGPASSWORD"); password != "" {
		parts = append(parts, connPart("password", password))
	}
	parts = append(parts, sslConnParts()...)
	return strings.Join(parts, " ")
}

func sslConnParts() []string {
	var parts []string
	if sslMode := envDefault("PGSSLMODE", "verify-full"); sslMode != "" {
		parts = append(parts, connPart("sslmode", sslMode))
	}
	if rootCert := os.Getenv("PGSSLROOTCERT"); rootCert != "" {
		parts = append(parts, connPart("sslrootcert", rootCert))
	}
	return parts
}

func connPart(key, value string) string {
	return key + "=" + quoteConnValue(value)
}

func quoteConnValue(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `'`, `\'`)
	return "'" + escaped + "'"
}

func envDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
