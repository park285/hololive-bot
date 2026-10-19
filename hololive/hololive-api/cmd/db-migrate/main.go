package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"

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

	pool, err := pgxdb.OpenPoolDSN(ctx, connString, pgxdb.Options{})
	if err != nil {
		return fmt.Errorf("open postgres pool: %w", err)
	}
	defer pool.Close()

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
// 비어 있으면 admin DB_PASSWORD로 대신하거나 비밀번호 없는 접속을 시도하지 않고, 원인을 알 수 있게 바로 실패한다.
func postgresConnString() (string, error) {
	password := os.Getenv("PGPASSWORD")
	if strings.TrimSpace(password) == "" {
		return "", errors.New("PGPASSWORD (HOLOLIVE_MIGRATOR_PASSWORD) is required")
	}

	if err := validatePrivilegedTransport(); err != nil {
		return "", err
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
	parts := []string{connPart("sslmode", os.Getenv("PGSSLMODE"))}

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

// privileged 접속은 격리된 Unix socket 또는 인증서 검증 TCP만 허용한다.
func validatePrivilegedTransport() error {
	host := envDefault("PGHOST", "postgres")
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("PGSSLMODE")))

	if filepath.IsAbs(host) && !strings.Contains(host, ",") {
		if mode != "disable" {
			return errors.New("unix socket PGHOST requires PGSSLMODE=disable")
		}

		return nil
	}

	if mode != "verify-full" {
		return errors.New("network PGHOST requires PGSSLMODE=verify-full")
	}

	return nil
}
