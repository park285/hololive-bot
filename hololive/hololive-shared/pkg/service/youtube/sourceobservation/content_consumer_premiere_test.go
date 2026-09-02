package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/content"
	livereconcile "github.com/kapu/hololive-shared/internal/service/youtube/reconcile/live"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func TestContentConsumerPremiereConvergesContentThenLive(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)

	repo := NewRepository(pool)
	contentProof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
	liveProof := seedPremiereLivePublishLease(ctx, t, pool, &contentProof)
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)
	scheduled := time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC)

	published, err := repo.PublishBatch(ctx, publishInput(premiereVideoListEnvelope(t, &contentProof, scheduled)))
	if err != nil {
		t.Fatalf("publish content: %v", err)
	}

	if consumeErr := consumer.Consume(ctx, contentClaimOptions()); consumeErr != nil {
		t.Fatalf("consume content: %v", consumeErr)
	}

	assertLiveSessionPremiere(t, pool, domain.LiveStatusUpcoming, new(true))
	assertTableCount(t, pool, "youtube_live_reconciliation_heads", 0)

	replay, err := repo.RequestReplay(ctx, ReplayInput{
		ObservationID: published.Results[0].ObservationID,
		RequestedBy:   testReplayOperator,
		Reason:        "Premiere projection replay",
	})
	if err != nil || !replay.Applied {
		t.Fatalf("request replay: %#v err=%v", replay, err)
	}

	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume replay: %v", err)
	}

	assertTableCount(t, pool, "youtube_live_sessions", 1)
	assertTableCount(t, pool, "youtube_live_reconciliation_heads", 0)
	assertTableCount(t, pool, "youtube_notification_outbox", 1)
	assertTableCount(t, pool, "source_reconciliation_conflicts", 0)

	live := liveSession(testVideoID, "LIVE")

	live.ScheduledAt = &scheduled

	if _, err := repo.PublishBatch(ctx, publishInput(liveSnapshotEnvelope(t, &liveProof, live))); err != nil {
		t.Fatalf("publish live: %v", err)
	}

	if err := consumer.Consume(ctx, liveClaimOptions()); err != nil {
		t.Fatalf("consume live: %v", err)
	}

	assertLiveSessionPremiere(t, pool, domain.LiveStatusLive, new(true))
	assertTableCount(t, pool, "youtube_live_reconciliation_heads", 1)
}

func TestContentConsumerPremiereConvergesLiveThenContent(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)

	repo := NewRepository(pool)
	contentProof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
	liveProof := seedPremiereLivePublishLease(ctx, t, pool, &contentProof)
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)
	scheduled := time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC)
	live := liveSession(testVideoID, "LIVE")

	live.ScheduledAt = &scheduled

	if _, err := repo.PublishBatch(ctx, publishInput(liveSnapshotEnvelope(t, &liveProof, live))); err != nil {
		t.Fatalf("publish live: %v", err)
	}

	if err := consumer.Consume(ctx, liveClaimOptions()); err != nil {
		t.Fatalf("consume live: %v", err)
	}

	assertLiveSessionPremiere(t, pool, domain.LiveStatusLive, nil)

	beforeSession, beforeHead := premiereProjectionSnapshots(t, pool)

	if _, err := repo.PublishBatch(ctx, publishInput(premiereVideoListEnvelope(t, &contentProof, scheduled))); err != nil {
		t.Fatalf("publish content: %v", err)
	}

	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume content: %v", err)
	}

	assertLiveSessionPremiere(t, pool, domain.LiveStatusLive, new(true))

	afterSession, afterHead := premiereProjectionSnapshots(t, pool)
	if afterSession != beforeSession {
		t.Fatalf("content merge changed live-owned session fields\n before: %s\n  after: %s", beforeSession, afterSession)
	}

	if afterHead != beforeHead {
		t.Fatalf("content merge changed live reconciliation head\n before: %s\n  after: %s", beforeHead, afterHead)
	}
}

