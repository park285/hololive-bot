package dbtest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	liveAbsenceEvidenceMigration = "218_live_absence_evidence_contract.sql"
	channelLiveCheckKind         = "channel_live_check"
	videoLiveCheckKind           = "video_live_check"
	sqlStateCheckViolation       = "23514"
	sqlStateUniqueViolation      = "23505"
	liveCheckUnknown             = "UNKNOWN"
	methodUnknown                = "unknown"
)

// 218 이전 허용 목록으로 되돌리고, consumer offset은 첫 단계(NOT VALID 추가) 뒤 중단된 상태로 둔다.
// 이미 적용된 DB에서 이전 상태를 재현해 업그레이드와 중단 재개 경로를 검증한다.
const restorePreLiveAbsenceKindVocabSQL = `
ALTER TABLE public.observation_contract_generations
	DROP CONSTRAINT chk_observation_contract_kind_vocab;
ALTER TABLE public.observation_contract_generations
	ADD CONSTRAINT chk_observation_contract_kind_vocab CHECK (observation_kind IN (
		'community_page', 'video_list', 'shorts_list', 'live_snapshot', 'viewer_sample',
		'channel_stats', 'channel_profile', 'channel_photo', 'schedule_snapshot'));
ALTER TABLE public.youtube_collection_targets
	DROP CONSTRAINT chk_youtube_collection_target_kind_vocab;
ALTER TABLE public.youtube_collection_targets
	ADD CONSTRAINT chk_youtube_collection_target_kind_vocab CHECK (observation_kind IN (
		'community_page', 'video_list', 'shorts_list', 'live_snapshot', 'viewer_sample',
		'channel_stats', 'channel_profile', 'channel_photo', 'schedule_snapshot'));
ALTER TABLE public.source_observation_consumer_offsets
	DROP CONSTRAINT chk_source_observation_consumer_offset_kind_vocab;
ALTER TABLE public.source_observation_consumer_offsets
	ADD CONSTRAINT chk_source_observation_consumer_offset_kind_vocab CHECK (observation_kind IN (
		'community_page', 'video_list', 'shorts_list', 'live_snapshot', 'viewer_sample',
		'channel_stats', 'channel_profile', 'channel_photo', 'schedule_snapshot'));
ALTER TABLE public.source_observation_consumer_offsets
	ADD CONSTRAINT chk_source_observation_consumer_offset_kind_vocab_next CHECK (observation_kind IN (
		'community_page', 'video_list', 'shorts_list', 'live_snapshot', 'viewer_sample',
		'channel_stats', 'channel_profile', 'channel_photo', 'schedule_snapshot',
		'channel_live_check', 'video_live_check')) NOT VALID`

func TestLiveAbsenceEvidenceMigrationUpgradesKindVocabIdempotently(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}

	restorePreLiveAbsenceContract(t, pool)

	for pass := 1; pass <= 2; pass++ {
		if err := applyMigrationFile(ctx, pool, dir, liveAbsenceEvidenceMigration); err != nil {
			t.Fatalf("apply %s pass %d: %v", liveAbsenceEvidenceMigration, pass, err)
		}
	}

	for _, kind := range []string{channelLiveCheckKind, videoLiveCheckKind} {
		assertObservationContract(t, pool, kind, 1, 1, "migration-218")
	}

	if _, err := pool.Exec(ctx, `
		UPDATE public.observation_contract_generations
		SET current_schema_version = 2, current_generation = 3, updated_by = 'later-rollout'
		WHERE provider = 'youtubejs' AND observation_kind = $1`, channelLiveCheckKind); err != nil {
		t.Fatalf("bump channel live check contract: %v", err)
	}

	if err := applyMigrationFile(ctx, pool, dir, liveAbsenceEvidenceMigration); err != nil {
		t.Fatalf("reapply %s after contract bump: %v", liveAbsenceEvidenceMigration, err)
	}

	assertObservationContract(t, pool, channelLiveCheckKind, 2, 3, "later-rollout")

	if _, err := pool.Exec(ctx, `
		INSERT INTO public.source_observation_consumer_offsets (consumer_name, observation_kind)
		VALUES ('dbtest-live-absence', $1), ('dbtest-live-absence', $2)`,
		channelLiveCheckKind, videoLiveCheckKind); err != nil {
		t.Fatalf("insert consumer offsets for new kinds: %v", err)
	}

	_, offsetErr := pool.Exec(ctx, `
		INSERT INTO public.source_observation_consumer_offsets (consumer_name, observation_kind)
		VALUES ('dbtest-live-absence', 'channel_live')`)
	requireSQLState(t, offsetErr, sqlStateCheckViolation)

	_, contractErr := pool.Exec(ctx, `
		INSERT INTO public.observation_contract_generations (
			provider, observation_kind, current_schema_version, current_generation, updated_by
		) VALUES ('youtubejs', 'video_live', 1, 1, 'dbtest')`)
	requireSQLState(t, contractErr, sqlStateCheckViolation)
}

