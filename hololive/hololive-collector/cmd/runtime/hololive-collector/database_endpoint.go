package main

import (
	"context"
	_ "embed"
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

//go:embed queries/read_only_session_limits.sql
var readOnlySessionLimitsSQL string

//go:embed queries/verify_database_session.sql
var verifyDatabaseSessionSQL string

func verifyDatabaseSession(ctx context.Context, conn *pgx.Conn, role string) (err error) {
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return errors.New("read-only transaction failed")
	}

	defer func() {
		if rollbackErr := pgxdb.Rollback(ctx, tx); rollbackErr != nil && err == nil {
			err = errors.New("read-only transaction rollback failed")
		}
	}()

	if _, execErr := tx.Exec(ctx, readOnlySessionLimitsSQL); execErr != nil {
		return errors.New("read-only transaction limits failed")
	}

	var verified bool

	if scanErr := tx.QueryRow(ctx, verifyDatabaseSessionSQL, role).Scan(&verified); scanErr != nil || !verified {
		return errors.New("database backend identity check failed")
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
