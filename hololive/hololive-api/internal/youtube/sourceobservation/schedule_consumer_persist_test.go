package sourceobservation

import (
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

func TestScheduleConsumerOfficialIsLiveDoesNotFlipLive(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderHololiveOfficial, contract.KindSchedule, "global:hololive-schedule", "official_schedule")
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)

	if _, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(scheduleEnvelope(t, &proof, contract.ScheduleItemV1{
		ExternalID: testVideoID, VideoID: testVideoID, ChannelID: testChannelID, Title: "Official Live",
		ScheduledAt: time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC), IsLive: true,
	}))); err != nil {
		t.Fatalf("publish schedule: %v", err)
	}

	if err := consumer.Consume(ctx, liveClaimOptions()); err != nil {
		t.Fatalf("consume schedule: %v", err)
	}

	if liveSessionStatus(t, pool) != string(domain.LiveStatusUpcoming) {
		t.Fatal("official isLive must not write LIVE")
	}

	assertLifecycleOrigin(t, pool, "metadata_only")
	assertTableCount(t, pool, "youtube_live_reconciliation_heads", 0)

	var isLive bool

	if err := pool.QueryRow(ctx, `
		SELECT is_live FROM youtube_schedule_items WHERE external_id = 'vid-a'
	`).Scan(&isLive); err != nil {
		t.Fatal(err)
	}

	if !isLive {
		t.Fatal("schedule item must retain is_live evidence")
	}
}

func TestScheduleConsumerPersistsOfficialCollaboTalentNames(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderHololiveOfficial, contract.KindSchedule, "global:hololive-schedule", "official_schedule")
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)

	if _, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(scheduleEnvelope(t, &proof, contract.ScheduleItemV1{
		ExternalID: testVideoID, VideoID: testVideoID, ChannelID: testChannelID, Title: "Official Collab",
		ScheduledAt:        time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC),
		CollaboTalentNames: []string{"Guest One", "Guest Two"},
	}))); err != nil {
		t.Fatalf("publish schedule: %v", err)
	}

	if err := consumer.Consume(ctx, liveClaimOptions()); err != nil {
		t.Fatalf("consume schedule: %v", err)
	}

	var names []string

	if err := pool.QueryRow(ctx, `
		SELECT collabo_talent_names FROM youtube_schedule_items WHERE external_id = 'vid-a'
	`).Scan(&names); err != nil {
		t.Fatal(err)
	}

	if len(names) != 2 || names[0] != "Guest One" || names[1] != "Guest Two" {
		t.Fatalf("collabo_talent_names = %#v", names)
	}
}

