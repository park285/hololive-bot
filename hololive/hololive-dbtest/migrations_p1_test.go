package dbtest

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// reloptions는 schema_snapshot.golden.sql 직렬화(컬럼·제약·인덱스)에 포함되지 않아
// 111의 autovacuum 튜닝은 골든 대신 이 구조 테스트로 고정한다.
func TestTelemetryHotTableAutovacuumTuned(t *testing.T) {
	pool := NewPool(t)
	ctx := context.Background()

	var reloptions []string
	if err := pool.QueryRow(ctx,
		"SELECT COALESCE(reloptions, '{}') FROM pg_class WHERE relname = 'youtube_notification_delivery_telemetry' AND relnamespace = current_schema()::regnamespace",
	).Scan(&reloptions); err != nil {
		t.Fatalf("query reloptions: %v", err)
	}

	want := map[string]bool{
		"autovacuum_vacuum_scale_factor=0.02":  false,
		"autovacuum_vacuum_threshold=50":       false,
		"autovacuum_analyze_scale_factor=0.02": false,
		"autovacuum_analyze_threshold=50":      false,
	}
	for _, opt := range reloptions {
		if _, ok := want[opt]; ok {
			want[opt] = true
		}
	}
	for opt, found := range want {
		if !found {
			t.Errorf("youtube_notification_delivery_telemetry missing storage parameter %q (got %v)", opt, reloptions)
		}
	}
}