func restorePreLiveAbsenceContract(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := t.Context()

	if _, err := pool.Exec(ctx, `
		DELETE FROM public.observation_contract_generations
		WHERE provider = 'youtubejs' AND observation_kind IN ($1, $2)`,
		channelLiveCheckKind, videoLiveCheckKind); err != nil {
		t.Fatalf("remove live absence contracts: %v", err)
	}

	if _, err := pool.Exec(ctx, restorePreLiveAbsenceKindVocabSQL); err != nil {
		t.Fatalf("restore pre-218 kind vocab: %v", err)
	}
}

func assertObservationContract(t *testing.T, pool *pgxpool.Pool, kind string, wantSchema int16, wantGeneration int64, wantUpdatedBy string) {
	t.Helper()

	var (
		schemaVersion int16
		generation    int64
		updatedBy     string
	)

	if err := pool.QueryRow(t.Context(), `
		SELECT current_schema_version, current_generation, updated_by
		FROM public.observation_contract_generations
		WHERE provider = 'youtubejs' AND observation_kind = $1`, kind).Scan(&schemaVersion, &generation, &updatedBy); err != nil {
		t.Fatalf("read %s contract: %v", kind, err)
	}

	if schemaVersion != wantSchema || generation != wantGeneration || updatedBy != wantUpdatedBy {
		t.Fatalf("%s contract = schema %d generation %d by %q, want schema %d generation %d by %q",
			kind, schemaVersion, generation, updatedBy, wantSchema, wantGeneration, wantUpdatedBy)
	}
}

type channelLiveCheckRow struct {
	provider        string
	outcome         string
	selectedVideoID *string
	confirmed       bool
	unknownReason   *string
	observationID   *int64
	hash            string
	clockSkew       time.Duration
}

func validChannelLiveCheck() channelLiveCheckRow {
	return channelLiveCheckRow{
		provider:  "youtubejs",
		outcome:   "CHANNEL_PAGE",
		confirmed: true,
		hash:      strings.Repeat("a", 64),
	}
}

func insertChannelLiveCheck(ctx context.Context, pool *pgxpool.Pool, channelID string, scheduledFor time.Time, row channelLiveCheckRow) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO public.youtube_channel_live_checks (
			channel_id, provider, outcome, selected_video_id, channel_identity_confirmed,
			unknown_reason, observation_id, evidence_sha256,
			scheduled_for, effective_at, observed_at, received_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $9, $9)`,
		channelID, row.provider, row.outcome, row.selectedVideoID, row.confirmed,
		row.unknownReason, row.observationID, row.hash,
		scheduledFor, scheduledFor.Add(row.clockSkew))
	if err != nil {
		return fmt.Errorf("insert channel live check %s: %w", channelID, err)
	}

	return nil
}

type videoAvailabilityRow struct {
	provider      string
	confirmed     bool
	availability  string
	method        string
	unknownReason *string
	observationID *int64
	hash          string
	clockSkew     time.Duration
}

func validVideoAvailability() videoAvailabilityRow {
	return videoAvailabilityRow{
		provider:     "youtubejs",
		confirmed:    true,
		availability: "PUBLIC",
		method:       "player_public",
		hash:         strings.Repeat("a", 64),
	}
}

func insertVideoAvailability(ctx context.Context, pool *pgxpool.Pool, videoID string, scheduledFor time.Time, row videoAvailabilityRow) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO public.youtube_video_availability (
			video_id, channel_id, provider, identity_confirmed, availability, method,
			unknown_reason, observation_id, evidence_sha256,
			scheduled_for, effective_at, observed_at, received_at
		) VALUES ($1, 'UCdbtestchannel', $2, $3, $4, $5, $6, $7, $8, $9, $10, $9, $9)`,
		videoID, row.provider, row.confirmed, row.availability, row.method,
		row.unknownReason, row.observationID, row.hash,
		scheduledFor, scheduledFor.Add(row.clockSkew))
	if err != nil {
		return fmt.Errorf("insert video availability %s: %w", videoID, err)
	}

	return nil
}

