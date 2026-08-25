package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func TestCommunityWindowBaselinePreventsPinnedPostReorderBurst(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	consumer := NewConsumer(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil)

	baseline := publishCommunityWindow(ctx, t, repo, &proof, "pinned", "known-a", "known-b")
	consumeCommunityWindow(ctx, t, consumer)
	assertCommunityWindowOutboxIDs(ctx, t, pool)
	assertCommunityWindowPostCount(ctx, t, pool, 3)
	assertCommunityWindowMarker(ctx, t, pool, baseline)

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)
	publishCommunityWindow(ctx, t, repo, &proof, "known-a", "new-a", "known-b")
	consumeCommunityWindow(ctx, t, consumer)
	assertCommunityWindowOutboxIDs(ctx, t, pool, "community:new-a")
	assertCommunityWindowPostCount(ctx, t, pool, 4)

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)
	publishCommunityWindow(ctx, t, repo, &proof, "known-a", "new-b", "new-a")
	consumeCommunityWindow(ctx, t, consumer)
	assertCommunityWindowOutboxIDs(ctx, t, pool, "community:new-a", "community:new-b")
}

func bootstrapCommunityWindow(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	repo *Repository,
	consumer *Consumer,
	proof contract.LeaseProof,
) contract.LeaseProof {
	t.Helper()

	publishCommunityWindow(ctx, t, repo, &proof, "baseline-post")
	consumeCommunityWindow(ctx, t, consumer)
	assertCommunityWindowOutboxIDs(ctx, t, pool)

	return advanceLease(ctx, t, pool, &proof, time.Minute)
}

func publishCommunityWindow(
	ctx context.Context,
	t *testing.T,
	repo *Repository,
	proof *contract.LeaseProof,
	postIDs ...string,
) int64 {
	t.Helper()

	envelope := communityWindowEnvelope(t, proof, postIDs...)

	published, err := repo.PublishBatch(ctx, publishInput(envelope))
	if err != nil {
		t.Fatalf("publish community window: %v", err)
	}

	return published.Results[0].ObservationID
}

func communityWindowEnvelope(t *testing.T, proof *contract.LeaseProof, postIDs ...string) *contract.Envelope {
	t.Helper()

	if len(postIDs) == 0 {
		t.Fatal("community window requires at least one post")

		return nil
	}

	var payload contract.CommunityPayloadV1

	envelope := communityEnvelope(t, proof, postIDs[0])

	if err := jsonv2.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatalf("decode community window payload: %v", err)
	}

	payload.Posts = make([]contract.CommunityPostV1, 0, len(postIDs))
	for _, postID := range postIDs {
		payload.Posts = append(payload.Posts, contract.CommunityPostV1{PostID: postID, ChannelID: testChannelID})
	}

	payload.Coverage.Exhausted = false

	encoded, err := contract.MarshalPayloadV1(payload)
	if err != nil {
		t.Fatalf("encode community window payload: %v", err)
	}

	envelope.Payload = encoded
	envelope.Completeness = contract.CompletenessPartial
	envelope.Continuity = contract.ContinuityGapUnresolved

	prepared, err := contract.PrepareEnvelope(*envelope)
	if err != nil {
		t.Fatalf("prepare community window envelope: %v", err)
	}

	return &prepared
}

func consumeCommunityWindow(ctx context.Context, t *testing.T, consumer *Consumer) {
	t.Helper()

	if err := consumer.Consume(ctx, claimOptions()); err != nil {
		t.Fatalf("consume community window: %v", err)
	}
}

func assertCommunityWindowOutboxIDs(ctx context.Context, t *testing.T, pool *pgxpool.Pool, want ...string) {
	t.Helper()

	rows, err := pool.Query(ctx, `
		SELECT content_id
		FROM youtube_notification_outbox
		WHERE kind = 'COMMUNITY_POST'
		ORDER BY content_id
	`)
	if err != nil {
		t.Fatalf("load community outbox ids: %v", err)
	}
	defer rows.Close()

	got := make([]string, 0, len(want))

	for rows.Next() {
		var contentID string

		if err := rows.Scan(&contentID); err != nil {
			t.Fatalf("scan community outbox id: %v", err)
		}

		got = append(got, contentID)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("load community outbox ids: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("community outbox ids = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("community outbox ids = %v, want %v", got, want)
		}
	}
}

func assertCommunityWindowPostCount(ctx context.Context, t *testing.T, pool *pgxpool.Pool, want int) {
	t.Helper()

	var count int

	if err := pool.QueryRow(ctx, `SELECT count(*) FROM youtube_community_posts WHERE channel_id = $1`, testChannelID).Scan(&count); err != nil {
		t.Fatalf("count community posts: %v", err)
	}

	if count != want {
		t.Fatalf("community post count = %d, want %d", count, want)
	}
}

func assertCommunityWindowMarker(ctx context.Context, t *testing.T, pool *pgxpool.Pool, observationID int64) {
	t.Helper()

	var count int

	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM source_observation_applications
		WHERE observation_id = $1
		  AND entity_kind = 'community_window'
		  AND entity_key = $2
		  AND decision = 'CANONICALIZED'
	`, observationID, testChannelID).Scan(&count); err != nil {
		t.Fatalf("load community window marker: %v", err)
	}

	if count != 1 {
		t.Fatalf("community window marker count = %d, want 1", count)
	}
}
