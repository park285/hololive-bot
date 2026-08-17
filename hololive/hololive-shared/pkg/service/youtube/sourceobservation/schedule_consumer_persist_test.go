package sourceobservation

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func TestScheduleConsumerOfficialIsLiveDoesNotFlipLive(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, context.Background(), pool, contract.ProviderHololiveOfficial, contract.KindSchedule, "global:hololive-schedule", "official_schedule")
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)
	if _, err := repo.PublishBatch(ctx, publishInput(scheduleEnvelope(t, &proof, contract.ScheduleItemV1{
		ExternalID: "vid-a", VideoID: "vid-a", ChannelID: "UC_TEST", Title: "Official Live",
		ScheduledAt: time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC), IsLive: true,
	}))); err != nil {
		t.Fatalf("publish schedule: %v", err)
	}
	if err := consumer.Consume(ctx, liveClaimOptions()); err != nil {
		t.Fatalf("consume schedule: %v", err)
	}
	if liveSessionStatus(t, pool) != string(domain.LiveStatusUpcoming) {
		t.Fatal("official isLive must not write LIVE")
	}
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
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, context.Background(), pool, contract.ProviderHololiveOfficial, contract.KindSchedule, "global:hololive-schedule", "official_schedule")
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)
	if _, err := repo.PublishBatch(ctx, publishInput(scheduleEnvelope(t, &proof, contract.ScheduleItemV1{
		ExternalID: "vid-a", VideoID: "vid-a", ChannelID: "UC_TEST", Title: "Official Collab",
		ScheduledAt:        time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC),
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

func TestScheduleConsumerDoesNotAdvanceLiveLastSeenAt(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seen := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, title, last_seen_at)
		VALUES ('vid-a', 'UC_TEST', 'LIVE', 'Keep', $1)
	`, seen); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	repo := NewRepository(pool)
	proof := seedPublishLease(t, context.Background(), pool, contract.ProviderHololiveOfficial, contract.KindSchedule, "global:hololive-schedule", "official_schedule")
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)
	if _, err := repo.PublishBatch(ctx, publishInput(scheduleEnvelope(t, &proof, contract.ScheduleItemV1{
		ExternalID: "vid-a", VideoID: "vid-a", ChannelID: "UC_TEST", Title: "Schedule Title",
		ScheduledAt: time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC), IsLive: true,
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
}

func TestScheduleConsumerTemporaryItemDoesNotMergeSession(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, title, last_seen_at)
		VALUES ('vid-a', 'UC_TEST', 'UPCOMING', 'Keep', NOW())
	`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	repo := NewRepository(pool)
	proof := seedPublishLease(t, context.Background(), pool, contract.ProviderHolodex, contract.KindSchedule, "global:hololive-schedule", "holodex_schedule")
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)
	if _, err := repo.PublishBatch(ctx, publishInput(scheduleEnvelope(t, &proof, contract.ScheduleItemV1{
		ExternalID: "holodex-temp", Title: "Temp", ScheduledAt: time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC),
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