var channelLiveCheckShapeCases = []struct {
	name   string
	mutate func(*channelLiveCheckRow)
	reject bool
}{
	{name: "channel page negative", mutate: func(*channelLiveCheckRow) {}},
	{name: "upcoming video negative", mutate: func(r *channelLiveCheckRow) {
		r.outcome, r.selectedVideoID = "UPCOMING_VIDEO", new("upcoming0001")
	}},
	{name: "live video selection", mutate: func(r *channelLiveCheckRow) {
		r.outcome, r.selectedVideoID = "LIVE_VIDEO", new("livevideo001")
	}},
	{name: "unknown after identity mismatch", mutate: func(r *channelLiveCheckRow) {
		r.outcome, r.confirmed = liveCheckUnknown, false
		r.selectedVideoID, r.unknownReason = new("othervideo01"), new("identity_mismatch")
	}},
	{name: "unknown after request failure", mutate: func(r *channelLiveCheckRow) {
		r.outcome, r.confirmed, r.unknownReason = liveCheckUnknown, false, new("request_failed")
	}},
	{name: "non youtubejs provider", reject: true, mutate: func(r *channelLiveCheckRow) { r.provider = "holodex" }},
	{name: "unlisted outcome", reject: true, mutate: func(r *channelLiveCheckRow) { r.outcome = "OFFLINE" }},
	{name: "channel page with selected video", reject: true, mutate: func(r *channelLiveCheckRow) {
		r.selectedVideoID = new("somevideo001")
	}},
	{name: "live video without selection", reject: true, mutate: func(r *channelLiveCheckRow) { r.outcome = "LIVE_VIDEO" }},
	{name: "upcoming video without selection", reject: true, mutate: func(r *channelLiveCheckRow) { r.outcome = "UPCOMING_VIDEO" }},
	{name: "empty selected video", reject: true, mutate: func(r *channelLiveCheckRow) {
		r.outcome, r.selectedVideoID = "UPCOMING_VIDEO", new("")
	}},
	{name: "known outcome without identity", reject: true, mutate: func(r *channelLiveCheckRow) { r.confirmed = false }},
	{name: "known outcome with reason", reject: true, mutate: func(r *channelLiveCheckRow) {
		r.unknownReason = new("structure_unrecognized")
	}},
	{name: "unknown without reason", reject: true, mutate: func(r *channelLiveCheckRow) {
		r.outcome, r.confirmed = liveCheckUnknown, false
	}},
	{name: "video-only availability reason", reject: true, mutate: func(r *channelLiveCheckRow) {
		r.outcome, r.unknownReason = liveCheckUnknown, new("availability_unclassified")
	}},
	{name: "identity mismatch marked confirmed", reject: true, mutate: func(r *channelLiveCheckRow) {
		r.outcome, r.unknownReason = liveCheckUnknown, new("identity_mismatch")
	}},
	{name: "non canonical evidence hash", reject: true, mutate: func(r *channelLiveCheckRow) {
		r.hash = strings.Repeat("A", 64)
	}},
	{name: "effective clock differs from slot", reject: true, mutate: func(r *channelLiveCheckRow) {
		r.clockSkew = time.Second
	}},
}

