package dbtest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/sqlsplit"
)

// reloptions는 schema_snapshot.golden.sql 직렬화(컬럼·제약·인덱스)에 포함되지 않아
// 111의 autovacuum 튜닝은 골든 대신 이 구조 테스트로 고정한다.
func TestTelemetryHotTableAutovacuumTuned(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

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

var sourceObservationReplayMigrations = []string{
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
	"156_source_observation_lock_api.sql",
	"157_source_observations_kind_received_index.sql",
	"158_source_observation_collision_fk_index.sql",
	"159_source_observation_replay_fk_index.sql",
	"160_youtube_live_reconciliation_candidate_fk_index.sql",
	"161_source_observation_subject_heads.sql",
	"162_youtube_content_evidence_clocks.sql",
	"163_youtube_live_viewer_schedule_canonical.sql",
	"191_source_observation_replay_epoch.sql",
	"192_live_reconciliation_evidence.sql",
	"193_live_absence_slot_channel_index.sql",
	"218_live_absence_evidence_contract.sql",
}

func TestSourceObservationMigrationReplaysWithoutRegressingContracts(t *testing.T) {
	pool, _ := channelStatisticsRemovalPool(t)
	ctx := t.Context()

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
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
		for _, migration := range sourceObservationReplayMigrations {
			if err := applyMigrationFile(ctx, pool, dir, migration); err != nil {
				t.Fatalf("replay migration %s pass %d: %v", migration, replay, err)
			}
		}
	}

	assertObservationContractsSurvivedReplay(t, pool)
}

func assertObservationContractsSurvivedReplay(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := t.Context()

	var (
		schemaVersion int16
		generation    int64
		updatedBy     string
	)

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
	pool, _ := channelStatisticsRemovalPool(t)
	ctx := t.Context()
	roles := createObservationGrantRoles(t, pool)

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}

	grantPreexistingScraperPrivileges(t, pool, roles.scraper)

	sql := strings.NewReplacer(
		"hololive_scraper", roles.scraper,
		"hololive_runtime", roles.runtime,
	).Replace(readObservationGrantMigrations(t, dir))
	if _, err := pool.Exec(ctx, sql); err != nil {
		t.Fatalf("apply source observation migration with isolated roles: %v", err)
	}

	assertPreexistingScraperPrivilegesRevoked(t, pool, roles.scraper)
	grantBroadObservationPrivileges(t, pool, roles)

	if _, err := pool.Exec(ctx, sql); err != nil {
		t.Fatalf("reapply source observation migration after broad grants: %v", err)
	}

	assertPreexistingScraperPrivilegesRevoked(t, pool, roles.scraper)

	// 234 이전 기준선 위에 실제 collector·API 쿼리가 요구하는 최소 현재 schema만 올린다. 259는 156의
	// 이전 lock 함수를 지우므로 144~218 묶음 재적용 뒤에 두고, 재적용 안전성을 위해 두 번 적용한다.
	for _, filename := range observationGrantPostBaselineMigrations {
		raw, readErr := fs.ReadFile(os.DirFS(dir), filename)
		if readErr != nil {
			t.Fatal(readErr)
		}

		content := strings.NewReplacer("hololive_scraper", roles.scraper, "hololive_runtime", roles.runtime).Replace(string(raw))
		if err := applyMigrationContent(ctx, pool, filename, content); err != nil {
			t.Fatalf("apply post-baseline privilege migration %s: %v", filename, err)
		}
	}

	assertObservationGrantMatrix(t, pool, roles)
	assertObservationLockAPIAccess(t, pool, roles)
	assertObservationRetentionAPIAccess(t, pool, roles)
	assertScheduleCollaboConstraintAccess(t, pool, roles)
	assertCollectionMembershipAPIAccess(t, pool, roles)
}

var observationGrantPostBaselineMigrations = []string{
	"238_source_observation_payload_prepare.sql",
	"239_source_observation_payload_index.sql",
	"240_source_observation_payload_backfill_index.sql",
	"241_source_observation_payload_cutover.sql",
	"242_drop_payload_backfill_index.sql",
	collectionMembershipMigration,
	collectionMembershipMigration,
}

