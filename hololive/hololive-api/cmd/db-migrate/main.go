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
	"strings"
	"syscall"
	"time"

	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-api/internal/migrationrunner"
	"github.com/kapu/hololive-api/scripts/migrations"
)

const commandTimeout = 15 * time.Minute

var deploymentOnly string

func migrationPoolOptions() pgxdb.Options {
	return pgxdb.Options{Ping: pgxdb.PingConfig{PingTimeout: 5 * time.Second}}
}

func main() {
	log.SetFlags(0)

	if err := run(); err != nil {
		log.Printf("db-migrate failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	if deploymentOnly != "" && (len(os.Args) != 2 || (os.Args[1] != "--schema-only" && os.Args[1] != "--initialize" && os.Args[1] != "--check")) {
		return errors.New("specify --schema-only, --check, or --initialize")
	}

	schemaOnly := flag.Bool("schema-only", false, "apply pending transactional migrations to a verified lineage using the protected endpoint")
	check := flag.Bool("check", false, "read-only verification of the complete migration head; fail when a migration is pending")
	configureRoles := flag.Bool("configure-login-roles", false, "configure login roles on a verified, already initialized database")
	initialize := flag.Bool("initialize", false, "initialize an empty database")
	statementTimeout := flag.Duration("statement-timeout", 0, "per-statement maintenance limit (default 4m, maximum 10m)")

	flag.Parse()

	if flag.NArg() != 0 {
		return errors.New("unexpected positional argument")
	}

	if multipleModes(*check, *configureRoles, *initialize, *schemaOnly) {
		return errors.New("select a single migration or login-role operation")
	}

	if *statementTimeout < 0 || *statementTimeout > migrationrunner.MaxStatementTimeout {
		return fmt.Errorf("statement-timeout must be between 0 and %s", migrationrunner.MaxStatementTimeout)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cfg := migrationrunner.Config{
		MigrationWindow:  *schemaOnly || *configureRoles,
		VerifyOnly:       *check || *configureRoles,
		InitializeOnly:   *initialize,
		StatementTimeout: *statementTimeout,
		BeforeInitialize: bootstrapLoginRoles,
	}

	if *configureRoles && strings.TrimSpace(os.Getenv("POSTGRES_ADMIN_PASSWORD")) == "" {
		return errors.New("configure-login-roles requires POSTGRES_ADMIN_PASSWORD")
	}

	if err := runMigrations(ctx, cfg); err != nil {
		return err
	}

	if *configureRoles {
		return bootstrapLoginRoles(ctx)
	}

	return nil
}

func multipleModes(modes ...bool) bool {
	selected := false

	for _, mode := range modes {
		if mode && selected {
			return true
		}

		selected = selected || mode
	}

	return false
}

func runMigrations(ctx context.Context, cfg migrationrunner.Config) error {
	connString, err := postgresConnString()
	if err != nil {
		return fmt.Errorf("postgres conn string: %w", err)
	}

	options := migrationPoolOptions()
	if preflightErr := pgxdb.PreflightDSN(connString, options); preflightErr != nil {
		return fmt.Errorf("preflight postgres pool: %w", preflightErr)
	}

	pool, err := pgxdb.OpenPoolDSN(ctx, connString, options)
	if err != nil {
		return fmt.Errorf("open postgres pool: %w", err)
	}
	defer pool.Close()

	result, err := migrationrunner.Run(ctx, pool, migrations.FS, cfg)
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}

	if _, err := fmt.Fprintf(os.Stdout, "==> hololive migrations applied (applied=%d skipped=%d total=%d)\n", result.Applied, result.Skipped, result.Total); err != nil {
		log.Printf("db-migrate: final stdout write failed after successful migration: %v", err)
	}

	return nil
}

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