func TestContentConsumerPremiereIgnoresUnknownAndFalse(t *testing.T) {
	tests := []struct {
		name       string
		isPremiere *bool
	}{
		{name: "unknown"},
		{name: "false", isPremiere: new(false)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			pool := dbtest.NewPool(t)
			seedContentWatermark(t, pool)

			repo := NewRepository(pool)
			proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
			consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)
			scheduled := time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC)

			if _, err := repo.PublishBatch(ctx, publishInput(classifiedVideoListEnvelope(t, &proof, scheduled, test.isPremiere))); err != nil {
				t.Fatalf("publish content: %v", err)
			}

			if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
				t.Fatalf("consume content: %v", err)
			}

			assertTableCount(t, pool, "youtube_live_sessions", 0)
			assertTableCount(t, pool, "youtube_live_reconciliation_heads", 0)
			assertTableCount(t, pool, "source_reconciliation_conflicts", 0)
		})
	}
}

func TestContentConsumerPremiereConflictKeepsFalseAndRecordsOnce(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)

	seen := time.Date(2026, time.August, 29, 1, 2, 3, 0, time.UTC)

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions (
			video_id, channel_id, status, title, last_seen_at, is_premiere
		) VALUES ($1, $2, 'LIVE', 'Keep live fields', $3, FALSE)
	`, testVideoID, testChannelID, seen); err != nil {
		t.Fatalf("seed non-Premiere live session: %v", err)
	}

	beforeSession := premiereSessionSnapshot(t, pool)
	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)
	scheduled := time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC)

	published, err := repo.PublishBatch(ctx, publishInput(premiereVideoListEnvelope(t, &proof, scheduled)))
	if err != nil {
		t.Fatalf("publish content: %v", err)
	}

	if consumeErr := consumer.Consume(ctx, contentClaimOptions()); consumeErr != nil {
		t.Fatalf("consume content: %v", consumeErr)
	}

	assertLiveSessionPremiere(t, pool, domain.LiveStatusLive, new(false))

	if afterSession := premiereSessionSnapshot(t, pool); afterSession != beforeSession {
		t.Fatalf("conflict changed live session\n before: %s\n  after: %s", beforeSession, afterSession)
	}

	assertTableCount(t, pool, "source_reconciliation_conflicts", 1)
	assertTableCount(t, pool, "youtube_live_reconciliation_heads", 0)
	assertPremiereConflict(t, pool)

	replay, err := repo.RequestReplay(ctx, ReplayInput{
		ObservationID: published.Results[0].ObservationID,
		RequestedBy:   testReplayOperator,
		Reason:        "Premiere conflict replay",
	})
	if err != nil || !replay.Applied {
		t.Fatalf("request replay: %#v err=%v", replay, err)
	}

	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume replay: %v", err)
	}

	assertLiveSessionPremiere(t, pool, domain.LiveStatusLive, new(false))
	assertTableCount(t, pool, "source_reconciliation_conflicts", 1)
}

func TestContentConsumerPremiereAtomicRollback(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)

	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION reject_premiere_projection() RETURNS trigger
		LANGUAGE plpgsql AS $body$
		BEGIN
			RAISE EXCEPTION 'reject Premiere projection';
		END
		$body$;
		CREATE TRIGGER reject_premiere_projection
		BEFORE INSERT OR UPDATE ON youtube_live_sessions
		FOR EACH ROW EXECUTE FUNCTION reject_premiere_projection()
	`); err != nil {
		t.Fatalf("install rejection trigger: %v", err)
	}

	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)
	scheduled := time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC)

	if _, err := repo.PublishBatch(ctx, publishInput(premiereVideoListEnvelope(t, &proof, scheduled))); err != nil {
		t.Fatalf("publish content: %v", err)
	}

	if err := consumer.Consume(ctx, contentClaimOptions()); err == nil {
		t.Fatal("consume must fail when Premiere live projection fails")
	}

	assertTableCount(t, pool, "youtube_videos", 0)
	assertTableCount(t, pool, "youtube_notification_outbox", 0)
	assertTableCount(t, pool, "youtube_content_evidence_clocks", 0)
	assertTableCount(t, pool, "youtube_live_sessions", 0)
	assertTableCount(t, pool, "source_observation_applications", 0)
}

