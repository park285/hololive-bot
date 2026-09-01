package youtubedispatch

import (
	"testing"
	"time"

	dispatchstate "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

// withFixedSentAtNow remains shared by tests for the upstream claim-state clock.
// Canonical delivery timestamps are now owned and tested by TransitionStore.
func withFixedSentAtNow(t *testing.T, fixed time.Time) {
	t.Helper()

	original := dispatchstate.SentAtNow

	dispatchstate.SentAtNow = func() time.Time {
		return fixed
	}

	t.Cleanup(func() {
		dispatchstate.SentAtNow = original
	})
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
