package youtubedispatch

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

type failingChunkSender struct {
	youtubeOutboxKaringTestSender

	failAt int
	err    error
	ids    []string
}

func (s *failingChunkSender) SendYouTubeOutboxKaring(ctx context.Context, room string, req *iris.KaringContentListRequest) error {
	s.ids = append(s.ids, *req.ClientRequestID)
	if len(s.ids) == s.failAt {
		return s.err
	}

	return s.youtubeOutboxKaringTestSender.SendYouTubeOutboxKaring(ctx, room, req)
}

func TestKaringChunksPersistIndependentOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want domain.OutboxStatus
	}{
		{name: "known failure", err: iris.ErrPermanent, want: domain.OutboxStatusFailed},
		{name: "unknown", err: egress.ErrKaringOutcomeUnknown, want: store.DeliveryStatusSending},
		{name: "timeout", err: context.DeadlineExceeded, want: store.DeliveryStatusSending},
	} {
		for failureAt := 1; failureAt <= 3; failureAt++ {
			t.Run(fmt.Sprintf("%s/chunk-%d", tc.name, failureAt), func(t *testing.T) {
				db := newDeliveryPool(t)
				sender := &failingChunkSender{failAt: failureAt, err: tc.err}
				d := newDispatcherForTest(t, db, cachemocks.NewLenientClient(), sender, nil,
					slog.New(slog.DiscardHandler), &dispatchstate.Config{BatchSize: 20, DeliverySendTimeout: time.Second})
				now := time.Now().UTC().Truncate(time.Microsecond)
				ids := make([]int64, 0, 9)

				for i := range 9 {
					contentID := fmt.Sprintf("video%06d", i)
					outbox := domain.YouTubeNotificationOutbox{
						Kind: domain.OutboxKindNewVideo, ChannelID: "UC_chunk_fixture", ContentID: contentID,
						Payload: fmt.Sprintf(`{"video_id":%q,"title":"synthetic"}`, contentID),
						Status:  domain.OutboxStatusPending, CreatedAt: now, NextAttemptAt: now,
					}
					require.NoError(t, insertDeliveryTestRows(db, &outbox).Error)

					row := domain.YouTubeNotificationDelivery{
						OutboxID: outbox.ID, RoomID: testRoomOne, Status: domain.OutboxStatusPending,
						CreatedAt: now, NextAttemptAt: now,
					}
					require.NoError(t, insertDeliveryTestRows(db, &row).Error)

					ids = append(ids, row.ID)
				}

				require.Equal(t, 9, d.claim.processPendingDeliveries(t.Context()))
				require.Len(t, sender.ids, failureAt)

				for i, id := range ids {
					var row deliveryTestDeliveryModel

					require.NoError(t, firstDeliveryTestRow(db, &row, id).Error)

					chunk := i/4 + 1
					want := tc.want

					if chunk < failureAt {
						want = domain.OutboxStatusSent
					} else if chunk > failureAt {
						want = domain.OutboxStatusPending
					}

					require.Equal(t, string(want), row.Status, "item %d", i)

					if want == store.DeliveryStatusSending {
						require.NotNil(t, row.LockedAt)
					} else {
						require.Nil(t, row.LockedAt)
					}
				}
			})
		}
	}
}

func TestKaringChunkIdentitySurvivesEarlierChunkCompletion(t *testing.T) {
	engine, _ := newOutcomeUnknownTestEngine(&youtubeOutboxKaringTestSender{}, nil, time.Second)
	boxes := make([]domain.YouTubeNotificationOutbox, 0, 8)

	for i := range 8 {
		boxes = append(boxes, domain.YouTubeNotificationOutbox{
			ID: int64(i + 1), Kind: domain.OutboxKindNewVideo, ChannelID: "UC_chunk_fixture",
			ContentID: fmt.Sprintf("video%06d", i), Payload: `{"title":"synthetic"}`,
		})
	}

	plan, err := engine.formatter.planKaringChunks(t.Context(), testRoomOne, "UC_chunk_fixture", domain.OutboxKindNewVideo, boxes)
	require.NoError(t, err)

	retry, err := engine.formatter.planKaringChunks(t.Context(), testRoomOne, "UC_chunk_fixture", domain.OutboxKindNewVideo, boxes[4:])
	require.NoError(t, err)
	require.Equal(t, plan[1].clientRequestID, retry[0].clientRequestID)

	boxes[4].Payload = `{"title":"changed"}`

	changed, err := engine.formatter.planKaringChunks(t.Context(), testRoomOne, "UC_chunk_fixture", domain.OutboxKindNewVideo, boxes[4:])
	require.NoError(t, err)
	require.NotEqual(t, retry[0].clientRequestID, changed[0].clientRequestID)
}