func TestMigration014OutboxGroupTemplatesSeedIsReapplySafe(t *testing.T) {
	pool := NewPool(t)
	ctx := context.Background()

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	// #nosec G304 -- 리포 내 마이그레이션 SSOT 디렉터리의 고정 파일명만 읽는다(사용자 입력 아님).
	raw, err := os.ReadFile(filepath.Join(dir, "014-add-outbox-group-templates.sql"))
	if err != nil {
		t.Fatalf("read 014 seed: %v", err)
	}

	if _, err := pool.Exec(ctx, string(raw)); err != nil {
		t.Fatalf("파일 중간 crash 후 재실행 시 014 전체가 재적용되므로 seed는 재실행-안전해야 한다: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM notification_templates WHERE template_key IN ('OUTBOX_VIDEO_GROUP','OUTBOX_SHORTS_GROUP','OUTBOX_COMMUNITY_GROUP') AND channel_id IS NULL",
	).Scan(&count); err != nil {
		t.Fatalf("count seeded templates: %v", err)
	}
	if count != 3 {
		t.Fatalf("seeded default templates = %d, want 3 (no duplicates after re-apply)", count)
	}
}

func TestSourceObservationMigrationReplaysWithoutRegressingContracts(t *testing.T) {
	pool := NewPool(t)
	ctx := context.Background()
	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	migrations := []string{
		"144_source_observation_outbox.sql",
		"145_source_observation_projection_current_index.sql",
		"146_source_observation_job_due_index.sql",
		"147_source_observation_queue_claim_index.sql",
		"148_source_observation_queue_lease_recovery_index.sql",
		"149_source_observation_queue_terminal_retention_index.sql",
		"150_source_observations_subject_time_index.sql",
		"151_source_observations_received_index.sql",
		"152_source_observations_kind_id_index.sql",
		"153_source_observation_collisions_occurred_index.sql",
		"154_source_observation_replay_pending_index.sql",
		"155_youtube_live_reconciliation_due_index.sql",
	}
	if _, err := pool.Exec(ctx, `
		UPDATE observation_contract_generations
		SET current_schema_version = 7,
		    current_generation = 42,
		    updated_by = 'later-migration'
		WHERE provider = 'youtubejs' AND observation_kind = 'community_page'`); err != nil {
		t.Fatalf("bump seeded contract: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		DELETE FROM observation_contract_generations
		WHERE provider = 'holodex' AND observation_kind = 'channel_photo'`); err != nil {
		t.Fatalf("remove contract for missing-row replay check: %v", err)
	}
	for replay := 1; replay <= 2; replay++ {
		for _, migration := range migrations {
			if err := applyMigrationFile(ctx, pool, dir, migration); err != nil {
				t.Fatalf("replay migration %s pass %d: %v", migration, replay, err)
			}
		}
	}
	var contracts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM observation_contract_generations`).Scan(&contracts); err != nil {
		t.Fatalf("count observation contracts: %v", err)
	}
	if contracts != 15 {
		t.Fatalf("observation contract seed count = %d, want 15", contracts)
	}
	var schemaVersion int16
	var generation int64
	var updatedBy string
	if err := pool.QueryRow(ctx, `
		SELECT current_schema_version, current_generation, updated_by
		FROM observation_contract_generations
		WHERE provider = 'youtubejs' AND observation_kind = 'community_page'`).Scan(
		&schemaVersion, &generation, &updatedBy); err != nil {
		t.Fatalf("read bumped observation contract: %v", err)
	}
	if schemaVersion != 7 || generation != 42 || updatedBy != "later-migration" {
		t.Fatalf("replayed migration regressed observation contract: schema=%d generation=%d updated_by=%q", schemaVersion, generation, updatedBy)
	}
}

func TestSourceObservationMigrationGrantsAreLeastPrivilege(t *testing.T) {
	pool := NewPool(t)
	ctx := context.Background()
	roles := createObservationGrantRoles(t, pool)
	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	// #nosec G304 -- 리포 내 마이그레이션 SSOT 디렉터리의 고정 파일명만 읽는다(사용자 입력 아님).
	raw, err := os.ReadFile(filepath.Join(dir, "144_source_observation_outbox.sql"))
	if err != nil {
		t.Fatalf("read 144 source observation migration: %v", err)
	}
	grantPreexistingScraperPrivileges(t, pool, roles.scraper)
	sql := strings.NewReplacer(
		"hololive_scraper", roles.scraper,
		"hololive_runtime", roles.runtime,
	).Replace(string(raw))
	if _, err := pool.Exec(ctx, sql); err != nil {
		t.Fatalf("apply source observation migration with isolated roles: %v", err)
	}
	assertPreexistingScraperPrivilegesRevoked(t, pool, roles.scraper)
	for _, role := range []string{roles.scraper, roles.runtime} {
		quoted := pgx.Identifier{role}.Sanitize()
		if _, err := pool.Exec(ctx, "GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO "+quoted); err != nil {
			t.Fatalf("seed broad table privileges for %s: %v", role, err)
		}
		if _, err := pool.Exec(ctx, "GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO "+quoted); err != nil {
			t.Fatalf("seed broad sequence privileges for %s: %v", role, err)
		}
	}
	if _, err := pool.Exec(ctx, sql); err != nil {
		t.Fatalf("reapply source observation migration after broad grants: %v", err)
	}
	assertPreexistingScraperPrivilegesRevoked(t, pool, roles.scraper)
	assertObservationGrantMatrix(t, pool, roles)
}

type observationGrantRoles struct {
	scraper string
	runtime string
}

func createObservationGrantRoles(t *testing.T, pool *pgxpool.Pool) observationGrantRoles {
	t.Helper()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	roles := observationGrantRoles{
		scraper: "dbtest_observation_scraper_" + suffix,
		runtime: "dbtest_observation_runtime_" + suffix,
	}
	roleNames := []string{roles.scraper, roles.runtime}
	createdRoles := make(map[string]bool, len(roleNames))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, role := range roleNames {
			if !createdRoles[role] {
				continue
			}
			quoted := pgx.Identifier{role}.Sanitize()
			if _, err := pool.Exec(ctx, "DROP OWNED BY "+quoted); err != nil {
				t.Errorf("cleanup observation grant role %s owned privileges: %v", role, err)
			}
			if _, err := pool.Exec(ctx, "DROP ROLE "+quoted); err != nil {
				t.Errorf("cleanup observation grant role %s: %v", role, err)
			}
		}
	})

	ctx := context.Background()
	for _, role := range roleNames {
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", role).Scan(&exists); err != nil {
			t.Fatalf("check isolated observation grant role %s: %v", role, err)
		}
		if exists {
			t.Fatalf("isolated observation grant role %s already exists", role)
		}
		quoted := pgx.Identifier{role}.Sanitize()
		if _, err := pool.Exec(ctx, "CREATE ROLE "+quoted+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT"); err != nil {
			t.Fatalf("create isolated observation grant role %s: %v", role, err)
		}
		createdRoles[role] = true
		if _, err := pool.Exec(ctx, "GRANT USAGE ON SCHEMA public TO "+quoted); err != nil {
			t.Fatalf("grant isolated observation role %s schema usage: %v", role, err)
		}
	}
	return roles
}

