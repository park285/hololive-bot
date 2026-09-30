package notifier

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestUpcomingNotificationExpiresBeforePublish(t *testing.T) {
	now := time.Now()
	n := domain.NewAlarmNotification("room", &domain.Channel{ID: "channel"}, &domain.Stream{ID: "stream", ChannelID: "channel", Status: domain.StreamStatusUpcoming, StartScheduled: new(now)}, 5, nil, "")
	require.Nil(t, resolveSendInput(n, now))
	require.NotNil(t, resolveSendInput(n, now.Add(-time.Second)))

	n.Stream.Status = domain.StreamStatusLive
	require.NotNil(t, resolveSendInput(n, now))
}
