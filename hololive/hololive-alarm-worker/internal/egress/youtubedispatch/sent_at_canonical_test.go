package youtubedispatch

import (
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dispatchstate "github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

// newFixedClockDispatcherForTest는 outbox 집계 sent_at 시계를 고정한 dispatcher를 만든다.
// 전송 시각 검증은 TransitionStore가 맡는다.
func newFixedClockDispatcherForTest(
	tb testing.TB,
	db *pgxpool.Pool,
	sender delivery.PreparedMessageSender,
	fixed time.Time,
	config *dispatchstate.Config,
) *Dispatcher {
	tb.Helper()

	return newDispatcherWithDepsForTest(tb, db, Dependencies{
		Cache:  cachemocks.NewLenientClient(),
		Sender: sender,
		Now:    func() time.Time { return fixed },
	}, slog.New(slog.DiscardHandler), config)
}

type deliveryTestTrackingModel struct {
	Kind                        string `db:"kind"`
	ContentID                   string `db:"content_id"`
	CanonicalContentID          string
	ChannelID                   string `db:"channel_id"`
	ActualPublishedAt           *time.Time
	DetectedAt                  time.Time `db:"detected_at"`
	AlarmSentAt                 *time.Time
	AlarmLatencyMillis          *int64
	AlarmLatencyExceeded        *bool
	DeliveryStatus              string `db:"delivery_status"`
	LatencyClassificationStatus string
	DelaySource                 string
	InternalDelayCause          string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

func (deliveryTestTrackingModel) TableName() string {
	return testTableContentAlarmTracking
}