var preexistingScraperTables = []string{
	"major_events",
	"major_event_subscriptions",
	"alarms",
	"youtube_notification_outbox",
}

var preexistingScraperSequences = []string{
	"major_events_id_seq",
}

func grantPreexistingScraperPrivileges(t *testing.T, pool *pgxpool.Pool, role string) {
	t.Helper()
	quotedRole := pgx.Identifier{role}.Sanitize()
	for _, statement := range []string{
		"GRANT SELECT, INSERT, UPDATE ON TABLE public.major_events TO " + quotedRole,
		"GRANT USAGE, SELECT ON SEQUENCE public.major_events_id_seq TO " + quotedRole,
		"GRANT SELECT ON TABLE public.major_event_subscriptions TO " + quotedRole,
		"GRANT SELECT ON TABLE public.alarms TO " + quotedRole,
		"GRANT SELECT ON TABLE public.youtube_notification_outbox TO " + quotedRole,
	} {
		if _, err := pool.Exec(context.Background(), statement); err != nil {
			t.Fatalf("seed preexisting scraper privilege with %q: %v", statement, err)
		}
	}
}

func assertPreexistingScraperPrivilegesRevoked(t *testing.T, pool *pgxpool.Pool, role string) {
	t.Helper()
	for _, table := range preexistingScraperTables {
		var exists bool
		if err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", "public."+table).Scan(&exists); err != nil {
			t.Fatalf("check preexisting scraper table %s: %v", table, err)
		}
		if !exists {
			continue
		}
		for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
			var allowed bool
			if err := pool.QueryRow(
				context.Background(),
				"SELECT has_table_privilege($1, $2, $3)",
				role,
				"public."+table,
				privilege,
			).Scan(&allowed); err != nil {
				t.Fatalf("check preexisting scraper privilege %s on %s: %v", privilege, table, err)
			}
			if allowed {
				t.Errorf("preexisting scraper privilege %s on %s remains granted", privilege, table)
			}
		}
	}
	for _, sequence := range preexistingScraperSequences {
		var exists bool
		if err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", "public."+sequence).Scan(&exists); err != nil {
			t.Fatalf("check preexisting scraper sequence %s: %v", sequence, err)
		}
		if !exists {
			continue
		}
		for _, privilege := range []string{"USAGE", "SELECT"} {
			var allowed bool
			if err := pool.QueryRow(
				context.Background(),
				"SELECT has_sequence_privilege($1, $2, $3)",
				role,
				"public."+sequence,
				privilege,
			).Scan(&allowed); err != nil {
				t.Fatalf("check preexisting scraper sequence privilege %s on %s: %v", privilege, sequence, err)
			}
			if allowed {
				t.Errorf("preexisting scraper sequence privilege %s on %s remains granted", privilege, sequence)
			}
		}
	}
}

var sourceObservationTables = []string{
	"observation_contract_generations",
	"youtube_collection_projection_generations",
	"youtube_collection_targets",
	"youtube_collection_target_reasons",
	"youtube_collection_job_leases",
	"source_collection_checkpoints",
	"source_observations",
	"source_observation_queue",
	"source_observation_collisions",
	"source_observation_consumer_offsets",
	"source_observation_replay_requests",
	"source_observation_applications",
	"source_reconciliation_conflicts",
	"youtube_live_reconciliation_heads",
}

var sourceObservationSequences = []string{
	"source_observations_id_seq",
	"source_observation_collisions_id_seq",
	"youtube_collection_projection_generations_generation_seq",
	"source_observation_replay_requests_id_seq",
	"source_observation_applications_id_seq",
	"source_reconciliation_conflicts_id_seq",
}