func TestContentPremierePersistencePreservesRowCreatedAfterRead(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	scheduled := time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC)
	received := scheduled.Add(-2 * time.Hour)

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions (
			video_id, channel_id, status, title, scheduled_start_time, last_seen_at
		) VALUES ($1, $2, 'UPCOMING', 'Schedule-owned title', $3, $3)
	`, testVideoID, testChannelID, scheduled); err != nil {
		t.Fatalf("seed concurrently created live session: %v", err)
	}

	before := premiereSessionSnapshot(t, pool)
	contentScheduled := scheduled.Add(30 * time.Minute)
	decision := livereconcile.PremiereDecision{Sessions: []livereconcile.SessionState{{
		VideoID:            testVideoID,
		ChannelID:          testChannelID,
		Status:             livereconcile.StatusUpcoming,
		Title:              "Content-owned title",
		ScheduledStartTime: &contentScheduled,
		LastSeenAt:         received,
		IsPremiere:         new(true),
	}}}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Premiere persistence: %v", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback Premiere persistence: %v", rollbackErr)
		}
	}()

	if err := persistPremiereDecision(ctx, tx, &Observation{}, &decision); err != nil {
		t.Fatalf("persist Premiere decision: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit Premiere persistence: %v", err)
	}

	assertLiveSessionPremiere(t, pool, domain.LiveStatusUpcoming, new(true))

	if after := premiereSessionSnapshot(t, pool); after != before {
		t.Fatalf("Premiere conflict changed concurrently created session\n before: %s\n  after: %s", before, after)
	}
}

func TestContentConsumerPremiereKeepsApplicationsWithinFinalizeLimit(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)

	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)
	scheduled := time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC)

	published, err := repo.PublishBatch(ctx, publishInput(largePremiereVideoListEnvelope(t, &proof, scheduled, 999)))
	if err != nil {
		t.Fatalf("publish large content observation: %v", err)
	}

	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume large content observation: %v", err)
	}

	var applicationCount int

	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM source_observation_applications
		WHERE observation_id = $1
	`, published.Results[0].ObservationID).Scan(&applicationCount); err != nil {
		t.Fatalf("count large observation applications: %v", err)
	}

	if applicationCount != 1000 {
		t.Fatalf("application count = %d, want 1000", applicationCount)
	}

	var projected bool

	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM source_observation_applications
			WHERE observation_id = $1
			  AND entity_kind = 'youtube_live_session'
			  AND entity_key = 'vid-0000'
		)
	`, published.Results[0].ObservationID).Scan(&projected); err != nil {
		t.Fatalf("load Premiere projection application: %v", err)
	}

	if !projected {
		t.Fatal("bounded applications omitted Premiere projection")
	}
}

func TestContentPremiereFactsBoundLiveTitle(t *testing.T) {
	evidence := content.Evidence{
		Kind: contract.KindVideoList,
		Videos: []content.Entity{{
			VideoID:    testVideoID,
			ChannelID:  testChannelID,
			Title:      strings.Repeat("x", 501),
			IsPremiere: new(true),
		}},
	}

	facts := confirmedPremiereFacts(&evidence)
	if len(facts) != 1 || len(facts[0].Title) != 500 {
		t.Fatalf("Premiere fact title length = %d, want 500", len(facts[0].Title))
	}
}

func premiereVideoListEnvelope(t *testing.T, proof *contract.LeaseProof, scheduled time.Time) *contract.Envelope {
	t.Helper()

	return classifiedVideoListEnvelope(t, proof, scheduled, new(true))
}

func classifiedVideoListEnvelope(
	t *testing.T,
	proof *contract.LeaseProof,
	scheduled time.Time,
	isPremiere *bool,
) *contract.Envelope {
	t.Helper()

	published := scheduled.Add(-24 * time.Hour)

	payload, err := contract.MarshalPayloadV1(contract.VideoListV1{
		ChannelID: testChannelID,
		Videos: []contract.VideoListItemV1{{
			VideoID:      testVideoID,
			ChannelID:    testChannelID,
			Title:        "Premiere title",
			PublishedAt:  &published,
			ScheduledFor: &scheduled,
			IsPremiere:   isPremiere,
		}},
		Coverage: contract.ChannelListCoverageV1{
			ChannelID:  testChannelID,
			MaxResults: 10,
			Exhausted:  true,
		},
	})
	if err != nil {
		t.Fatalf("marshal Premiere video list payload: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider:           contract.ProviderYouTubeJS,
		ObservationKind:    contract.KindVideoList,
		SubjectKey:         testChannelID,
		SchemaVersion:      contract.SchemaVersionV1,
		ContractGeneration: 1,
		ScheduledFor:       proof.ScheduledFor,
		ObservedAt:         proof.ScheduledFor.Add(time.Second),
		Completeness:       contract.CompletenessComplete,
		Continuity:         contract.ContinuityContiguous,
		Payload:            payload,
		CollectorInstance:  proof.OwnerInstance,
		Lease:              *proof,
	})
	if err != nil {
		t.Fatalf("prepare Premiere video list envelope: %v", err)
	}

	return &envelope
}

func largePremiereVideoListEnvelope(
	t *testing.T,
	proof *contract.LeaseProof,
	scheduled time.Time,
	count int,
) *contract.Envelope {
	t.Helper()

	published := scheduled.Add(-24 * time.Hour)
	videos := make([]contract.VideoListItemV1, count)

	for i := range count {
		videos[i] = contract.VideoListItemV1{
			VideoID:     fmt.Sprintf("vid-%04d", i),
			ChannelID:   testChannelID,
			Title:       fmt.Sprintf("Video %d", i),
			PublishedAt: &published,
		}
	}

	videos[0].ScheduledFor = &scheduled
	videos[0].IsPremiere = new(true)

	payload, err := contract.MarshalPayloadV1(contract.VideoListV1{
		ChannelID: testChannelID,
		Videos:    videos,
		Coverage: contract.ChannelListCoverageV1{
			ChannelID:  testChannelID,
			MaxResults: count,
			Exhausted:  true,
		},
	})
	if err != nil {
		t.Fatalf("marshal large Premiere video list payload: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider:           contract.ProviderYouTubeJS,
		ObservationKind:    contract.KindVideoList,
		SubjectKey:         testChannelID,
		SchemaVersion:      contract.SchemaVersionV1,
		ContractGeneration: 1,
		ScheduledFor:       proof.ScheduledFor,
		ObservedAt:         proof.ScheduledFor.Add(time.Second),
		Completeness:       contract.CompletenessComplete,
		Continuity:         contract.ContinuityContiguous,
		Payload:            payload,
		CollectorInstance:  proof.OwnerInstance,
		Lease:              *proof,
	})
	if err != nil {
		t.Fatalf("prepare large Premiere video list envelope: %v", err)
	}

	return &envelope
}

func seedPremiereLivePublishLease(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	contentProof *contract.LeaseProof,
) contract.LeaseProof {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_targets (
			projection_generation, subject_key, observation_kind,
			priority, poll_interval_ms, enabled, valid_until
		) VALUES ($1, $2, $3, 50, 60000, TRUE, NOW() + INTERVAL '1 day')
	`, contentProof.ProjectionGeneration, testChannelID, contract.KindLiveSnapshot); err != nil {
		t.Fatalf("seed live target: %v", err)
	}

	proof := contract.LeaseProof{
		JobKey:               "job:youtubejs_channel_live:" + testChannelID,
		CollectionJobKind:    "youtubejs_channel_live",
		OwnerInstance:        contentProof.OwnerInstance,
		FenceEpoch:           1,
		ProjectionGeneration: contentProof.ProjectionGeneration,
		ScheduledFor:         contentProof.ScheduledFor,
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_job_leases (
			job_key, provider, job_class, collection_job_kind, subject_key,
			projection_generation, poll_interval_ms, slot_state, scheduled_for,
			next_due_at, fence_epoch, owner_instance, lease_expires_at
		) VALUES ($1, $2, 'SUBJECT', $3, $4, $5, 60000, 'ACTIVE', $6, $6, $7, $8, NOW() + INTERVAL '1 hour')
	`, proof.JobKey, contract.ProviderYouTubeJS, proof.CollectionJobKind, testChannelID,
		proof.ProjectionGeneration, proof.ScheduledFor, proof.FenceEpoch, proof.OwnerInstance); err != nil {
		t.Fatalf("seed live lease: %v", err)
	}

	return proof
}

func assertLiveSessionPremiere(t *testing.T, pool *pgxpool.Pool, wantStatus domain.LiveStatus, wantPremiere *bool) {
	t.Helper()

	var (
		status     domain.LiveStatus
		isPremiere *bool
	)

	if err := pool.QueryRow(t.Context(), `
		SELECT status, is_premiere
		FROM youtube_live_sessions
		WHERE video_id = $1
	`, testVideoID).Scan(&status, &isPremiere); err != nil {
		t.Fatalf("load Premiere live session: %v", err)
	}

	if status != wantStatus {
		t.Fatalf("status = %s, want %s", status, wantStatus)
	}

	if wantPremiere == nil {
		if isPremiere != nil {
			t.Fatalf("is_premiere = %v, want nil", *isPremiere)
		}

		return
	}

	if isPremiere == nil || *isPremiere != *wantPremiere {
		t.Fatalf("is_premiere = %v, want %t", isPremiere, *wantPremiere)
	}
}

func premiereProjectionSnapshots(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()

	return premiereSessionSnapshot(t, pool), premiereHeadSnapshot(t, pool)
}

func premiereSessionSnapshot(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var snapshot string

	if err := pool.QueryRow(t.Context(), `
		SELECT (to_jsonb(session) - 'is_premiere')::text
		FROM youtube_live_sessions AS session
		WHERE video_id = $1
	`, testVideoID).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot live session: %v", err)
	}

	return snapshot
}

func premiereHeadSnapshot(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var snapshot string

	if err := pool.QueryRow(t.Context(), `
		SELECT to_jsonb(head)::text
		FROM youtube_live_reconciliation_heads AS head
		WHERE video_id = $1
	`, testVideoID).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot live head: %v", err)
	}

	return snapshot
}

func assertPremiereConflict(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	var (
		entityKind   string
		entityKey    string
		fieldName    string
		existingSHA  string
		attemptedSHA string
		decision     string
	)

	if err := pool.QueryRow(t.Context(), `
		SELECT entity_kind, entity_key, field_name,
		       existing_value_sha256, attempted_value_sha256, decision
		FROM source_reconciliation_conflicts
	`).Scan(&entityKind, &entityKey, &fieldName, &existingSHA, &attemptedSHA, &decision); err != nil {
		t.Fatalf("load Premiere conflict: %v", err)
	}

	if entityKind != "youtube_live_session" || entityKey != testVideoID || fieldName != "is_premiere" || decision != "KEEP_EXISTING" {
		t.Fatalf("conflict identity = {%s %s %s %s}", entityKind, entityKey, fieldName, decision)
	}

	if existingSHA != contract.SHA256Hex([]byte("false")) || attemptedSHA != contract.SHA256Hex([]byte("true")) {
		t.Fatalf("conflict hashes = {%s %s}", existingSHA, attemptedSHA)
	}
}
