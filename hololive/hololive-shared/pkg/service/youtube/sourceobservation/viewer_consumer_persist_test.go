package sourceobservation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func TestViewerConsumerRetainsEqualConsecutiveSamples(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, last_seen_at)
		VALUES ('vid-a', 'UC_TEST', 'LIVE', NOW())
	`); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	repo := historicalViewerPublisher(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindViewerSample, testVideoID, "youtubejs_viewer")
	consumer := NewConsumerWithGraces(NewRepository(pool), NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)
	first := time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC)
	second := first.Add(2 * time.Minute)

	proof = publishConsumeViewer(ctx, t, pool, repo, consumer, &proof, first, 10)
	publishConsumeViewer(ctx, t, pool, repo, consumer, &proof, second, 10)
	assertTableCount(t, pool, "youtube_live_viewer_samples", 2)
}

func TestViewerConsumerEqualWindowConflictStaysUnresolved(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, last_seen_at)
		VALUES ('vid-a', 'UC_TEST', 'LIVE', NOW())
	`); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	repo := historicalViewerPublisher(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindViewerSample, testVideoID, "youtubejs_viewer")
	consumer := NewConsumerWithGraces(NewRepository(pool), NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)
	first := time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC)
	second := first.Add(2 * time.Minute)

	proof = publishConsumeViewer(ctx, t, pool, repo, consumer, &proof, first, 10)

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_viewer_sample_evidence (
			video_id, sample_window_start, provider, viewer_count, availability,
			sample_window_seconds, scheduled_for, effective_at, received_at
		) VALUES ('vid-a', $1, 'holodex', 99, 'AVAILABLE', 120, $1, $1, $1)
	`, second); err != nil {
		t.Fatalf("seed conflicting evidence: %v", err)
	}

	publishConsumeViewer(ctx, t, pool, repo, consumer, &proof, second, 20)

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
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	repo *Repository,
	consumer *Consumer,
	proof *contract.LeaseProof,
	window time.Time,
	count int64,
) contract.LeaseProof {
	t.Helper()

	payload, err := contract.MarshalPayloadV1(contract.ViewerSampleV1{
		VideoID: testVideoID, ViewerCount: &count, Availability: "AVAILABLE",
		SampleWindowStart: window, SampleWindowSeconds: 120,
		Coverage: contract.ViewerSampleCoverageV1{
			VideoID: testVideoID, SampleWindowStart: window, SampleWindowSeconds: 120,
		},
	})
	if err != nil {
		t.Fatalf("marshal viewer: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindViewerSample, SubjectKey: testVideoID,
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

	return advanceLease(ctx, t, pool, proof, time.Minute)
}

// 과거 생산자의 계약으로 보존 관측을 시드합니다. 현재 운영 publisher에는 적용하지 않습니다.
func historicalViewerJobContracts() StaticJobContracts {
	jobs := InitialJobContracts()
	youtubejs := mustJobID(contract.ProviderYouTubeJS, "youtubejs_viewer")

	jobs[youtubejs] = mustJobContract(youtubejs, JobClassSubject, JobMembershipExactSubject, "",
		[]contract.ObservationKind{contract.KindViewerSample},
		[]contract.ObservationKind{contract.KindViewerSample}, nil)

	holodex := mustJobID(contract.ProviderHolodex, "holodex_live")

	jobs[holodex] = mustJobContract(holodex, JobClassGlobal, JobMembershipCurrentProjection, "global:holodex_live",
		[]contract.ObservationKind{contract.KindLiveSnapshot, contract.KindViewerSample},
		[]contract.ObservationKind{contract.KindLiveSnapshot, contract.KindViewerSample},
		[]contract.ObservationKind{contract.KindLiveSnapshot})

	return jobs
}

func historicalViewerPublisher(pool *pgxpool.Pool) *Repository {
	return NewRepositoryWithContracts(pool, InitialSupportedContracts(), historicalViewerJobContracts(), nil)
}

func TestPublishRejectsRetiredViewerCollectionWithoutSideEffects(t *testing.T) {
	for _, tc := range []struct {
		provider contract.Provider
		job      string
		wantErr  error
	}{
		{contract.ProviderYouTubeJS, "youtubejs_viewer", ErrCollectionFenceLost},
		{contract.ProviderHolodex, "holodex_live", ErrTargetDisabled},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			pool := dbtest.NewPool(t)
			proof := seedPublishLease(t.Context(), t, pool, tc.provider, contract.KindViewerSample, "video-1", tc.job)
			historical := viewerEnvelope(t, &proof, 1, 100)

			envelope, err := contract.PrepareEnvelope(contract.Envelope{
				Provider: tc.provider, ObservationKind: contract.KindViewerSample, SubjectKey: historical.SubjectKey,
				SchemaVersion: historical.SchemaVersion, ContractGeneration: historical.ContractGeneration,
				ScheduledFor: historical.ScheduledFor, ObservedAt: historical.ObservedAt,
				Completeness: historical.Completeness, Continuity: historical.Continuity,
				Payload: historical.Payload, CollectorInstance: proof.OwnerInstance, Lease: proof,
			})
			if err != nil {
				t.Fatal(err)
			}

			if _, err := NewRepository(pool).PublishBatch(t.Context(), publishInput(&envelope)); !errors.Is(err, tc.wantErr) {
				t.Fatalf("retired viewer publish error = %v, want %v", err, tc.wantErr)
			}

			assertPublishSideEffects(t, pool, 0, 0, 0)

			var state string

			if err := pool.QueryRow(t.Context(), `SELECT slot_state FROM youtube_collection_job_leases WHERE job_key = $1`, proof.JobKey).Scan(&state); err != nil {
				t.Fatal(err)
			}

			if state != "ACTIVE" {
				t.Fatalf("rejected viewer publish changed lease to %s", state)
			}
		})
	}
}
