package twitch

import (
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

const (
	testTwitchLogin      = "aqua"
	testTwitchChannelID  = "ch1"
	testTwitchStreamType = "live"
	testTwitchRoomID     = "room-1"
	testTwitchStreamID   = "stream-1"
	testTwitchUserID     = "user-1"
	testTwitchYouTubeID  = "yt-1"
)

func newCheckerTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newCheckerTestCacheClient(t *testing.T) cache.Client {
	t.Helper()

	return testutil.NewTestCacheService(t.Context(), t)
}
