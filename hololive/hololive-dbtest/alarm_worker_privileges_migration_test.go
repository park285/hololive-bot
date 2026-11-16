package dbtest

import (
	"context"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	alarmWorkerRoleMigration = "029_alarm_worker_database_role.sql"
	readWriteDeleteACL       = "arwd"
)

var alarmWorkerTableACL = map[string]string{
	"public.alarm_dispatch_closeout_receipts":        "r",
	"public.alarm_dispatch_deliveries":               readWriteDeleteACL,
	"public.alarm_dispatch_event_collisions":         "arw",
	"public.alarm_dispatch_events":                   readWriteDeleteACL,
	"public.alarm_dispatch_send_units":               readWriteDeleteACL,
	"public.alarm_room_display_names":                readWriteDeleteACL,
	"public.alarm_upcoming_candidates":               readWriteDeleteACL,
	"public.alarms":                                  readWriteDeleteACL,
	"public.kakao_rooms":                             "arw",
	"public.members":                                 "r",
	"public.message_strings":                         "r",
	"public.notification_templates":                  "r",
	"public.x_space_session":                         "rw",
	"public.x_space_starts":                          "ard",
	"public.youtube_community_shorts_alarm_states":   "arw",
	"public.youtube_content_alarm_tracking":          "rw",
	"public.youtube_live_sessions":                   "r",
	"public.youtube_notification_delivery":           "arw",
	"public.youtube_notification_delivery_ledger":    readWriteDeleteACL,
	"public.youtube_notification_delivery_telemetry": readWriteDeleteACL,
	"public.youtube_notification_outbox":             "rwd",
	"public.youtube_notification_send_request":       readWriteDeleteACL,
	"public.youtube_schedule_items":                  "r",
}

var alarmWorkerSequenceACL = map[string]string{
	"public.alarm_dispatch_deliveries_id_seq":               "U",
	"public.alarm_dispatch_event_collisions_id_seq":         "U",
	"public.alarm_dispatch_events_id_seq":                   "U",
	"public.alarm_dispatch_send_units_id_seq":               "U",
	"public.alarms_id_seq":                                  "U",
	"public.youtube_notification_delivery_id_seq":           "U",
	"public.youtube_notification_delivery_telemetry_id_seq": "U",
}

func TestAlarmWorkerRoleGrantsAreLeastPrivilege(t *testing.T) {
	pool := NewPool(t)
	role := createAlarmWorkerGrantRole(t, pool)
	applyAlarmWorkerRoleMigration(t, pool, role)

	assertAlarmWorkerSchemaPrivileges(t, pool, role)
	assertAlarmWorkerACL(t, pool, role, "table", alarmWorkerTableACL, `SELECT format('%I.%I', namespace.nspname, relation.relname),
		concat_ws('',
			CASE WHEN has_table_privilege($1, relation.oid, 'INSERT') THEN 'a' END,
			CASE WHEN has_table_privilege($1, relation.oid, 'SELECT') THEN 'r' END,
			CASE WHEN has_table_privilege($1, relation.oid, 'UPDATE') THEN 'w' END,
			CASE WHEN has_table_privilege($1, relation.oid, 'DELETE') THEN 'd' END,
			CASE WHEN has_table_privilege($1, relation.oid, 'TRUNCATE') THEN 'D' END,
			CASE WHEN has_table_privilege($1, relation.oid, 'REFERENCES') THEN 'x' END,
			CASE WHEN has_table_privilege($1, relation.oid, 'TRIGGER') THEN 't' END,
			CASE WHEN has_table_privilege($1, relation.oid, 'MAINTAIN') THEN 'm' END)
		FROM pg_class AS relation
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = 'public' AND relation.relkind IN ('r', 'p', 'v', 'm', 'f')`)
	assertAlarmWorkerACL(t, pool, role, "sequence", alarmWorkerSequenceACL, `SELECT format('%I.%I', namespace.nspname, relation.relname),
		concat_ws('',
			CASE WHEN has_sequence_privilege($1, relation.oid, 'USAGE') THEN 'U' END,
			CASE WHEN has_sequence_privilege($1, relation.oid, 'SELECT') THEN 'r' END,
			CASE WHEN has_sequence_privilege($1, relation.oid, 'UPDATE') THEN 'w' END)
		FROM pg_class AS relation
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = 'public' AND relation.relkind = 'S'`)
	assertAlarmWorkerFunctionPrivileges(t, pool, role)
	assertAlarmWorkerDefaultPrivileges(t, pool, role)
}