func TestLiveAbsenceEvidenceChannelLiveCheckShape(t *testing.T) {
	pool := NewPool(t)
	scheduledFor := time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC)

	for i, tc := range channelLiveCheckShapeCases {
		t.Run(tc.name, func(t *testing.T) {
			row := validChannelLiveCheck()
			tc.mutate(&row)

			err := insertChannelLiveCheck(t.Context(), pool, fmt.Sprintf("UCshape%02d", i), scheduledFor, row)

			if tc.reject {
				requireSQLState(t, err, sqlStateCheckViolation)

				return
			}

			if err != nil {
				t.Fatalf("valid channel live check rejected: %v", err)
			}
		})
	}
}

var videoAvailabilityShapeCases = []struct {
	name   string
	mutate func(*videoAvailabilityRow)
	reject bool
}{
	{name: "public", mutate: func(*videoAvailabilityRow) {}},
	{name: "members only", mutate: func(r *videoAvailabilityRow) {
		r.availability, r.method = "MEMBERS_ONLY", "player_members_only"
	}},
	{name: "public unavailable", mutate: func(r *videoAvailabilityRow) {
		r.availability, r.method = "PUBLIC_UNAVAILABLE", "player_private"
	}},
	{name: "lifecycle facts without availability", mutate: func(r *videoAvailabilityRow) {
		r.availability, r.method, r.unknownReason = liveCheckUnknown, methodUnknown, new("availability_unclassified")
	}},
	{name: "request failure", mutate: func(r *videoAvailabilityRow) {
		r.availability, r.method, r.confirmed, r.unknownReason = liveCheckUnknown, methodUnknown, false, new("request_failed")
	}},
	{name: "non youtubejs provider", reject: true, mutate: func(r *videoAvailabilityRow) { r.provider = "holodex" }},
	{name: "unlisted availability", reject: true, mutate: func(r *videoAvailabilityRow) { r.availability = "PRIVATE" }},
	{name: "public by private method", reject: true, mutate: func(r *videoAvailabilityRow) { r.method = "player_private" }},
	{name: "unknown by public method", reject: true, mutate: func(r *videoAvailabilityRow) {
		r.availability, r.unknownReason = liveCheckUnknown, new("structure_unrecognized")
	}},
	{name: "unknown method for known availability", reject: true, mutate: func(r *videoAvailabilityRow) { r.method = methodUnknown }},
	{name: "public unavailable without identity", reject: true, mutate: func(r *videoAvailabilityRow) {
		r.availability, r.method, r.confirmed = "PUBLIC_UNAVAILABLE", "player_private", false
	}},
	{name: "known availability with reason", reject: true, mutate: func(r *videoAvailabilityRow) {
		r.unknownReason = new("login_required_unclassified")
	}},
	{name: "unknown without reason", reject: true, mutate: func(r *videoAvailabilityRow) {
		r.availability, r.method = liveCheckUnknown, methodUnknown
	}},
	{name: "availability unclassified without identity", reject: true, mutate: func(r *videoAvailabilityRow) {
		r.availability, r.method, r.confirmed, r.unknownReason = liveCheckUnknown, methodUnknown, false, new("availability_unclassified")
	}},
	{name: "identity missing marked confirmed", reject: true, mutate: func(r *videoAvailabilityRow) {
		r.availability, r.method, r.unknownReason = liveCheckUnknown, methodUnknown, new("identity_missing")
	}},
	{name: "unlisted reason", reject: true, mutate: func(r *videoAvailabilityRow) {
		r.availability, r.method, r.unknownReason = liveCheckUnknown, methodUnknown, new("robot_check")
	}},
	{name: "effective clock differs from slot", reject: true, mutate: func(r *videoAvailabilityRow) {
		r.clockSkew = -time.Second
	}},
}