// 이어 붙이는 순서가 grant/revoke 결과를 결정하므로 파일 번호순이 아니라 적용 순서대로 나열한다.
var observationGrantMigrationFiles = []string{
	"144_source_observation_outbox.sql",
	"156_source_observation_lock_api.sql",
	"161_source_observation_subject_heads.sql",
	"162_youtube_content_evidence_clocks.sql",
	"163_youtube_live_viewer_schedule_canonical.sql",
	"174_youtube_projection_retention_grant.sql",
	"175_youtube_runtime_lock_retention_api.sql",
	"187_youtube_dependent_retention_api.sql",
	"176_youtube_projection_retention_revoke_delete.sql",
	"178_youtube_schedule_collabo_talent_names.sql",
	"191_source_observation_replay_epoch.sql",
	"192_live_reconciliation_evidence.sql",
	"218_live_absence_evidence_contract.sql",
}

func readObservationGrantMigrations(t *testing.T, dir string) string {
	t.Helper()

	migrations := os.DirFS(dir)
	parts := make([]string, 0, len(observationGrantMigrationFiles))

	for _, name := range observationGrantMigrationFiles {
		raw, err := fs.ReadFile(migrations, name)
		if err != nil {
			t.Fatalf("read %s observation grant migration: %v", name, err)
		}

		parts = append(parts, string(raw))
	}

	return strings.Join(parts, "\n")
}

func grantBroadObservationPrivileges(t *testing.T, pool *pgxpool.Pool, roles observationGrantRoles) {
	t.Helper()

	ctx := t.Context()

	for _, role := range []string{roles.scraper, roles.runtime} {
		quoted := pgx.Identifier{role}.Sanitize()
		if _, err := pool.Exec(ctx, "GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO "+quoted); err != nil {
			t.Fatalf("seed broad table privileges for %s: %v", role, err)
		}

		if _, err := pool.Exec(ctx, "GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO "+quoted); err != nil {
			t.Fatalf("seed broad sequence privileges for %s: %v", role, err)
		}
	}
}

func TestSourceObservationLockAPIMigrationIsAtomic(t *testing.T) {
	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}

	// #nosec G304 -- 리포 내 마이그레이션 SSOT 디렉터리의 고정 파일명만 읽는다(사용자 입력 아님).
	raw, err := os.ReadFile(filepath.Join(dir, "156_source_observation_lock_api.sql"))
	if err != nil {
		t.Fatalf("read 156 source observation lock API migration: %v", err)
	}

	segments, err := sqlsplit.Segments(string(raw))
	if err != nil {
		t.Fatalf("parse 156 source observation lock API migration: %v", err)
	}

	if len(segments) != 1 || !segments[0].Transactional {
		t.Fatalf("156 migration segments = %#v, want one transactional segment", segments)
	}
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
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
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

	ctx := t.Context()

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
		if _, err := pool.Exec(t.Context(), statement); err != nil {
			t.Fatalf("seed preexisting scraper privilege with %q: %v", statement, err)
		}
	}
}

func assertPreexistingScraperPrivilegesRevoked(t *testing.T, pool *pgxpool.Pool, role string) {
	t.Helper()
	assertPreexistingTablePrivilegesRevoked(t, pool, role)
	assertPreexistingSequencePrivilegesRevoked(t, pool, role)
}

