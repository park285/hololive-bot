// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package dbtest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestIdleInTransactionSessionTimeoutDatabaseDefault(t *testing.T) {
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