func createAlarmWorkerGrantRole(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	role := "dbtest_alarm_worker_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	quoted := pgx.Identifier{role}.Sanitize()

	if _, err := pool.Exec(t.Context(), "CREATE ROLE "+quoted+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT"); err != nil {
		t.Fatalf("create alarm-worker grant role: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
		defer cancel()

		if _, err := pool.Exec(ctx, "DROP OWNED BY "+quoted); err != nil {
			t.Errorf("cleanup alarm-worker grant role privileges: %v", err)
		}

		if _, err := pool.Exec(ctx, "DROP ROLE "+quoted); err != nil {
			t.Errorf("cleanup alarm-worker grant role: %v", err)
		}
	})

	return role
}

func applyAlarmWorkerRoleMigration(t *testing.T, pool *pgxpool.Pool, role string) {
	t.Helper()

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	raw, err := fs.ReadFile(os.DirFS(dir), alarmWorkerRoleMigration)
	if err != nil {
		t.Fatal(err)
	}

	migration := strings.ReplaceAll(string(raw), "hololive_alarm_worker", role)
	if err := applyMigrationContent(t.Context(), pool, alarmWorkerRoleMigration, migration); err != nil {
		t.Fatal(err)
	}
}

func assertAlarmWorkerSchemaPrivileges(t *testing.T, pool *pgxpool.Pool, role string) {
	t.Helper()

	var usage, create bool

	if err := pool.QueryRow(
		t.Context(),
		"SELECT has_schema_privilege($1, 'public', 'USAGE'), has_schema_privilege($1, 'public', 'CREATE')",
		role,
	).Scan(&usage, &create); err != nil {
		t.Fatal(err)
	}

	if !usage || create {
		t.Fatalf("alarm-worker schema privileges = usage:%t create:%t, want usage:true create:false", usage, create)
	}
}

func assertAlarmWorkerACL(t *testing.T, pool *pgxpool.Pool, role, kind string, want map[string]string, query string) {
	t.Helper()

	rows, err := pool.Query(t.Context(), query, role)
	if err != nil {
		t.Fatalf("read alarm-worker %s privileges: %v", kind, err)
	}

	got, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([2]string, error) {
		var pair [2]string

		return pair, row.Scan(&pair[0], &pair[1])
	})
	if err != nil {
		t.Fatalf("scan alarm-worker %s privileges: %v", kind, err)
	}

	seen := make(map[string]bool, len(want))

	for _, pair := range got {
		name, acl := pair[0], pair[1]
		if _, listed := want[name]; listed {
			seen[name] = true
		}

		if acl != want[name] {
			t.Errorf("alarm-worker %s privileges on %s = %q, want %q", kind, name, acl, want[name])
		}
	}

	for name := range want {
		if !seen[name] {
			t.Errorf("alarm-worker %s %s is missing from the schema", kind, name)
		}
	}
}

func assertAlarmWorkerFunctionPrivileges(t *testing.T, pool *pgxpool.Pool, role string) {
	t.Helper()

	var executable int

	if err := pool.QueryRow(t.Context(), `SELECT count(*)
		FROM pg_proc AS proc
		JOIN pg_namespace AS nsp ON nsp.oid = proc.pronamespace
		WHERE nsp.nspname = 'public'
		  AND has_function_privilege($1, proc.oid, 'EXECUTE')
		  AND (proc.prosecdef OR has_function_privilege('public', proc.oid, 'EXECUTE') IS NOT TRUE)`, role).Scan(&executable); err != nil {
		t.Fatal(err)
	}

	if executable != 0 {
		t.Fatalf("alarm-worker can execute %d SECURITY DEFINER or non-PUBLIC functions", executable)
	}
}

func assertAlarmWorkerDefaultPrivileges(t *testing.T, pool *pgxpool.Pool, role string) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `CREATE TABLE alarm_worker_future_acl_probe(id bigserial PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	var tableAccess, sequenceAccess bool

	if err := pool.QueryRow(t.Context(), `SELECT
		has_table_privilege($1, 'public.alarm_worker_future_acl_probe', 'SELECT,INSERT,UPDATE,DELETE'),
		has_sequence_privilege($1, 'public.alarm_worker_future_acl_probe_id_seq', 'USAGE,SELECT')`, role).
		Scan(&tableAccess, &sequenceAccess); err != nil {
		t.Fatal(err)
	}

	if tableAccess || sequenceAccess {
		t.Fatalf("future object privileges = table:%t sequence:%t, want default denial", tableAccess, sequenceAccess)
	}
}
