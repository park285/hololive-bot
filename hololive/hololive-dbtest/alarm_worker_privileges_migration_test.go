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

const alarmWorkerRoleMigration = "028_alarm_worker_database_role.sql"

func TestAlarmWorkerRoleGrantsAreLeastPrivilege(t *testing.T) {
	pool := NewPool(t)
	role := createAlarmWorkerGrantRole(t, pool)
	grantAlarmWorkerBroadPrivileges(t, pool, role)
	applyAlarmWorkerRoleMigration(t, pool, role)

	assertAlarmWorkerSchemaPrivileges(t, pool, role)
	assertAlarmWorkerTablePrivileges(t, pool, role)
	assertAlarmWorkerSequencePrivileges(t, pool, role)
	assertAlarmWorkerDefaultPrivileges(t, pool, role)
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

func grantAlarmWorkerBroadPrivileges(t *testing.T, pool *pgxpool.Pool, role string) {
	t.Helper()

	quoted := pgx.Identifier{role}.Sanitize()
	if _, err := pool.Exec(t.Context(), "GRANT ALL PRIVILEGES ON SCHEMA public TO "+quoted); err != nil {
		t.Fatalf("grant broad schema privileges: %v", err)
	}
	if _, err := pool.Exec(t.Context(), "GRANT ALL PRIVILEGES ON SCHEMA public TO PUBLIC"); err != nil {
		t.Fatalf("grant broad public schema privileges: %v", err)
	}

	for _, objects := range []string{"TABLES", "SEQUENCES", "FUNCTIONS"} {
		if _, err := pool.Exec(t.Context(), "GRANT ALL PRIVILEGES ON ALL "+objects+" IN SCHEMA public TO "+quoted); err != nil {
			t.Fatalf("grant broad existing %s privileges: %v", objects, err)
		}
		if _, err := pool.Exec(t.Context(), "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON "+objects+" TO "+quoted); err != nil {
			t.Fatalf("grant broad default %s privileges: %v", objects, err)
		}
	}

	if _, err := pool.Exec(t.Context(), "GRANT ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public TO PUBLIC"); err != nil {
		t.Fatalf("grant broad public function privileges: %v", err)
	}
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

func assertAlarmWorkerTablePrivileges(t *testing.T, pool *pgxpool.Pool, role string) {
	t.Helper()

	expected := map[string]map[string]bool{
		"alarm_dispatch_closeout_receipts":          tablePrivileges("SELECT"),
		"alarm_dispatch_deliveries":                 tablePrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
		"alarm_dispatch_event_collisions":           tablePrivileges("SELECT", "INSERT", "UPDATE"),
		"alarm_dispatch_events":                     tablePrivileges("SELECT", "INSERT", "DELETE"),
		"alarm_dispatch_send_units":                 tablePrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
		"alarm_room_display_names":                  tablePrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
		"alarm_upcoming_candidates":                 tablePrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
		"alarms":                                    tablePrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
		"kakao_rooms":                               tablePrivileges("SELECT", "INSERT", "UPDATE"),
		"members":                                   tablePrivileges("SELECT"),
		"message_strings":                           tablePrivileges("SELECT"),
		"notification_templates":                    tablePrivileges("SELECT"),
		"source_observation_queue":                  tablePrivileges(),
		"source_observations":                       tablePrivileges(),
		"schedule_posters":                          tablePrivileges(),
		"x_space_session":                           tablePrivileges("SELECT", "UPDATE"),
		"x_space_starts":                            tablePrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
		"youtube_collection_projection_generations": tablePrivileges(),
		"youtube_collection_targets":                tablePrivileges(),
		"youtube_community_shorts_alarm_states":     tablePrivileges("SELECT", "INSERT", "UPDATE"),
		"youtube_content_alarm_tracking":            tablePrivileges("SELECT", "UPDATE"),
		"youtube_live_pending_ends":                 tablePrivileges(),
		"youtube_live_sessions":                     tablePrivileges("SELECT"),
		"youtube_notification_delivery":             tablePrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
		"youtube_notification_delivery_ledger":      tablePrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
		"youtube_notification_delivery_telemetry": tablePrivileges(
			"SELECT", "INSERT", "UPDATE", "DELETE",
		),
		"youtube_notification_outbox":       tablePrivileges("SELECT", "UPDATE", "DELETE"),
		"youtube_notification_send_request": tablePrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
		"youtube_schedule_items":            tablePrivileges("SELECT"),
	}

	allowedTables := make([]string, 0, len(expected))
	for table, privileges := range expected {
		if len(privileges) > 0 {
			allowedTables = append(allowedTables, table)
		}

		for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
			var got bool
			if err := pool.QueryRow(
				t.Context(),
				"SELECT has_table_privilege($1, $2, $3)",
				role,
				"public."+table,
				privilege,
			).Scan(&got); err != nil {
				t.Fatalf("check alarm-worker %s privilege on %s: %v", privilege, table, err)
			}

			if got != privileges[privilege] {
				t.Errorf("alarm-worker %s privilege on %s = %t, want %t", privilege, table, got, privileges[privilege])
			}
		}
	}

	var unexpected int
	if err := pool.QueryRow(t.Context(), `SELECT count(*)
		FROM pg_class AS relation
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = 'public'
		  AND relation.relkind IN ('r', 'p', 'v', 'm', 'f')
		  AND relation.relname::text <> ALL($2::text[])
		  AND (
		      has_table_privilege($1, relation.oid, 'SELECT')
		      OR has_table_privilege($1, relation.oid, 'INSERT')
		      OR has_table_privilege($1, relation.oid, 'UPDATE')
		      OR has_table_privilege($1, relation.oid, 'DELETE')
		      OR has_table_privilege($1, relation.oid, 'TRUNCATE')
		      OR has_table_privilege($1, relation.oid, 'REFERENCES')
		      OR has_table_privilege($1, relation.oid, 'TRIGGER')
		  )`, role, allowedTables).Scan(&unexpected); err != nil {
		t.Fatal(err)
	}

	if unexpected != 0 {
		t.Fatalf("alarm-worker retained privileges on %d tables outside its allowlist", unexpected)
	}
}

func assertAlarmWorkerSequencePrivileges(t *testing.T, pool *pgxpool.Pool, role string) {
	t.Helper()

	allowed := map[string]bool{
		"alarm_dispatch_deliveries_id_seq":               true,
		"alarm_dispatch_event_collisions_id_seq":         true,
		"alarm_dispatch_events_id_seq":                   true,
		"alarm_dispatch_send_units_id_seq":               true,
		"alarms_id_seq":                                  true,
		"source_observations_id_seq":                     false,
		"youtube_notification_delivery_id_seq":           true,
		"youtube_notification_delivery_telemetry_id_seq": true,
		"youtube_notification_outbox_id_seq":             false,
	}

	for sequence, wantUsage := range allowed {
		var usage, selectPrivilege, updatePrivilege bool
		if err := pool.QueryRow(
			t.Context(),
			"SELECT has_sequence_privilege($1, $2, 'USAGE'), has_sequence_privilege($1, $2, 'SELECT'), has_sequence_privilege($1, $2, 'UPDATE')",
			role,
			"public."+sequence,
		).Scan(&usage, &selectPrivilege, &updatePrivilege); err != nil {
			t.Fatalf("check alarm-worker sequence privileges on %s: %v", sequence, err)
		}

		if usage != wantUsage || selectPrivilege || updatePrivilege {
			t.Errorf("alarm-worker sequence privileges on %s = usage:%t select:%t update:%t, want usage:%t select:false update:false", sequence, usage, selectPrivilege, updatePrivilege, wantUsage)
		}
	}
}

func assertAlarmWorkerDefaultPrivileges(t *testing.T, pool *pgxpool.Pool, role string) {
	t.Helper()

	var currentFunctionAccess bool
	if err := pool.QueryRow(
		t.Context(),
		"SELECT has_function_privilege($1, 'public.delete_source_observation_retention_batch(text[],timestamptz[],integer)', 'EXECUTE')",
		role,
	).Scan(&currentFunctionAccess); err != nil {
		t.Fatal(err)
	}

	if currentFunctionAccess {
		t.Fatal("alarm-worker retained execution access to an API-owned function")
	}

	var executableFunctions int
	if err := pool.QueryRow(t.Context(), `SELECT count(*)
		FROM pg_proc AS proc
		JOIN pg_namespace AS nsp ON nsp.oid = proc.pronamespace
		WHERE nsp.nspname = 'public'
		  AND has_function_privilege($1, proc.oid, 'EXECUTE')`, role).Scan(&executableFunctions); err != nil {
		t.Fatal(err)
	}

	if executableFunctions != 0 {
		t.Fatalf("alarm-worker retained execution access to %d public functions", executableFunctions)
	}

	if _, err := pool.Exec(t.Context(), `
		CREATE TABLE alarm_worker_future_acl_probe(id bigserial PRIMARY KEY);
		CREATE FUNCTION alarm_worker_future_acl_probe() RETURNS integer LANGUAGE sql AS 'SELECT 1'`); err != nil {
		t.Fatal(err)
	}

	var tableAccess, sequenceAccess, functionAccess bool
	if err := pool.QueryRow(t.Context(), `SELECT
		has_table_privilege($1, 'public.alarm_worker_future_acl_probe', 'SELECT')
		OR has_table_privilege($1, 'public.alarm_worker_future_acl_probe', 'INSERT')
		OR has_table_privilege($1, 'public.alarm_worker_future_acl_probe', 'UPDATE')
		OR has_table_privilege($1, 'public.alarm_worker_future_acl_probe', 'DELETE'),
		has_sequence_privilege($1, 'public.alarm_worker_future_acl_probe_id_seq', 'USAGE')
		OR has_sequence_privilege($1, 'public.alarm_worker_future_acl_probe_id_seq', 'SELECT'),
		has_function_privilege($1, 'public.alarm_worker_future_acl_probe()', 'EXECUTE')`, role).
		Scan(&tableAccess, &sequenceAccess, &functionAccess); err != nil {
		t.Fatal(err)
	}

	if tableAccess || sequenceAccess || functionAccess {
		t.Fatalf("future object privileges = table:%t sequence:%t function:%t, want default denial", tableAccess, sequenceAccess, functionAccess)
	}
}

func tablePrivileges(privileges ...string) map[string]bool {
	out := make(map[string]bool, len(privileges))
	for _, privilege := range privileges {
		out[privilege] = true
	}

	return out
}
