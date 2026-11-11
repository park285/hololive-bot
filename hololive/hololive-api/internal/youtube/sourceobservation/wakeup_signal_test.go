package sourceobservation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/wakeup"
	"github.com/kapu/hololive-shared/pkg/testutil"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

// alarm-worker YouTube consumer와 맞춘 신호 queue key다.
const youtubeWakeupQueue = "youtube:delivery:wakeup"

type finalizeResultStub struct {
	result ReconcileResult
	err    error
}

func (finalizeResultStub) ClaimBatch(context.Context, ClaimOptions) (ClaimedBatch, error) {
	return ClaimedBatch{}, nil
}

func (s finalizeResultStub) Finalize(context.Context, Claim, ReconcileWrite) (ReconcileResult, error) {
	return s.result, s.err
}

func TestConsumeClaimSignalsOnlyAfterCommittedNotifications(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		stub   finalizeResultStub
		tokens int
	}{
		{name: "committed notifications", stub: finalizeResultStub{result: ReconcileResult{NotificationsQueued: true}}, tokens: 1},
		{name: "rolled back", stub: finalizeResultStub{result: ReconcileResult{NotificationsQueued: true}, err: errors.New("finalize failed")}},
		{name: "no notifications", stub: finalizeResultStub{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, mini := testutil.NewTestCacheServiceWithMini(t.Context(), t)
			consumer := NewConsumerWithGraces(tc.stub, 0, 0).WithWakeup(wakeup.NewSignaler(client, wakeup.YouTubeDelivery, nil))

			err := consumer.ConsumeClaim(t.Context(), Claim{})
			if (err != nil) != (tc.stub.err != nil) {
				t.Fatalf("ConsumeClaim() error = %v, want error %v", err, tc.stub.err != nil)
			}

			got, err := mini.List(youtubeWakeupQueue)
			if err != nil && !errors.Is(err, miniredis.ErrKeyNotFound) {
				t.Fatalf("List(%s) error = %v", youtubeWakeupQueue, err)
			}

			if len(got) != tc.tokens {
				t.Fatalf("wakeup tokens = %d, want %d", len(got), tc.tokens)
			}
		})
	}
}

func TestContentConsumerSignalsOnlyWhenNotificationRowIsQueued(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)

	client, mini := testutil.NewTestCacheServiceWithMini(ctx, t)
	consumer := NewConsumerWithGraces(NewRepository(pool), 0, 0).WithWakeup(wakeup.NewSignaler(client, wakeup.YouTubeDelivery, nil))
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")

	if _, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(videoListEnvelope(t, &proof, contract.CompletenessComplete, testVideoID))); err != nil {
		t.Fatalf("publish positive: %v", err)
	}

	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume positive: %v", err)
	}

	assertTableCount(t, pool, "youtube_notification_outbox", 1)

	if got, err := mini.List(youtubeWakeupQueue); err != nil || len(got) != 1 {
		t.Fatalf("wakeup after new video notification = %v, %v; want one token", got, err)
	}

	mini.FlushAll()

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)

	if _, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(videoListEnvelope(t, &proof, contract.CompletenessComplete))); err != nil {
		t.Fatalf("publish negative: %v", err)
	}

	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume negative: %v", err)
	}

	if mini.Exists(youtubeWakeupQueue) {
		t.Fatal("absence observation without notification signaled the consumer")
	}
}

func TestCommunityConsumerSignalsAfterNotificationCommit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedMixedCommunityWatermark(ctx, t, pool)

	client, mini := testutil.NewTestCacheServiceWithMini(ctx, t)
	consumer := NewConsumerWithGraces(NewRepository(pool), 0, 0).WithWakeup(wakeup.NewSignaler(client, wakeup.YouTubeDelivery, nil))
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

	consumeCommunityPost := func(postID string) {
		t.Helper()

		if _, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, postID))); err != nil {
			t.Fatalf("publish %s: %v", postID, err)
		}

		if err := consumer.Consume(ctx, claimOptions()); err != nil {
			t.Fatalf("consume %s: %v", postID, err)
		}
	}

	// 채널의 첫 관측은 기준선이라 알림 행이 없으므로 깨우지 않는다.
	consumeCommunityPost("post-base")
	assertTableCount(t, pool, "youtube_notification_outbox", 0)

	if mini.Exists(youtubeWakeupQueue) {
		t.Fatal("baseline community observation signaled the consumer")
	}

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)

	consumeCommunityPost("post-new")
	assertTableCount(t, pool, "youtube_notification_outbox", 1)

	if got, err := mini.List(youtubeWakeupQueue); err != nil || len(got) != 1 {
		t.Fatalf("wakeup after community notification = %v, %v; want one token", got, err)
	}
}
