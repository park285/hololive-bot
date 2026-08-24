package sourceobservation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func TestChannelStatsConsumerRetainsEqualConsecutiveSamplesBySlot(t *testing.T) {
	pool, repo, consumer, proof := startChannelPersist(t, contract.KindChannelStats)
	ctx := t.Context()

	proof = publishConsumeStats(ctx, t, pool, repo, consumer, &proof, contract.ProviderYouTubeJS, 10)
	publishConsumeStats(ctx, t, pool, repo, consumer, &proof, contract.ProviderYouTubeJS, 10)
	assertTableCount(t, pool, "youtube_channel_stats_snapshots", 2)
	assertTableCount(t, pool, "youtube_channel_stats_evidence", 2)
}

func TestChannelStatsConsumerHiddenCountRemainsNil(t *testing.T) {
	pool, repo, consumer, proof := startChannelPersist(t, contract.KindChannelStats)
	ctx := t.Context()
	publishConsumeStatsHidden(ctx, t, repo, consumer, &proof)

	var sub, views, videos *int64

	if err := pool.QueryRow(ctx, `
		SELECT subscriber_count, view_count, video_count
		FROM youtube_channel_stats_snapshots WHERE channel_id = 'UC_TEST'
	`).Scan(&sub, &views, &videos); err != nil {
		t.Fatal(err)
	}

	if sub != nil || views != nil || videos != nil {
		t.Fatalf("hidden snapshot counts = %v %v %v", sub, views, videos)
	}

	if err := pool.QueryRow(ctx, `
		SELECT last_resolved_subscriber_count, last_resolved_view_count, last_resolved_video_count
		FROM youtube_channel_stats_heads WHERE channel_id = 'UC_TEST'
	`).Scan(&sub, &views, &videos); err != nil {
		t.Fatal(err)
	}

	if sub != nil || views != nil || videos != nil {
		t.Fatalf("hidden latest counts = %v %v %v", sub, views, videos)
	}
}

func TestChannelStatsConsumerEqualTimeConflictDoesNotOverwrite(t *testing.T) {
	pool, repo, consumer, proof := startChannelPersist(t, contract.KindChannelStats)
	ctx := t.Context()
	alt := seedAdditionalLease(t, pool, &proof, contract.ProviderHolodex, contract.KindChannelStats, testChannelID, "holodex_metadata")
	publishConsumeStats(ctx, t, pool, repo, consumer, &proof, contract.ProviderYouTubeJS, 10)
	publishConsumeStats(ctx, t, pool, repo, consumer, &alt, contract.ProviderHolodex, 99)
	assertTableCount(t, pool, "youtube_channel_stats_snapshots", 0)

	var unresolved *time.Time

	if err := pool.QueryRow(ctx, `
		SELECT unresolved_scheduled_for FROM youtube_channel_stats_heads WHERE channel_id = 'UC_TEST'
	`).Scan(&unresolved); err != nil {
		t.Fatal(err)
	}

	if unresolved == nil || !unresolved.Equal(proof.ScheduledFor) {
		t.Fatalf("unresolved = %v", unresolved)
	}

	assertTableCount(t, pool, "source_reconciliation_conflicts", 1)
}

func TestChannelProfileAbsentFieldDoesNotClear(t *testing.T) {
	pool, repo, consumer, proof := startChannelPersist(t, contract.KindChannelProfile)
	ctx := t.Context()

	proof = publishConsumeProfile(ctx, t, pool, repo, consumer, &proof, present("hello"), absentField(), absentField())
	publishConsumeProfile(ctx, t, pool, repo, consumer, &proof, absentField(), present("KR"), absentField())

	var (
		description string
		set         bool
	)

	if err := pool.QueryRow(ctx, `
		SELECT description_set, description FROM youtube_channel_profile_heads WHERE channel_id = 'UC_TEST'
	`).Scan(&set, &description); err != nil {
		t.Fatal(err)
	}

	if !set || description != "hello" {
		t.Fatalf("absent field cleared description: set=%t value=%q", set, description)
	}
}

func TestChannelProfileExplicitEmptyRequiresStability(t *testing.T) {
	pool, repo, consumer, proof := startChannelPersist(t, contract.KindChannelProfile)
	ctx := t.Context()

	proof = publishConsumeProfile(ctx, t, pool, repo, consumer, &proof, present("hello"), absentField(), absentField())
	publishConsumeProfile(ctx, t, pool, repo, consumer, &proof, present(""), absentField(), absentField())

	var description string

	if err := pool.QueryRow(ctx, `
		SELECT description FROM youtube_channel_profile_heads WHERE channel_id = 'UC_TEST'
	`).Scan(&description); err != nil {
		t.Fatal(err)
	}

	if description != "hello" {
		t.Fatalf("disabled explicit empty cleared description: %q", description)
	}
}

