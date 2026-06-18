package chzzk

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/notification"
)

func TestNewChzzkCheckerValidation(t *testing.T) {
	t.Parallel()

	_, err := NewChzzkChecker(nil, &chzzk.Client{}, newCheckerTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache service is nil")

	cache := newCheckerTestCacheClient(t)

	_, err = NewChzzkChecker(cache, nil, newCheckerTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chzzk client is nil")
}

func TestChzzkCheckerCheck_EmptyMappings(t *testing.T) {
	t.Parallel()

	cache := newCheckerTestCacheClient(t)
	checker, err := NewChzzkChecker(
		cache,
		chzzk.NewClient(&http.Client{Timeout: time.Second}, chzzk.DefaultBaseURL, newCheckerTestLogger()),
		newCheckerTestLogger(),
	)
	require.NoError(t, err)

	notifications, checkErr := checker.Check(t.Context())
	require.NoError(t, checkErr)
	assert.Empty(t, notifications)
}

func TestChzzkHelperFunctions(t *testing.T) {
	t.Parallel()

	assert.False(t, isChzzkLive(nil))
	assert.True(t, isChzzkLive(&chzzk.LiveStatusContent{Status: "OPEN"}))
	assert.True(t, isChzzkLive(&chzzk.LiveStatusContent{Status: "open"}))
	assert.False(t, isChzzkLive(&chzzk.LiveStatusContent{Status: "CLOSE"}))

	detected := time.Date(2026, time.March, 5, 2, 4, 59, 0, time.UTC)
	assert.Equal(
		t,
		notification.ChzzkLiveNotifiedKeyPrefix+"chzzk1:20260305T0200",
		buildChzzkLiveDedupKey("chzzk1", detected),
	)

	stream := buildChzzkLiveStream("yt1", "chzzk1", "", nil, detected)
	require.NotNil(t, stream)
	assert.Equal(t, domain.StreamStatusLive, stream.Status)
	assert.Equal(t, "치지직 라이브", stream.Title)
	assert.Equal(t, "yt1", stream.ChannelID)
	assert.Equal(t, "yt1", stream.ChannelName)
	assert.True(t, stream.IsChzzkOnly)
	assert.Equal(t, "https://chzzk.naver.com/live/chzzk1", stream.ChzzkLiveURL)

	status := &chzzk.LiveStatusContent{
		Status:              "OPEN",
		LiveTitle:           "  치지직 타이틀  ",
		LiveCategoryValue:   "게임",
		ConcurrentUserCount: 777,
	}

	stream = buildChzzkLiveStream("yt2", "chzzk2", "라덴", status, detected)
	require.NotNil(t, stream)
	require.NotNil(t, stream.ViewerCount)
	assert.Equal(t, 777, *stream.ViewerCount)
	assert.Equal(t, "치지직 타이틀", stream.Title)
	assert.Equal(t, "라덴", stream.ChannelName)
}