func assertPreexistingTablePrivilegesRevoked(t *testing.T, pool *pgxpool.Pool, role string) {
	t.Helper()

	for _, table := range preexistingScraperTables {
		var exists bool

		if err := pool.QueryRow(t.Context(), "SELECT to_regclass($1) IS NOT NULL", "public."+table).Scan(&exists); err != nil {
			t.Fatalf("check preexisting scraper table %s: %v", table, err)
		}

		if !exists {
			continue
		}

		for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
			var allowed bool

			if err := pool.QueryRow(
				t.Context(),
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
}

func assertPreexistingSequencePrivilegesRevoked(t *testing.T, pool *pgxpool.Pool, role string) {
	t.Helper()

	for _, sequence := range preexistingScraperSequences {
		var exists bool

		if err := pool.QueryRow(t.Context(), "SELECT to_regclass($1) IS NOT NULL", "public."+sequence).Scan(&exists); err != nil {
			t.Fatalf("check preexisting scraper sequence %s: %v", sequence, err)
		}

		if !exists {
			continue
		}

		for _, privilege := range []string{"USAGE", "SELECT"} {
			var allowed bool

			if err := pool.QueryRow(
				t.Context(),
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
	"youtube_collection_projection_guard",
	"youtube_collection_target_reasons",
	"youtube_collection_job_leases",
	"source_collection_checkpoints",
	"source_observations",
	"source_observation_payloads",
	"source_observation_payload_gc_state",
	"source_observation_queue",
	"source_observation_collisions",
	"source_observation_consumer_offsets",
	"source_observation_replay_requests",
	"source_observation_replay_epoch",
	"source_observation_applications",
	"source_observation_subject_heads",
	"source_reconciliation_conflicts",
	"youtube_live_reconciliation_heads",
	"youtube_live_pending_ends",
	"youtube_live_absence_slots",
	"youtube_content_evidence_clocks",
	"youtube_content_absence_slots",
	"youtube_content_channel_heads",
	"youtube_live_viewer_sample_evidence",
	"youtube_live_viewer_sample_heads",
	"youtube_schedule_items",
	"youtube_channel_live_checks",
	"youtube_video_availability",
}

var sourceObservationSequences = []string{
	"source_observations_id_seq",
	"source_observation_payloads_id_seq",
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
			"source_observation_payloads":               observationPrivileges("SELECT", "INSERT"),
			"source_observation_queue":                  observationPrivileges("SELECT", "INSERT"),
			"source_observation_collisions":             observationPrivileges("INSERT"),
		},
		roles.runtime: {
			"observation_contract_generations":          observationPrivileges("SELECT"),
			"youtube_collection_projection_generations": observationPrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
			"youtube_collection_targets":                observationPrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
			"youtube_collection_projection_guard":       observationPrivileges("SELECT", "UPDATE"),
			"youtube_collection_target_reasons":         observationPrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
			"youtube_collection_job_leases":             observationPrivileges("SELECT"),
			"source_observations":                       observationPrivileges("SELECT", "DELETE"),
			"source_observation_payloads":               observationPrivileges("SELECT"),
			"source_observation_queue":                  observationPrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
			"source_observation_collisions":             observationPrivileges("SELECT", "DELETE"),
			"source_observation_consumer_offsets":       observationPrivileges("SELECT", "INSERT", "UPDATE"),
			"source_observation_replay_requests":        observationPrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
			"source_observation_replay_epoch":           observationPrivileges("SELECT", "INSERT"),
			"source_observation_applications":           observationPrivileges("SELECT", "INSERT", "DELETE"),
			"source_observation_subject_heads":          observationPrivileges("SELECT", "INSERT", "UPDATE"),
			"source_reconciliation_conflicts":           observationPrivileges("SELECT", "INSERT", "DELETE"),
			"youtube_live_reconciliation_heads":         observationPrivileges("SELECT", "INSERT", "UPDATE"),
			"youtube_live_pending_ends":                 observationPrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
			"youtube_live_absence_slots":                observationPrivileges("SELECT", "INSERT"),
			"youtube_content_evidence_clocks":           observationPrivileges("SELECT", "INSERT", "UPDATE"),
			"youtube_content_absence_slots":             observationPrivileges("SELECT", "INSERT", "UPDATE"),
			"youtube_content_channel_heads":             observationPrivileges("SELECT", "INSERT", "UPDATE"),
			"youtube_live_viewer_sample_evidence":       observationPrivileges("SELECT", "INSERT", "UPDATE"),
			"youtube_live_viewer_sample_heads":          observationPrivileges("SELECT", "INSERT", "UPDATE"),
			"youtube_schedule_items":                    observationPrivileges("SELECT", "INSERT", "UPDATE"),
			"youtube_live_sessions":                     observationPrivileges("SELECT", "INSERT", "UPDATE"),
			"youtube_live_viewer_samples":               observationPrivileges("SELECT", "INSERT", "UPDATE", "DELETE"),
			"youtube_channel_live_checks":               observationPrivileges("SELECT", "INSERT", "UPDATE"),
			"youtube_video_availability":                observationPrivileges("SELECT", "INSERT", "UPDATE"),
		},
	}
	sequencePrivileges := map[string]map[string]map[string]bool{
		roles.scraper: {
			"source_observations_id_seq":           observationPrivileges("USAGE", "SELECT"),
			"source_observation_payloads_id_seq":   observationPrivileges("USAGE", "SELECT"),
			"source_observation_collisions_id_seq": observationPrivileges("USAGE", "SELECT"),
		},
		roles.runtime: {
			"youtube_collection_projection_generations_generation_seq": observationPrivileges("USAGE", "SELECT"),
			"source_observation_replay_requests_id_seq":                observationPrivileges("USAGE", "SELECT"),
			"source_observation_applications_id_seq":                   observationPrivileges("USAGE", "SELECT"),
			"source_reconciliation_conflicts_id_seq":                   observationPrivileges("USAGE", "SELECT"),
		},
	}

	assertObservationTableGrants(t, pool, tablePrivileges)
	assertObservationSequenceGrants(t, pool, sequencePrivileges)
}

func assertObservationTableGrants(t *testing.T, pool *pgxpool.Pool, rolePrivileges map[string]map[string]map[string]bool) {
	t.Helper()

	for role, privilegesByTable := range rolePrivileges {
		for _, table := range sourceObservationTables {
			want := privilegesByTable[table]

			for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
				var got bool

				if err := pool.QueryRow(
					t.Context(),
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
}

func assertObservationSequenceGrants(t *testing.T, pool *pgxpool.Pool, rolePrivileges map[string]map[string]map[string]bool) {
	t.Helper()

	for role, privilegesBySequence := range rolePrivileges {
		for _, sequence := range sourceObservationSequences {
			want := privilegesBySequence[sequence]

			for _, privilege := range []string{"USAGE", "SELECT"} {
				var got bool

				if err := pool.QueryRow(
					t.Context(),
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

func assertObservationLockAPIAccess(t *testing.T, pool *pgxpool.Pool, roles observationGrantRoles) {
	t.Helper()

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatalf("resolve migrations dir for observation lock API check: %v", err)
	}

	// 발행 SQL은 collector, 소비 SQL은 API의 sourceobservation 패키지가 소유한다.
	publishQueryDir := filepath.Clean(filepath.Join(dir, "..", "..", "..", "hololive-youtube-collector", "internal", "runtime", "sourceobservation", "queries"))
	consumeQueryDir := filepath.Clean(filepath.Join(dir, "..", "..", "internal", "youtube", "sourceobservation", "queries"))
	roleSQLPath := filepath.Clean(filepath.Join(dir, "..", "..", "..", "hololive-dbtest", "testdata", "queries", "set_local_role.sql"))
	checks := map[string]struct {
		dir     string
		queries []observationRoleQuery
	}{
		roles.scraper: {dir: publishQueryDir, queries: []observationRoleQuery{
			{name: "repository_projection_current_0002_02.sql"},
			{name: "repository_contract_batch_current_0031_31.sql", args: []any{fmt.Sprintf(
				`[{"provider":"youtubejs","observation_kind":%q,"schema_version":1,"contract_generation":1}]`, communityPageKind,
			)}},
			{name: "repository_publish_set_0032_32.sql", args: []any{"[]"}},
		}},
		roles.runtime: {dir: consumeQueryDir, queries: []observationRoleQuery{
			{name: "repository_replay_epoch_activate_0085_85.sql", args: []any{"grant-test", "verify replay epoch runtime grant"}},
			{name: "repository_replay_epoch_load_0086_86.sql"},
			{name: "repository_replay_observation_0020_20.sql", args: []any{int64(0)}},
			{name: "repository_claim_lock_0013_13.sql", args: []any{int64(0), strings.Repeat("0", 64)}},
			{name: "repository_live_pending_ends.sql", args: []any{[]string{}}},
			{name: "repository_live_absence_slots.sql", args: []any{[]string{}, nil, time.Now().UTC(), []string{}}},
		}},
	}

	for role, check := range checks {
		runObservationRoleQueries(t, pool, role, roleSQLPath, check.dir, check.queries)
	}
}

// runObservationRoleQueries는 쿼리 자산을 모두 읽은 뒤 role 하나로 SET LOCAL ROLE한 트랜잭션에서
// 차례로 실행하고 항상 롤백한다. 검사 목적이 권한이므로 어떤 행도 남기지 않는다. 파일 읽기는 Begin 앞에
// 둔다. 트랜잭션을 연 채 Fatalf로 빠져나가면 연결이 pool에 돌아가지 않아 cleanup의 pool.Close가
// go test timeout까지 멈추기 때문이다.
func runObservationRoleQueries(
	t *testing.T,
	pool *pgxpool.Pool,
	role, roleSQLPath, queryDir string,
	queries []observationRoleQuery,
) {
	t.Helper()

	quoted := pgx.Identifier{role}.Sanitize()
	roleSQL := readObservationRoleSQL(t, roleSQLPath, map[string]string{"__ROLE__": quoted})
	querySQL := make([]string, len(queries))

	for i, check := range queries {
		querySQL[i] = readObservationRoleSQL(t, filepath.Join(queryDir, check.name), nil)
	}

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin observation lock API check for %s: %v", role, err)
	}

	if _, err := tx.Exec(t.Context(), roleSQL); err != nil {
		rollbackObservationRoleTx(t, tx, role, "role setup")
		t.Fatalf("set observation lock API role %s: %v", role, err)
	}

	for i, check := range queries {
		if err := execObservationRoleQuery(t.Context(), tx, querySQL[i], check.args); err != nil {
			rollbackObservationRoleTx(t, tx, role, "query "+check.name)
			t.Fatalf("execute observation lock API query %s as %s: %v", check.name, role, err)
		}
	}

	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatalf("rollback observation lock API check for %s: %v", role, err)
	}
}

// execObservationRoleQuery는 Close 뒤 Err()까지 본다. Err()를 확인하지 않으면 pgx가 그쪽으로
// 넘긴 권한 오류가 다음 쿼리의 25P02로 둔갑해 원인 쿼리를 잃고, 목록 마지막 쿼리의 오류는 조용히
// 통과한다.
func execObservationRoleQuery(ctx context.Context, tx pgx.Tx, query string, args []any) error {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("start observation role query: %w", err)
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		return fmt.Errorf("read observation role query result: %w", err)
	}

	return nil
}

func rollbackObservationRoleTx(t *testing.T, tx pgx.Tx, role, stage string) {
	t.Helper()

	if err := tx.Rollback(t.Context()); err != nil {
		t.Errorf("rollback observation lock API %s for %s: %v", stage, role, err)
	}
}

func assertObservationRetentionAPIAccess(t *testing.T, pool *pgxpool.Pool, roles observationGrantRoles) {
	t.Helper()

	functions := []string{
		"public.delete_retired_youtube_collection_job_leases(timestamp with time zone,integer)",
		"public.delete_source_observation_retention_batch(text[],timestamp with time zone[],integer)",
		"public.delete_source_observation_application_retention_batch(text[],timestamp with time zone[],integer)",
		"public.delete_source_collection_checkpoint_retention_batch(timestamp with time zone,integer)",
		"public.delete_source_observation_payload_batch(timestamp with time zone,integer)",
	}

	for role, want := range map[string]bool{roles.scraper: false, roles.runtime: true} {
		for _, function := range functions {
			var got bool

			if err := pool.QueryRow(
				t.Context(),
				"SELECT has_function_privilege($1, $2, 'EXECUTE')",
				role,
				function,
			).Scan(&got); err != nil {
				t.Fatalf("check retention function %s privilege for %s: %v", function, role, err)
			}

			if got != want {
				t.Errorf("retention function %s privilege for %s = %t, want %t", function, role, got, want)
			}
		}
	}

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin runtime retention API check: %v", err)
	}

	if _, err := tx.Exec(t.Context(), "SET LOCAL ROLE "+pgx.Identifier{roles.runtime}.Sanitize()); err != nil {
		if rollbackErr := tx.Rollback(t.Context()); rollbackErr != nil {
			t.Errorf("rollback runtime retention role setup: %v", rollbackErr)
		}

		t.Fatalf("set runtime retention role: %v", err)
	}

	for _, query := range []string{
		"SELECT * FROM public.delete_retired_youtube_collection_job_leases(clock_timestamp(), 1)",
		"SELECT * FROM public.delete_source_observation_retention_batch(ARRAY['community_page']::text[], ARRAY[clock_timestamp()]::timestamptz[], 1)",
		"SELECT * FROM public.delete_source_observation_application_retention_batch(ARRAY['community_page']::text[], ARRAY[clock_timestamp()]::timestamptz[], 1)",
		"SELECT * FROM public.delete_source_collection_checkpoint_retention_batch(clock_timestamp(), 1)",
		"SELECT public.delete_source_observation_payload_batch(clock_timestamp(), 1)",
	} {
		rows, queryErr := tx.Query(t.Context(), query)
		if queryErr != nil {
			if rollbackErr := tx.Rollback(t.Context()); rollbackErr != nil {
				t.Errorf("rollback runtime retention API query: %v", rollbackErr)
			}

			t.Fatalf("execute runtime retention API query: %v", queryErr)
		}

		rows.Close()
	}

	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatalf("rollback runtime retention API check: %v", err)
	}
}

func assertScheduleCollaboConstraintAccess(t *testing.T, pool *pgxpool.Pool, roles observationGrantRoles) {
	t.Helper()

	function := "public.youtube_schedule_collabo_talent_names_valid(text[])"

	for role, want := range map[string]bool{roles.scraper: false, roles.runtime: true} {
		var got bool

		if err := pool.QueryRow(
			t.Context(),
			"SELECT has_function_privilege($1, $2, 'EXECUTE')",
			role,
			function,
		).Scan(&got); err != nil {
			t.Fatalf("check schedule collabo constraint function privilege for %s: %v", role, err)
		}

		if got != want {
			t.Errorf("schedule collabo constraint function privilege for %s = %t, want %t", role, got, want)
		}
	}

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin schedule collabo constraint runtime check: %v", err)
	}

	if _, err := tx.Exec(t.Context(), "SET LOCAL ROLE "+pgx.Identifier{roles.runtime}.Sanitize()); err != nil {
		if rollbackErr := tx.Rollback(t.Context()); rollbackErr != nil {
			t.Errorf("rollback schedule collabo constraint role setup: %v", rollbackErr)
		}

		t.Fatalf("set schedule collabo constraint runtime role: %v", err)
	}

	if _, err := tx.Exec(t.Context(), `
		INSERT INTO public.youtube_schedule_items(
			group_key, provider, external_id, title, scheduled_at, collabo_talent_names
		) VALUES ('grant-check', 'hololive_official', 'grant-check', 'grant-check', clock_timestamp(), ARRAY['Guest'])
	`); err != nil {
		if rollbackErr := tx.Rollback(t.Context()); rollbackErr != nil {
			t.Errorf("rollback schedule collabo constraint runtime insert: %v", rollbackErr)
		}

		t.Fatalf("insert schedule item through collabo constraint as runtime role: %v", err)
	}

	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatalf("rollback schedule collabo constraint runtime check: %v", err)
	}
}

type observationRoleQuery struct {
	name string
	args []any
}

func readObservationRoleSQL(t *testing.T, path string, replacements map[string]string) string {
	t.Helper()

	// #nosec G304 -- 리포 내부의 고정 SQL 자산 경로만 호출자가 전달한다.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read observation role SQL %s: %v", filepath.Base(path), err)
	}

	query := string(raw)

	for old, replacement := range replacements {
		query = strings.ReplaceAll(query, old, replacement)
	}

	return query
}

func observationPrivileges(privileges ...string) map[string]bool {
	result := make(map[string]bool, len(privileges))
	for _, privilege := range privileges {
		result[privilege] = true
	}

	return result
}

const (
	membershipGrantSubject = "UCgrantcheck0000000000ab"
	membershipGrantOwner   = "grant-check-collector"
)

// assertCollectionMembershipAPIAccess는 259의 guard·membership 경로를 실제 collector·API 쿼리로 실행한다.
// 이때 scraper는 guard 테이블 권한 없이 SECURITY DEFINER lock 함수로만 공유 잠금하고 membership을 판정하며,
// runtime은 refresh의 guard FOR UPDATE만 할 수 있고 guard row 변경이나 collector 함수 실행은 거부돼야 한다.
func assertCollectionMembershipAPIAccess(t *testing.T, pool *pgxpool.Pool, roles observationGrantRoles) {
	t.Helper()

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatalf("resolve migrations dir for collection membership check: %v", err)
	}

	collectorQueryDir := filepath.Clean(filepath.Join(dir, "..", "..", "..", "hololive-youtube-collector", "internal", "runtime", "sourceobservation", "queries"))
	refreshQueryDir := filepath.Clean(filepath.Join(dir, "..", "..", "internal", "planes", "youtube", "targetprojection", "queries"))
	roleSQLPath := filepath.Clean(filepath.Join(dir, "..", "..", "..", "hololive-dbtest", "testdata", "queries", "set_local_role.sql"))

	for role, want := range map[string]bool{roles.scraper: true, roles.runtime: false} {
		for _, function := range []string{
			"public.lock_current_youtube_collection_projection()",
			"public.youtube_collection_membership_valid(text[],boolean,text,integer,bigint)",
		} {
			var got bool

			if err := pool.QueryRow(t.Context(), "SELECT has_function_privilege($1, $2, 'EXECUTE')", role, function).Scan(&got); err != nil {
				t.Fatalf("check collection membership function %s privilege for %s: %v", function, role, err)
			}

			if got != want {
				t.Errorf("collection membership function %s privilege for %s = %t, want %t", function, role, got, want)
			}
		}
	}

	assertScraperCollectionMembership(t, pool, roles.scraper, roleSQLPath, collectorQueryDir)
	assertRuntimeProjectionGuardAccess(t, pool, roles.runtime, roleSQLPath, refreshQueryDir)
}

// assertScraperCollectionMembership은 acquire가 기록하는 범위·수를 가진 ACTIVE lease를 소유자 권한으로
// 시드한 뒤 scraper로 실제 lock·membership 쿼리를 실행한다. 시드는 같은 트랜잭션과 함께 롤백된다.
func assertScraperCollectionMembership(t *testing.T, pool *pgxpool.Pool, role, roleSQLPath, queryDir string) {
	t.Helper()

	ctx := t.Context()
	roleSQL := readObservationRoleSQL(t, roleSQLPath, map[string]string{"__ROLE__": pgx.Identifier{role}.Sanitize()})
	lockSQL := readObservationRoleSQL(t, filepath.Join(queryDir, "repository_projection_current_0002_02.sql"), nil)
	membershipSQL := readObservationRoleSQL(t, filepath.Join(queryDir, "repository_lease_membership_0004_04.sql"), nil)
	kinds := []string{"shorts_list", "video_list"}
	scheduledFor := time.Date(2026, time.October, 3, 0, 0, 0, 0, time.UTC)
	jobKey := "collector:youtubejs:youtubejs_content:" + membershipGrantSubject

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin scraper collection membership check: %v", err)
	}

	generation := seedScraperCollectionMembership(t, tx, role, jobKey, kinds, scheduledFor)

	if _, err := tx.Exec(ctx, roleSQL); err != nil {
		failObservationRoleTx(t, tx, role, "set scraper collection membership role: %v", err)
	}

	var locked int64

	if err := tx.QueryRow(ctx, lockSQL).Scan(&locked); err != nil {
		failObservationRoleTx(t, tx, role, "lock CURRENT projection as scraper: %v", err)
	}

	if locked != generation {
		failObservationRoleTx(t, tx, role, "scraper locked projection generation = %d, want CURRENT %d", locked, generation)
	}

	var projectionCurrent, membershipValid bool

	if err := tx.QueryRow(ctx, membershipSQL,
		jobKey, membershipGrantOwner, int64(1), generation, scheduledFor, kinds, true,
	).Scan(&projectionCurrent, &membershipValid); err != nil {
		failObservationRoleTx(t, tx, role, "check lease membership as scraper: %v", err)
	}

	if !projectionCurrent || !membershipValid {
		failObservationRoleTx(t, tx, role, "scraper lease membership = (current %t, valid %t), want both true", projectionCurrent, membershipValid)
	}

	assertScraperProjectionGuardDenied(t, tx, role)

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback scraper collection membership check: %v", err)
	}
}

