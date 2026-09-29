package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
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
	allowBlockingIndexDropDefault, err := envBool("MIGRATION_ALLOW_BLOCKING_INDEX_DROP")
	if err != nil {
		return fmt.Errorf("env bool: %w", err)
	}

	baselineThrough := flag.String("baseline-through", os.Getenv("MIGRATION_BASELINE_THROUGH"), "baseline watermark")
	statementTimeout := flag.Duration("statement-timeout", 0, "per-statement maintenance limit (0 uses 4m; maximum 10m; command limit remains 15m)")
	allowBlockingIndexDrop := flag.Bool(
		"allow-blocking-index-drop",
		allowBlockingIndexDropDefault,
		"allow plain index drops on an existing database during a dedicated maintenance window",
	)

	flag.Parse()

	if *statementTimeout < 0 || *statementTimeout > migrationrunner.MaxStatementTimeout {
		return fmt.Errorf("statement-timeout must be between 0 and %s", migrationrunner.MaxStatementTimeout)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	if bootstrapErr := bootstrapScraperRole(ctx); bootstrapErr != nil {
		return fmt.Errorf("bootstrap scraper role: %w", bootstrapErr)
	}

	connString, err := postgresConnString()
	if err != nil {
		return fmt.Errorf("postgres conn string: %w", err)
	}

	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return fmt.Errorf("open postgres pool: %w", err)
	}
	defer pool.Close()

	if pingErr := pool.Ping(ctx); pingErr != nil {
		return fmt.Errorf("ping postgres: %w", pingErr)
	}

	stdout := log.New(os.Stdout, "", 0)

	result, err := migrationrunner.Run(ctx, pool, migrations.FS, migrationrunner.Config{
		BaselineThrough:        *baselineThrough,
		AllowBlockingIndexDrop: *allowBlockingIndexDrop,
		StatementTimeout:       *statementTimeout,
		Logf:                   stdout.Printf,
	})
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}

	if _, err := fmt.Fprintf(os.Stdout, "==> hololive migrations applied (applied=%d skipped=%d total=%d)\n", result.Applied, result.Skipped, result.Total); err != nil {
		log.Printf("db-migrate: final stdout write failed after successful migration: %v", err)
	}

	return nil
}

// postgresConnString은 migrator 접속 문자열을 만든다. PGPASSWORD(compose의 HOLOLIVE_MIGRATOR_PASSWORD)는 필수다.
// 예전에 compose가 admin DB_PASSWORD로 채우던 폴백을 지웠으므로, 비어 있으면 비밀번호 없는 접속을 시도하지 않고
// 원인을 알 수 있게 바로 실패한다(stack audit 2026-09-26).
func postgresConnString() (string, error) {
	password := os.Getenv("PGPASSWORD")
	if strings.TrimSpace(password) == "" {
		return "", errors.New("PGPASSWORD (HOLOLIVE_MIGRATOR_PASSWORD) is required")
	}

	sslParts := sslConnParts()
	parts := make([]string, 0, 5+len(sslParts))

	parts = append(parts,
		connPart("host", envDefault("PGHOST", "postgres")),
		connPart("port", envDefault("PGPORT", "5432")),
		connPart("dbname", envDefault("PGDATABASE", "hololive")),
		connPart("user", envDefault("PGUSER", "hololive_migrator")),
		connPart("password", password),
	)
	parts = append(parts, sslParts...)

	return strings.Join(parts, " "), nil
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

func envBool(key string) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return false, nil
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", key, err)
	}

	return value, nil
}
