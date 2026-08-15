package sourceobservation

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func TestViewerConsumerRetainsEqualConsecutiveSamples(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, last_seen_at)
		VALUES ('vid-a', 'UC_TEST', 'LIVE', NOW())
	`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	repo := NewRepository(pool)
	proof := seedPublishLease(t, context.Background(), pool, contract.ProviderYouTubeJS, contract.KindViewerSample, "vid-a", "youtubejs_viewer")
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)
	first := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	second := first.Add(2 * time.Minute)
	proof = publishConsumeViewer(t, ctx, pool, repo, consumer, &proof, first, 10)
	publishConsumeViewer(t, ctx, pool, repo, consumer, &proof, second, 10)
	assertTableCount(t, pool, "youtube_live_viewer_samples", 2)
}

func TestViewerConsumerEqualWindowConflictStaysUnresolved(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, last_seen_at)
		VALUES ('vid-a', 'UC_TEST', 'LIVE', NOW())
	`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	repo := NewRepository(pool)
	proof := seedPublishLease(t, context.Background(), pool, contract.ProviderYouTubeJS, contract.KindViewerSample, "vid-a", "youtubejs_viewer")
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)
	first := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	second := first.Add(2 * time.Minute)
	proof = publishConsumeViewer(t, ctx, pool, repo, consumer, &proof, first, 10)
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_viewer_sample_evidence (
			video_id, sample_window_start, provider, viewer_count, availability,
			sample_window_seconds, scheduled_for, effective_at, received_at
		) VALUES ('vid-a', $1, 'holodex', 99, 'AVAILABLE', 120, $1, $1, $1)
	`, second); err != nil {
		t.Fatalf("seed conflicting evidence: %v", err)
	}
	publishConsumeViewer(t, ctx, pool, repo, consumer, &proof, second, 20)
	var unresolved *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT unresolved_window_start FROM youtube_live_viewer_sample_heads WHERE video_id = 'vid-a'
	`).Scan(&unresolved); err != nil {
		t.Fatal(err)
	}
	if unresolved == nil || !unresolved.Equal(second) {
		t.Fatalf("unresolved = %v", unresolved)
	}
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM youtube_live_viewer_samples WHERE video_id = 'vid-a' AND captured_at = $1
	`, second).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("unresolved window must not advance last resolved product")
	}
}

func publishConsumeViewer(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo *Repository,
	consumer *Consumer,
	proof *contract.LeaseProof,
	window time.Time,
	count int64,
) contract.LeaseProof {
	t.Helper()
	payload, err := contract.MarshalPayloadV1(contract.ViewerSampleV1{
		VideoID: "vid-a", ViewerCount: &count, Availability: "AVAILABLE",
		SampleWindowStart: window, SampleWindowSeconds: 120,
		Coverage: contract.ViewerSampleCoverageV1{
			VideoID: "vid-a", SampleWindowStart: window, SampleWindowSeconds: 120,
		},
	})
	if err != nil {
		t.Fatalf("marshal viewer: %v", err)
	}
	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindViewerSample, SubjectKey: "vid-a",
		SchemaVersion: contract.SchemaVersionV1, ContractGeneration: 1,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityNotApplicable,
		Payload: payload, CollectorInstance: proof.OwnerInstance, Lease: *proof,
	})
	if err != nil {
		t.Fatalf("prepare viewer: %v", err)
	}
	if _, err := repo.PublishBatch(ctx, publishInput(&envelope)); err != nil {
		t.Fatalf("publish viewer: %v", err)
	}
	if err := consumer.Consume(ctx, liveClaimOptions()); err != nil {
		t.Fatalf("consume viewer: %v", err)
	}
	return advanceLease(t, ctx, pool, proof, time.Minute)
}