func TestScheduleConsumerPreservesPremiereAndDoesNotAdvanceLiveLastSeenAt(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seen := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, title, last_seen_at, is_premiere)
		VALUES ('vid-a', 'UC_TEST', 'LIVE', 'Keep', $1, TRUE)
	`, seen); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	repo := NewRepository(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderHololiveOfficial, contract.KindSchedule, "global:hololive-schedule", "official_schedule")
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)

	if _, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(scheduleEnvelope(t, &proof, contract.ScheduleItemV1{
		ExternalID: testVideoID, VideoID: testVideoID, ChannelID: testChannelID, Title: "Schedule Title",
		ScheduledAt: time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC), IsLive: true,
	}))); err != nil {
		t.Fatalf("publish schedule: %v", err)
	}

	if err := consumer.Consume(ctx, liveClaimOptions()); err != nil {
		t.Fatalf("consume schedule: %v", err)
	}

	if liveSessionStatus(t, pool) != string(domain.LiveStatusLive) {
		t.Fatal("schedule merge must not own live liveness")
	}

	if !liveLastSeen(t, pool).Equal(seen) {
		t.Fatalf("schedule consume advanced last_seen_at: %s", liveLastSeen(t, pool))
	}

	assertLiveSessionPremiere(t, pool, domain.LiveStatusLive, new(true))
}

// schedule reducer는 누적 item을 읽지 않으므로 consume은 과거 item 행을 읽거나 잠그지 않아야 한다.
// FOR UPDATE는 커밋 뒤에도 튜플 xmax에 잠금 트랜잭션을 남기므로 xmax=0이 잠금 부재의 증거다.
func TestScheduleConsumerDoesNotLockRetainedItemHistory(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	const historyCount = 50

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_schedule_items (
			group_key, provider, external_id, video_id, channel_id, title, scheduled_at, updated_at
		)
		SELECT 'global:hololive-schedule', 'hololive_official', 'history-' || n, 'history-' || n, 'UC_TEST',
		       'History ' || n, TIMESTAMPTZ '2026-08-01 00:00:00+00' + n * INTERVAL '1 hour',
		       TIMESTAMPTZ '2026-08-02 00:00:00+00'
		FROM generate_series(1, $1::int) AS n
	`, historyCount); err != nil {
		t.Fatalf("seed schedule history: %v", err)
	}

	repo := NewRepository(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderHololiveOfficial, contract.KindSchedule, "global:hololive-schedule", "official_schedule")
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)

	if _, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(scheduleEnvelope(t, &proof, contract.ScheduleItemV1{
		ExternalID: testVideoID, VideoID: testVideoID, ChannelID: testChannelID, Title: "Official Upcoming",
		ScheduledAt: time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC),
	}))); err != nil {
		t.Fatalf("publish schedule: %v", err)
	}

	if err := consumer.Consume(ctx, liveClaimOptions()); err != nil {
		t.Fatalf("consume schedule: %v", err)
	}

	if liveSessionStatus(t, pool) != string(domain.LiveStatusUpcoming) {
		t.Fatal("schedule consume must still merge the observed item into its live session")
	}

	var retained, locked, touched int

	if err := pool.QueryRow(ctx, `
		SELECT count(external_id),
		       count(external_id) FILTER (WHERE xmax::text <> '0'),
		       count(external_id) FILTER (WHERE updated_at <> TIMESTAMPTZ '2026-08-02 00:00:00+00')
		FROM youtube_schedule_items
		WHERE external_id LIKE 'history-%'
	`).Scan(&retained, &locked, &touched); err != nil {
		t.Fatalf("load schedule history: %v", err)
	}

	if retained != historyCount || locked != 0 || touched != 0 {
		t.Fatalf("retained history = %d locked = %d touched = %d, want %d unlocked and untouched rows", retained, locked, touched, historyCount)
	}

	var observedTitle string

	if err := pool.QueryRow(ctx, `
		SELECT title FROM youtube_schedule_items WHERE external_id = $1
	`, testVideoID).Scan(&observedTitle); err != nil {
		t.Fatalf("load observed schedule item: %v", err)
	}

	if observedTitle != "Official Upcoming" {
		t.Fatalf("observed schedule item title = %q", observedTitle)
	}
}

func TestScheduleConsumerTemporaryItemDoesNotMergeSession(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, title, last_seen_at)
		VALUES ('vid-a', 'UC_TEST', 'UPCOMING', 'Keep', NOW())
	`); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	repo := NewRepository(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderHolodex, contract.KindSchedule, "global:hololive-schedule", "holodex_schedule")
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)

	if _, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(scheduleEnvelope(t, &proof, contract.ScheduleItemV1{
		ExternalID: "holodex-temp", Title: "Temp", ScheduledAt: time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC),
	}))); err != nil {
		t.Fatalf("publish schedule: %v", err)
	}

	if err := consumer.Consume(ctx, liveClaimOptions()); err != nil {
		t.Fatalf("consume schedule: %v", err)
	}

	var title string

	if err := pool.QueryRow(ctx, `SELECT title FROM youtube_live_sessions WHERE video_id = 'vid-a'`).Scan(&title); err != nil {
		t.Fatal(err)
	}

	if title != "Keep" {
		t.Fatalf("temporary schedule item merged into YouTube session: %s", title)
	}
}

func scheduleEnvelope(t *testing.T, proof *contract.LeaseProof, items ...contract.ScheduleItemV1) *contract.Envelope {
	t.Helper()

	payload, err := contract.MarshalPayloadV1(contract.ScheduleSnapshotV1{
		GroupKey: "global:hololive-schedule",
		Items:    items,
		Coverage: contract.ScheduleCoverageV1{GroupKey: "global:hololive-schedule"},
	})
	if err != nil {
		t.Fatalf("marshal schedule: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: proofProvider(proof), ObservationKind: contract.KindSchedule, SubjectKey: "global:hololive-schedule",
		SchemaVersion: contract.SchemaVersionV1, ContractGeneration: 1,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityNotApplicable,
		Payload: payload, CollectorInstance: proof.OwnerInstance, Lease: *proof,
	})
	if err != nil {
		t.Fatalf("prepare schedule: %v", err)
	}

	return &envelope
}

func proofProvider(proof *contract.LeaseProof) contract.Provider {
	if proof.CollectionJobKind == "holodex_schedule" {
		return contract.ProviderHolodex
	}

	return contract.ProviderHololiveOfficial
}