func TestChannelProfileJoinedDateConflictIsRecorded(t *testing.T) {
	pool, repo, consumer, proof := startChannelPersist(t, contract.KindChannelProfile)
	ctx := t.Context()

	proof = publishConsumeProfile(ctx, t, pool, repo, consumer, &proof, absentField(), absentField(), present("2019-01-02"))
	publishConsumeProfile(ctx, t, pool, repo, consumer, &proof, absentField(), absentField(), present("2020-03-04"))

	var joined string

	if err := pool.QueryRow(ctx, `
		SELECT joined_date FROM youtube_channel_profile_heads WHERE channel_id = 'UC_TEST'
	`).Scan(&joined); err != nil {
		t.Fatal(err)
	}

	if joined != "2019-01-02" {
		t.Fatalf("joined date overwritten: %s", joined)
	}

	assertTableCount(t, pool, "source_reconciliation_conflicts", 1)
}

func TestChannelPhotoSameIdentityNewURLCreatesNoChangeEvent(t *testing.T) {
	pool, repo, consumer, proof := startChannelPersistPolicy(t, contract.KindChannelPhoto, ChannelPolicy{
		PhotoChangeMinObservations: 2, PhotoChangeStability: time.Nanosecond,
	})
	ctx := t.Context()
	first := photoVariant("https://img.test/a.jpg?s=88", 88, "media-1")

	proof = publishConsumePhoto(ctx, t, pool, repo, consumer, &proof, first)
	proof = publishConsumePhoto(ctx, t, pool, repo, consumer, &proof, first)
	publishConsumePhoto(ctx, t, pool, repo, consumer, &proof, photoVariant("https://img.test/a.jpg?s=800", 800, "media-1"))
	assertTableCount(t, pool, "youtube_channel_profiles", 1)

	var avatar string

	if err := pool.QueryRow(ctx, `SELECT avatar::text FROM youtube_channel_profiles WHERE channel_id = 'UC_TEST'`).Scan(&avatar); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(avatar, "s=800") {
		t.Fatalf("same identity URL wrote a change event: %s", avatar)
	}
}

func TestChannelPhotoWithoutIdentityCannotChangeCanonical(t *testing.T) {
	pool, repo, consumer, proof := startChannelPersistPolicy(t, contract.KindChannelPhoto, ChannelPolicy{
		PhotoChangeMinObservations: 2, PhotoChangeStability: time.Nanosecond,
	})
	ctx := t.Context()
	publishConsumePhoto(ctx, t, pool, repo, consumer, &proof, photoVariant("https://img.test/a.jpg?s=88", 88, ""))
	assertTableCount(t, pool, "youtube_channel_photo_variants", 1)
	assertTableCount(t, pool, "youtube_channel_profiles", 0)
}

func TestChannelPhotoDifferentIdentityRequiresStability(t *testing.T) {
	pool, repo, consumer, proof := startChannelPersist(t, contract.KindChannelPhoto)
	ctx := t.Context()

	proof = publishConsumePhoto(ctx, t, pool, repo, consumer, &proof, photoVariant("https://img.test/a.jpg", 88, "media-1"))
	publishConsumePhoto(ctx, t, pool, repo, consumer, &proof, photoVariant("https://img.test/b.jpg", 88, "media-2"))
	assertTableCount(t, pool, "youtube_channel_profiles", 0)
	assertTableCount(t, pool, "youtube_channel_photo_variants", 2)
}

func TestChannelPhotoCollectorDoesNotSynthesizeFingerprint(t *testing.T) {
	pool, repo, consumer, proof := startChannelPersist(t, contract.KindChannelPhoto)
	ctx := t.Context()
	publishConsumePhoto(ctx, t, pool, repo, consumer, &proof, photoVariant("https://img.test/a.jpg?keep=1", 88, ""))

	var fingerprint string

	if err := pool.QueryRow(ctx, `
		SELECT content_fingerprint FROM youtube_channel_photo_variants WHERE channel_id = 'UC_TEST'
	`).Scan(&fingerprint); err != nil {
		t.Fatal(err)
	}

	if fingerprint != "" {
		t.Fatalf("synthesized fingerprint = %q", fingerprint)
	}
}

func TestChannelConsumerProviderPermutationsYieldSameProjection(t *testing.T) {
	forward := projectStatsPermutation(t, contract.ProviderYouTubeJS, contract.ProviderHolodex)
	reverse := projectStatsPermutation(t, contract.ProviderHolodex, contract.ProviderYouTubeJS)

	if forward != reverse {
		t.Fatalf("stats permutation %q vs %q", forward, reverse)
	}
}

