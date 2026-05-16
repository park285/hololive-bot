package polling

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func TestShouldEnqueueRoutedNotification(t *testing.T) {
	t.Parallel()

	publishedAt := time.Date(2026, 4, 10, 10, 11, 12, 0, time.FixedZone("KST", 9*60*60))
	captured := NotificationRouteRequest{}
	got := shouldEnqueueRoutedNotification(func(req NotificationRouteRequest) bool {
		captured = req
		return req.ChannelID == "UCtarget" && req.AlarmType == domain.AlarmTypeShorts
	}, domain.AlarmTypeShorts, "UCtarget", publishedAt)

	assert.True(t, got)
	assert.Equal(t, NotificationRouteRequest{
		AlarmType:   domain.AlarmTypeShorts,
		ChannelID:   "UCtarget",
		PublishedAt: yttimestamp.Normalize(publishedAt),
	}, captured)
	assert.True(t, shouldEnqueueRoutedNotification(nil, domain.AlarmTypeCommunity, "UCfallback", time.Time{}))
}