// seedScraperCollectionMembership은 소유자 권한으로 CURRENT projection, 두 kind의 target, acquire와 같은
// 범위·수를 기록한 ACTIVE lease를 시드하고 CURRENT generation을 돌려준다.
func seedScraperCollectionMembership(t *testing.T, tx pgx.Tx, role, jobKey string, kinds []string, scheduledFor time.Time) int64 {
	t.Helper()

	ctx := t.Context()

	var generation int64

	if err := tx.QueryRow(ctx, `
		INSERT INTO youtube_collection_projection_generations(status, row_count, projection_sha256, valid_until, activated_at)
		VALUES ('CURRENT', 2, repeat('d', 64), clock_timestamp() + interval '1 hour', clock_timestamp())
		RETURNING generation`).Scan(&generation); err != nil {
		failObservationRoleTx(t, tx, role, "seed CURRENT collection projection: %v", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO youtube_collection_targets(projection_generation, subject_key, observation_kind, priority,
			poll_interval_ms, enabled, valid_until, member_since_generation)
		SELECT $1, $2, kind, 40, 600000, TRUE, clock_timestamp() + interval '1 hour', $1
		FROM unnest($3::text[]) AS kind`, generation, membershipGrantSubject, kinds); err != nil {
		failObservationRoleTx(t, tx, role, "seed collection membership targets: %v", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO youtube_collection_job_leases(job_key, provider, job_class, collection_job_kind, subject_key,
			projection_generation, poll_interval_ms, slot_state, scheduled_for, next_due_at, fence_epoch,
			owner_instance, lease_expires_at, membership_kinds, membership_exact_subject, membership_target_count)
		VALUES ($1, 'youtubejs', 'SUBJECT', 'youtubejs_content', $2, $3, 600000, 'ACTIVE', $4, $4, 1,
			$5, clock_timestamp() + interval '1 hour', $6::text[], TRUE, cardinality($6::text[]))`,
		jobKey, membershipGrantSubject, generation, scheduledFor, membershipGrantOwner, kinds); err != nil {
		failObservationRoleTx(t, tx, role, "seed scoped collection lease: %v", err)
	}

	return generation
}

// assertScraperProjectionGuardDenied는 scraper 역할이 guard row를 직접 잠그거나 지우지 못하는지 확인한다.
// 이 guard row는 lock 함수로만 공유 잠금한다. 직접 잠금·삭제 권한이 있으면 refresh의 배타 잠금을 막거나
// guard를 지워 collector 전체를 멈출 수 있다.
func assertScraperProjectionGuardDenied(t *testing.T, tx pgx.Tx, role string) {
	t.Helper()

	for _, query := range []string{
		"SELECT guard_key FROM youtube_collection_projection_guard WHERE guard_key FOR SHARE",
		"DELETE FROM youtube_collection_projection_guard",
	} {
		if err := expectObservationPermissionDenied(t.Context(), tx, query); err != nil {
			failObservationRoleTx(t, tx, role, "scraper guard access: %v", err)
		}
	}
}

// assertRuntimeProjectionGuardAccess는 API refresh의 실제 guard 잠금 쿼리가 runtime으로 성공하고,
// guard row 추가·삭제와 collector 전용 함수 실행은 권한 오류로 거부되는지 확인한다.
func assertRuntimeProjectionGuardAccess(t *testing.T, pool *pgxpool.Pool, role, roleSQLPath, queryDir string) {
	t.Helper()

	ctx := t.Context()
	roleSQL := readObservationRoleSQL(t, roleSQLPath, map[string]string{"__ROLE__": pgx.Identifier{role}.Sanitize()})
	refreshSQL := readObservationRoleSQL(t, filepath.Join(queryDir, "lock_projection_guard.sql"), nil)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin runtime projection guard check: %v", err)
	}

	if _, err := tx.Exec(ctx, roleSQL); err != nil {
		failObservationRoleTx(t, tx, role, "set runtime projection guard role: %v", err)
	}

	var guard bool

	if err := tx.QueryRow(ctx, refreshSQL).Scan(&guard); err != nil {
		failObservationRoleTx(t, tx, role, "lock projection guard as runtime: %v", err)
	}

	if !guard {
		failObservationRoleTx(t, tx, role, "runtime locked guard_key = false, want singleton true row")
	}

	for _, query := range []string{
		"DELETE FROM youtube_collection_projection_guard",
		"INSERT INTO youtube_collection_projection_guard(guard_key) VALUES (TRUE) ON CONFLICT DO NOTHING",
		"SELECT generation FROM lock_current_youtube_collection_projection()",
		"SELECT youtube_collection_membership_valid(ARRAY['video_list']::text[], TRUE, 'UCgrantcheck0000000000ab', 1, 1)",
	} {
		if err := expectObservationPermissionDenied(ctx, tx, query); err != nil {
			failObservationRoleTx(t, tx, role, "runtime guard access: %v", err)
		}
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback runtime projection guard check: %v", err)
	}
}

