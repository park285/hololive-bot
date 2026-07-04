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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-api/internal/migrationrunner"
	"github.com/kapu/hololive-api/scripts/migrations"
)

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

	pool, err := pgxpool.New(ctx, postgresConnString())
	if err != nil {
		return fmt.Errorf("open postgres pool: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	result, err := migrationrunner.Run(ctx, pool, migrations.FS, migrationrunner.Config{
		BaselineThrough: *baselineThrough,
	})
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(os.Stdout, "==> hololive migrations applied (applied=%d)\n", result.Applied); err != nil {
		return fmt.Errorf("write stdout: %w", err)
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
	if sslMode := envDefault("PGSSLMODE", "verify-full"); sslMode != "" {
		parts = append(parts, connPart("sslmode", sslMode))
	}
	if rootCert := os.Getenv("PGSSLROOTCERT"); rootCert != "" {
		parts = append(parts, connPart("sslrootcert", rootCert))
	}
	return strings.Join(parts, " ")
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
