package main

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

var errUnexpectedLibpqEnv = errors.New("unexpected libpq environment")

type collectorEndpoint struct {
	host string
	port int
	ca   string
}

const (
	centralPostgresAddress = "100.100.1.8"
	centralPostgresTLSPort = 5433
	containerPostgresCA    = "/run/hololive-bot/certs/postgres-ca.pem"
)

var collectorEndpoints = map[string]collectorEndpoint{
	"collector-a": {centralPostgresAddress, centralPostgresTLSPort, "/etc/stack-secrets/hololive-bot/certs/postgres-ca.pem"},
	"collector-b": {centralPostgresAddress, centralPostgresTLSPort, containerPostgresCA},
	"collector-c": {"holo-postgres", 5432, containerPostgresCA},
	"collector-d": {centralPostgresAddress, centralPostgresTLSPort, containerPostgresCA},
}

func checkDatabaseEndpoint(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()

	cfg, err := databaseCheckConfig()
	if err != nil {
		return err
	}

	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return errors.New("database connection failed")
	}

	proofErr := verifyDatabaseSession(ctx, conn, cfg.User)
	closeErr := conn.Close(ctx)

	if proofErr != nil {
		return proofErr
	}

	if closeErr != nil {
		return errors.New("database connection close failed")
	}

	return nil
}

func databaseCheckConfig() (*pgx.ConnConfig, error) {
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "PG") && key != "PGAPPNAME" {
			return nil, errUnexpectedLibpqEnv
		}
	}

	instance := os.Getenv("COLLECTOR_INSTANCE_ID")

	db, err := settings.LoadPostgresEndpointConfig()
	if err != nil || !databaseEndpointAllowed(instance, &db) {
		return nil, errors.New("invalid collector database settings")
	}

	cfg, err := pgxdb.ParseConfig("host=" + db.Host + " port=" + strconv.Itoa(db.Port) + " dbname=" + db.Database +
		" user=" + db.User + " password=unused passfile=/dev/null sslcert='' sslkey='' sslmode=verify-full sslrootcert=" + db.SSLRootCert +
		" default_query_exec_mode=cache_statement")
	if err != nil || cfg.TLSConfig == nil || len(cfg.Fallbacks) != 0 || cfg.DefaultQueryExecMode != pgx.QueryExecModeCacheStatement {
		return nil, errors.New("invalid database connection configuration")
	}

	cfg.Password = db.Password
	cfg.RequireAuth = "scram-sha-256"
	cfg.ConnectTimeout = 5 * time.Second
	cfg.RuntimeParams["application_name"] = "hololive-collector-database-check"

	dial := cfg.DialFunc

	var attempted atomic.Bool

	cfg.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		if attempted.Swap(true) {
			return nil, errors.New("additional database connection refused")
		}

		return dial(ctx, network, address)
	}

	return cfg, nil
}

func verifyDatabaseSession(ctx context.Context, conn *pgx.Conn, role string) error {
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return errors.New("read-only transaction failed")
	}

	for _, statement := range [...]string{
		"SET LOCAL default_transaction_read_only = on",
		"SET LOCAL statement_timeout = '30s'",
		"SET LOCAL lock_timeout = '3s'",
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return errors.New("read-only transaction limits failed")
		}
	}

	const query = `SELECT current_database() = 'hololive' AND current_user = $1::name AND session_user = $1::name
		AND current_setting('default_transaction_read_only') = 'on' AND current_setting('transaction_read_only') = 'on'
		AND current_setting('statement_timeout') = '30s' AND current_setting('lock_timeout') = '3s'
		AND EXISTS (SELECT 1 FROM pg_catalog.pg_stat_ssl WHERE pid = pg_backend_pid() AND ssl AND version = 'TLSv1.3')`

	var verified bool

	if err := tx.QueryRow(ctx, query, role).Scan(&verified); err != nil || !verified {
		return errors.New("database backend identity check failed")
	}

	if err := tx.Rollback(ctx); err != nil {
		return errors.New("read-only transaction rollback failed")
	}

	return nil
}

func databaseEndpointAllowed(instance string, db *settings.PostgresConfig) bool {
	role, ok := runtimepolicy.PostgresCollectorRoleUser(instance)
	if !ok || db.User != role || db.Database != "hololive" || db.SocketPath != "" || db.Password == "" ||
		db.SSLMode != "verify-full" || db.QueryExecMode != "cache_statement" {
		return false
	}

	endpoint, ok := collectorEndpoints[instance]

	return ok && db.Host == endpoint.host && db.Port == endpoint.port && db.SSLRootCert == endpoint.ca
}