// expectObservationPermissionDenied는 savepoint 안에서 query를 실행해 insufficient_privilege(42501)만
// 성공으로 본다. 다른 오류나 성공은 권한 경계가 아니라 다른 원인이므로 실패로 돌려준다.
func expectObservationPermissionDenied(ctx context.Context, tx pgx.Tx, query string) error {
	if _, err := tx.Exec(ctx, "SAVEPOINT observation_permission_check"); err != nil {
		return fmt.Errorf("open permission savepoint: %w", err)
	}

	_, execErr := tx.Exec(ctx, query)

	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT observation_permission_check"); err != nil {
		return fmt.Errorf("rollback permission savepoint: %w", err)
	}

	if execErr == nil {
		return fmt.Errorf("query %q succeeded, want insufficient_privilege", query)
	}

	if pgErr, ok := errors.AsType[*pgconn.PgError](execErr); !ok || pgErr.Code != "42501" {
		return fmt.Errorf("query %q error = %w, want insufficient_privilege", query, execErr)
	}

	return nil
}

// failObservationRoleTx는 열린 트랜잭션을 먼저 롤백해 연결을 pool에 돌려준 뒤 테스트를 멈춘다.
func failObservationRoleTx(t *testing.T, tx pgx.Tx, role, format string, args ...any) {
	t.Helper()
	rollbackObservationRoleTx(t, tx, role, "failure")
	t.Fatalf(format, args...)
}