func TestLiveAbsenceEvidenceVideoAvailabilityShape(t *testing.T) {
	pool := NewPool(t)
	scheduledFor := time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC)

	for i, tc := range videoAvailabilityShapeCases {
		t.Run(tc.name, func(t *testing.T) {
			row := validVideoAvailability()
			tc.mutate(&row)

			err := insertVideoAvailability(t.Context(), pool, fmt.Sprintf("vshape%02d", i), scheduledFor, row)

			if tc.reject {
				requireSQLState(t, err, sqlStateCheckViolation)

				return
			}

			if err != nil {
				t.Fatalf("valid video availability rejected: %v", err)
			}
		})
	}
}

// 원시 evidence retention이 canonical 최신값을 지우거나 막지 않고 hash와 시각을 남기는지 확인한다.
func TestLiveAbsenceEvidenceCanonicalRowsSurviveEvidenceRetention(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()
	base := time.Date(2026, time.September, 20, 0, 0, 0, 0, time.UTC)
	channelObservation := insertRetentionObservation(t, pool, channelLiveCheckKind, base, "UCretention")
	videoObservation := insertRetentionObservation(t, pool, videoLiveCheckKind, base, "vretention1")
	evidenceHash := strings.Repeat("c", 64)

	channelRow := validChannelLiveCheck()

	channelRow.observationID, channelRow.hash = &channelObservation, evidenceHash

	if err := insertChannelLiveCheck(ctx, pool, "UCretention", base, channelRow); err != nil {
		t.Fatal(err)
	}

	videoRow := validVideoAvailability()

	videoRow.observationID, videoRow.hash = &videoObservation, evidenceHash

	if err := insertVideoAvailability(ctx, pool, "vretention1", base, videoRow); err != nil {
		t.Fatal(err)
	}

	// 관측 하나는 subject 하나의 최신값만 증명한다.
	requireSQLState(t, insertChannelLiveCheck(ctx, pool, "UCretention2", base, channelRow), sqlStateUniqueViolation)
	requireSQLState(t, insertVideoAvailability(ctx, pool, "vretention2", base, videoRow), sqlStateUniqueViolation)

	deleted := deleteRetentionBatch(t, pool,
		[]string{channelLiveCheckKind, videoLiveCheckKind},
		[]time.Time{base.Add(time.Hour), base.Add(time.Hour)},
		10)
	if len(deleted) != 2 {
		t.Fatalf("retention deleted %v, want both live check observations", deleted)
	}

	for _, table := range []struct{ name, query string }{
		{name: "youtube_channel_live_checks", query: `
			SELECT observation_id, evidence_sha256, scheduled_for, effective_at, observed_at, received_at
			FROM public.youtube_channel_live_checks WHERE channel_id = 'UCretention'`},
		{name: "youtube_video_availability", query: `
			SELECT observation_id, evidence_sha256, scheduled_for, effective_at, observed_at, received_at
			FROM public.youtube_video_availability WHERE video_id = 'vretention1'`},
	} {
		var (
			observationID                                   *int64
			hash                                            string
			scheduledFor, effectiveAt, observedAt, received time.Time
		)

		if err := pool.QueryRow(ctx, table.query).Scan(
			&observationID, &hash, &scheduledFor, &effectiveAt, &observedAt, &received,
		); err != nil {
			t.Fatalf("read %s after evidence retention: %v", table.name, err)
		}

		if observationID != nil {
			t.Fatalf("%s observation_id = %d after evidence deletion, want NULL", table.name, *observationID)
		}

		if hash != evidenceHash {
			t.Fatalf("%s evidence_sha256 = %q, want %q", table.name, hash, evidenceHash)
		}

		for _, clock := range []time.Time{scheduledFor, effectiveAt, observedAt, received} {
			if !clock.Equal(base) {
				t.Fatalf("%s clocks = %v/%v/%v/%v, want %v", table.name, scheduledFor, effectiveAt, observedAt, received, base)
			}
		}
	}
}

func requireSQLState(t *testing.T, err error, want string) {
	t.Helper()

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		t.Fatalf("error = %v, want SQLSTATE %s", err, want)
	}

	if pgErr.Code != want {
		t.Fatalf("SQLSTATE = %s (%s), want %s", pgErr.Code, pgErr.ConstraintName, want)
	}
}