func assertObservationGrantMatrix(t *testing.T, pool *pgxpool.Pool, roles observationGrantRoles) {
	t.Helper()
	tablePrivileges := map[string]map[string]map[string]bool{
		roles.scraper: {
			"observation_contract_generations":          observationPrivileges("SELECT"),
			"youtube_collection_projection_generations": observationPrivileges("SELECT"),
			"youtube_collection_targets":                observationPrivileges("SELECT"),
			"youtube_collection_job_leases":             observationPrivileges("SELECT", "INSERT", "UPDATE"),
			"source_collection_checkpoints":             observationPrivileges("SELECT", "INSERT", "UPDATE"),
			"source_observations":                       observationPrivileges("SELECT", "INSERT"),
			"source_observation_queue":                  observationPrivileges("SELECT", "INSERT"),
			"source_observation_collisions":             observationPrivileges("INSERT"),
		},
		roles.runtime: {
			"observation_contract_generations":          observationPrivileges("SELECT"),
			"youtube_collection_projection_generations": observationPrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
			"youtube_collection_targets":                observationPrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
			"youtube_collection_target_reasons":         observationPrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
			"source_observations":                       observationPrivileges("SELECT", "DELETE"),
			"source_observation_queue":                  observationPrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
			"source_observation_collisions":             observationPrivileges("SELECT", "DELETE"),
			"source_observation_consumer_offsets":       observationPrivileges("SELECT", "INSERT", "UPDATE"),
			"source_observation_replay_requests":        observationPrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
			"source_observation_applications":           observationPrivileges("SELECT", "INSERT", "DELETE"),
			"source_reconciliation_conflicts":           observationPrivileges("SELECT", "INSERT", "DELETE"),
			"youtube_live_reconciliation_heads":         observationPrivileges("SELECT", "INSERT", "UPDATE"),
		},
	}
	sequencePrivileges := map[string]map[string]map[string]bool{
		roles.scraper: {
			"source_observations_id_seq":           observationPrivileges("USAGE", "SELECT"),
			"source_observation_collisions_id_seq": observationPrivileges("USAGE", "SELECT"),
		},
		roles.runtime: {
			"youtube_collection_projection_generations_generation_seq": observationPrivileges("USAGE", "SELECT"),
			"source_observation_replay_requests_id_seq":                observationPrivileges("USAGE", "SELECT"),
			"source_observation_applications_id_seq":                   observationPrivileges("USAGE", "SELECT"),
			"source_reconciliation_conflicts_id_seq":                   observationPrivileges("USAGE", "SELECT"),
		},
	}

	for role, privilegesByTable := range tablePrivileges {
		for _, table := range sourceObservationTables {
			want := privilegesByTable[table]
			for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
				var got bool
				if err := pool.QueryRow(
					context.Background(),
					"SELECT has_table_privilege($1, $2, $3)",
					role,
					"public."+table,
					privilege,
				).Scan(&got); err != nil {
					t.Fatalf("check %s table privilege %s on %s: %v", role, privilege, table, err)
				}
				if got != want[privilege] {
					t.Errorf("%s table privilege %s on %s = %t, want %t", role, privilege, table, got, want[privilege])
				}
			}
		}
	}

	for role, privilegesBySequence := range sequencePrivileges {
		for _, sequence := range sourceObservationSequences {
			want := privilegesBySequence[sequence]
			for _, privilege := range []string{"USAGE", "SELECT"} {
				var got bool
				if err := pool.QueryRow(
					context.Background(),
					"SELECT has_sequence_privilege($1, $2, $3)",
					role,
					"public."+sequence,
					privilege,
				).Scan(&got); err != nil {
					t.Fatalf("check %s sequence privilege %s on %s: %v", role, privilege, sequence, err)
				}
				if got != want[privilege] {
					t.Errorf("%s sequence privilege %s on %s = %t, want %t", role, privilege, sequence, got, want[privilege])
				}
			}
		}
	}
}

func observationPrivileges(privileges ...string) map[string]bool {
	result := make(map[string]bool, len(privileges))
	for _, privilege := range privileges {
		result[privilege] = true
	}
	return result
}
