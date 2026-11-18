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

func checkPooledDatabase(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()

	cfg, err := pooledCheckConfig()
	if err != nil {
		return err
	}

	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return errors.New("pooled connection failed")
	}

	proofErr := verifyPooledSession(ctx, conn, cfg.User)
	closeErr := conn.Close(ctx)

	if proofErr != nil {
		return proofErr
	}

	if closeErr != nil {
		return errors.New("pooled connection close failed")
	}

	return nil
}

func pooledCheckConfig() (*pgx.ConnConfig, error) {
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "PG") && key != "PGAPPNAME" {
			return nil, errUnexpectedLibpqEnv
		}
	}

	db, err := settings.LoadPostgresEndpointConfig()
	if err != nil || !pooledEndpointAllowed(os.Getenv("YOUTUBE_COLLECTOR_INSTANCE_ID"), &db) {
		return nil, errors.New("invalid collector database settings")
	}

	cfg, err := pgxdb.ParseConfig("host=" + db.Host + " port=" + strconv.Itoa(db.Port) + " dbname=" + db.Database +
		" user=" + db.User + " password=unused passfile=/dev/null sslcert='' sslkey='' sslmode=verify-full sslrootcert=" + db.SSLRootCert +
		" default_query_exec_mode=cache_statement")
	if err != nil || cfg.TLSConfig == nil || len(cfg.Fallbacks) != 0 || cfg.DefaultQueryExecMode != pgx.QueryExecModeCacheStatement {
		return nil, errors.New("invalid pooled connection configuration")
	}

	cfg.Password = db.Password
	cfg.RequireAuth = "scram-sha-256"
	cfg.ConnectTimeout = 5 * time.Second
	cfg.RuntimeParams["application_name"] = "hololive-collector-pooled-check"

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

func verifyPooledSession(ctx context.Context, conn *pgx.Conn, role string) error {
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
		AND inet_client_addr() = inet '172.23.255.254'
		AND to_regclass('public.member_schedule_card_templates') IS NOT NULL
		AND (SELECT count(*)=2 FROM pg_catalog.pg_trigger WHERE NOT tgisinternal AND tgenabled IN ('O','A') AND (
			(tgrelid='public.source_observations'::regclass AND tgname='reject_retired_holodex_channel_profile' AND tgfoid=to_regprocedure('public.reject_retired_holodex_channel_profile()')) OR
			(tgrelid='public.source_observation_queue'::regclass AND tgname='terminalize_retired_holodex_channel_profile' AND tgfoid=to_regprocedure('public.terminalize_retired_holodex_channel_profile()'))))
		AND EXISTS (SELECT 1 FROM pg_catalog.pg_stat_ssl WHERE pid = pg_backend_pid() AND ssl AND version IN ('TLSv1.2','TLSv1.3'))`

	var verified bool

	if err := tx.QueryRow(ctx, query, role).Scan(&verified); err != nil || !verified {
		return errors.New("pooled backend identity check failed")
	}

	if err := tx.Rollback(ctx); err != nil {
		return errors.New("read-only transaction rollback failed")
	}

	return nil
}

func pooledEndpointAllowed(instance string, db *settings.PostgresConfig) bool {
	role, ok := runtimepolicy.PostgresCollectorRoleUser(instance)
	if !ok || db.User != role || db.Database != "hololive" || db.SocketPath != "" || db.Password == "" ||
		db.SSLMode != "verify-full" || db.QueryExecMode != "cache_statement" {
		return false
	}

	host, port, ca := "100.100.1.8", 6433, "/run/hololive-bot/certs/postgres-ca.pem"

	switch instance {
	case "youtube-collector-a":
		ca = "/etc/stack-secrets/hololive-bot/certs/postgres-ca.pem"
	case "youtube-collector-c":
		host, port = "holo-pgbouncer", 6432
	}

	return db.Host == host && db.Port == port && db.SSLRootCert == ca
}
