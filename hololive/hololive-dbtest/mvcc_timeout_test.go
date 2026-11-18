package dbtest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestIdleInTransactionSessionTimeoutDatabaseDefault(t *testing.T) {
	t.Parallel()

	pool := NewReplayPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)

	defer cancel()

	var databaseDefaultExists bool

	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_catalog.pg_db_role_setting AS database_setting
			CROSS JOIN LATERAL pg_catalog.unnest(database_setting.setconfig) AS setting(config)
			WHERE database_setting.setdatabase = (
				SELECT database_catalog.oid
				FROM pg_catalog.pg_database AS database_catalog
				WHERE database_catalog.datname = pg_catalog.current_database()
			)
			  AND database_setting.setrole = 0
			  AND setting.config LIKE 'idle_in_transaction_session_timeout=%'
		)`).Scan(&databaseDefaultExists); err != nil {
		t.Fatalf("query idle transaction database default: %v", err)
	}

	if !databaseDefaultExists {
		t.Fatal("idle_in_transaction_session_timeout database default is missing")
	}

	connectionConfig := pool.Config().ConnConfig.Copy()

	connection, err := pgx.ConnectConfig(ctx, connectionConfig)
	if err != nil {
		t.Fatalf("connect fresh session for idle transaction timeout: %v", err)
	}

	defer func() {
		if closeErr := connection.Close(ctx); closeErr != nil {
			t.Errorf("close fresh timeout verification session: %v", closeErr)
		}
	}()

	var (
		timeoutMilliseconds int64
		timeoutUnit         string
	)

	if err := connection.QueryRow(ctx, `
		SELECT setting::bigint, unit
		FROM pg_catalog.pg_settings
		WHERE name = 'idle_in_transaction_session_timeout'`).Scan(&timeoutMilliseconds, &timeoutUnit); err != nil {
		t.Fatalf("read idle transaction timeout from fresh session: %v", err)
	}

	if timeoutUnit != "ms" || timeoutMilliseconds != 5*60*1000 {
		t.Fatalf(
			"idle_in_transaction_session_timeout = %d%s, want 300000ms",
			timeoutMilliseconds,
			timeoutUnit,
		)
	}
}