func seedAdditionalLease(
	t *testing.T,
	pool *pgxpool.Pool,
	existing *contract.LeaseProof,
	provider contract.Provider,
	kind contract.ObservationKind,
	subjectKey string,
	jobKind string,
) contract.LeaseProof {
	t.Helper()

	proof := *existing

	proof.JobKey = "job:" + jobKind + ":" + subjectKey
	proof.CollectionJobKind = jobKind

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_collection_targets (
			projection_generation, subject_key, observation_kind,
			priority, poll_interval_ms, enabled, valid_until
		) VALUES ($1, $2, $3, 50, 60000, TRUE, NOW() + INTERVAL '1 day')
		ON CONFLICT (projection_generation, subject_key, observation_kind) DO NOTHING
	`, proof.ProjectionGeneration, subjectKey, kind); err != nil {
		t.Fatalf("seed additional target: %v", err)
	}

	jobClass := "SUBJECT"

	if jobKind == "holodex_metadata" || jobKind == "official_schedule" {
		jobClass = "GLOBAL"
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_collection_job_leases (
			job_key, provider, job_class, collection_job_kind, subject_key,
			projection_generation, poll_interval_ms, slot_state, scheduled_for,
			next_due_at, fence_epoch, owner_instance, lease_expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, 60000, 'ACTIVE', $7, $7, $8, $9, NOW() + INTERVAL '1 hour')
	`, proof.JobKey, provider, jobClass, jobKind, subjectKey, proof.ProjectionGeneration,
		proof.ScheduledFor, proof.FenceEpoch, proof.OwnerInstance); err != nil {
		t.Fatalf("seed additional lease: %v", err)
	}

	return proof
}

func startChannelPersist(t *testing.T, kind contract.ObservationKind) (*pgxpool.Pool, *Repository, *Consumer, contract.LeaseProof) {
	t.Helper()

	return startChannelPersistPolicy(t, kind, ChannelPolicy{})
}

func startChannelPersistPolicy(
	t *testing.T,
	kind contract.ObservationKind,
	policy ChannelPolicy,
) (*pgxpool.Pool, *Repository, *Consumer, contract.LeaseProof) {
	t.Helper()

	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, kind, testChannelID, "youtubejs_channel_metadata")
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0).
		WithChannelPolicy(policy)

	return pool, repo, consumer, proof
}

func channelClaimOptions() ClaimOptions {
	return ClaimOptions{
		ConsumerName:  "youtube-channel-processor",
		LeaseOwner:    "api-a",
		Kinds:         []contract.ObservationKind{contract.KindChannelStats, contract.KindChannelProfile, contract.KindChannelPhoto},
		Limit:         10,
		LeaseDuration: 30 * time.Second,
	}
}

func publishConsumeStats(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	repo *Repository,
	consumer *Consumer,
	proof *contract.LeaseProof,
	provider contract.Provider,
	sub int64,
) contract.LeaseProof {
	t.Helper()

	views := int64(20)
	videos := int64(3)

	payload, err := contract.MarshalPayloadV1(contract.ChannelStatsV1{
		ChannelID: testChannelID, SubscriberCount: &sub, ViewCount: &views, VideoCount: &videos,
		Coverage: contract.ChannelStatsCoverageV1{
			ChannelID: testChannelID, Fields: []string{"subscriber_count", "view_count", "video_count"},
		},
	})
	if err != nil {
		t.Fatalf("marshal stats: %v", err)
	}

	publishConsumeEnvelope(ctx, t, repo, consumer, statsEnvelope(t, proof, provider, payload))

	return advanceLease(ctx, t, pool, proof, time.Hour)
}

func publishConsumeStatsHidden(
	ctx context.Context,
	t *testing.T,
	repo *Repository,
	consumer *Consumer,
	proof *contract.LeaseProof,
) {
	t.Helper()

	payload, err := contract.MarshalPayloadV1(contract.ChannelStatsV1{
		ChannelID: testChannelID,
		Coverage: contract.ChannelStatsCoverageV1{
			ChannelID: testChannelID, Fields: []string{"subscriber_count", "view_count", "video_count"},
		},
	})
	if err != nil {
		t.Fatalf("marshal hidden stats: %v", err)
	}

	publishConsumeEnvelope(ctx, t, repo, consumer, statsEnvelope(t, proof, contract.ProviderYouTubeJS, payload))
}

func publishConsumeProfile(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	repo *Repository,
	consumer *Consumer,
	proof *contract.LeaseProof,
	description, country, joined contract.FieldValue[string],
) contract.LeaseProof {
	t.Helper()

	fields := make([]string, 0, 3)

	if description.Present {
		fields = append(fields, "description")
	}

	if country.Present {
		fields = append(fields, "country")
	}

	if joined.Present {
		fields = append(fields, "joined_date")
	}

	payload, err := contract.MarshalPayloadV1(contract.ChannelProfileV1{
		ChannelID: testChannelID, Description: description, Country: country, JoinedDate: joined,
		Coverage: contract.ChannelProfileCoverageV1{ChannelID: testChannelID, Fields: fields},
	})
	if err != nil {
		t.Fatalf("marshal profile: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindChannelProfile, SubjectKey: testChannelID,
		SchemaVersion: contract.SchemaVersionV1, ContractGeneration: 1,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous,
		Payload: payload, CollectorInstance: proof.OwnerInstance, Lease: *proof,
	})
	if err != nil {
		t.Fatalf("prepare profile: %v", err)
	}

	publishConsumeEnvelope(ctx, t, repo, consumer, &envelope)

	return advanceLease(ctx, t, pool, proof, time.Hour)
}

func publishConsumePhoto(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	repo *Repository,
	consumer *Consumer,
	proof *contract.LeaseProof,
	variant *contract.PhotoVariantV1,
) contract.LeaseProof {
	t.Helper()

	payload, err := contract.MarshalPayloadV1(contract.ChannelPhotoV1{
		ChannelID: testChannelID,
		Variants:  []contract.PhotoVariantV1{*variant},
		Coverage:  contract.ChannelPhotoCoverageV1{ChannelID: testChannelID, Variants: []string{variant.Kind}},
	})
	if err != nil {
		t.Fatalf("marshal photo: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindChannelPhoto, SubjectKey: testChannelID,
		SchemaVersion: contract.SchemaVersionV1, ContractGeneration: 1,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous,
		Payload: payload, CollectorInstance: proof.OwnerInstance, Lease: *proof,
	})
	if err != nil {
		t.Fatalf("prepare photo: %v", err)
	}

	publishConsumeEnvelope(ctx, t, repo, consumer, &envelope)

	return advanceLease(ctx, t, pool, proof, time.Hour)
}

func publishConsumeEnvelope(ctx context.Context, t *testing.T, repo *Repository, consumer *Consumer, envelope *contract.Envelope) {
	t.Helper()

	if _, err := repo.PublishBatch(ctx, publishInput(envelope)); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if err := consumer.Consume(ctx, channelClaimOptions()); err != nil {
		t.Fatalf("consume: %v", err)
	}
}

func statsEnvelope(t *testing.T, proof *contract.LeaseProof, provider contract.Provider, payload []byte) *contract.Envelope {
	t.Helper()

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: provider, ObservationKind: contract.KindChannelStats, SubjectKey: testChannelID,
		SchemaVersion: contract.SchemaVersionV1, ContractGeneration: 1,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous,
		Payload: payload, CollectorInstance: proof.OwnerInstance, Lease: *proof,
	})
	if err != nil {
		t.Fatalf("prepare stats: %v", err)
	}

	return &envelope
}

func jobKindFor(provider contract.Provider) string {
	if provider == contract.ProviderHolodex {
		return "holodex_metadata"
	}

	return "youtubejs_channel_metadata"
}

func present(value string) contract.FieldValue[string] {
	return contract.FieldValue[string]{Present: true, Value: value}
}

func absentField() contract.FieldValue[string] {
	return contract.FieldValue[string]{}
}

func photoVariant(rawURL string, size int, mediaID string) *contract.PhotoVariantV1 {
	return &contract.PhotoVariantV1{Kind: "avatar", URL: rawURL, Width: size, Height: size, StableMediaID: mediaID}
}

func projectStatsPermutation(t *testing.T, first, second contract.Provider) string {
	t.Helper()

	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)
	ctx := t.Context()
	firstProof := seedPublishLease(t.Context(), t, pool, first, contract.KindChannelStats, testChannelID, jobKindFor(first))
	secondProof := seedAdditionalLease(t, pool, &firstProof, second, contract.KindChannelStats, testChannelID, jobKindFor(second))
	publishConsumeStats(ctx, t, pool, repo, consumer, &firstProof, first, 10)
	publishConsumeStats(ctx, t, pool, repo, consumer, &secondProof, second, 10)

	var latest string

	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(last_resolved_subscriber_count::text, 'nil') || '/' ||
		       COALESCE(unresolved_scheduled_for::text, 'ok')
		FROM youtube_channel_stats_heads WHERE channel_id = 'UC_TEST'
	`).Scan(&latest); err != nil {
		t.Fatal(err)
	}

	return latest
}
