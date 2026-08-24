package parser

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractWatchLiveMetadata(t *testing.T) {
	premiereHTML := `<script>var ytInitialPlayerResponse = {"videoDetails":{"isUpcoming":true,"isLiveContent":false},"microformat":{"playerMicroformatRenderer":{"liveBroadcastDetails":{"isLiveNow":false,"startTimestamp":"2026-08-02T12:00:00+00:00"}}}};</script>`
	liveHTML := `<script>var ytInitialPlayerResponse = {"videoDetails":{"isUpcoming":true,"isLiveContent":true},"microformat":{"playerMicroformatRenderer":{"liveBroadcastDetails":{"isLiveNow":false,"startTimestamp":"2027-03-20T12:20:00+00:00"}}}};</script>`

	got := ExtractWatchLiveMetadata(premiereHTML)
	assert.Equal(t, LiveContentFalse, got.LiveContent)
	require.NotNil(t, got.StartTimestamp)
	assert.Equal(t, time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC), got.StartTimestamp.UTC())

	got = ExtractWatchLiveMetadata(liveHTML)
	assert.Equal(t, LiveContentTrue, got.LiveContent)

	got = ExtractWatchLiveMetadata("<html>no player response</html>")
	assert.Equal(t, LiveContentUnknown, got.LiveContent)
	assert.Nil(t, got.StartTimestamp)
}

func TestExtractWatchLiveMetadataIgnoresUnrelatedPageJSON(t *testing.T) {
	html := `<script>var unrelated = {"isLiveContent":true,"startTimestamp":"2025-01-01T00:00:00Z"};</script>
		<script>var ytInitialPlayerResponse = {"videoDetails":{"isUpcoming":true,"isLiveContent":false}};</script>`

	got := ExtractWatchLiveMetadata(html)

	assert.Equal(t, LiveContentFalse, got.LiveContent)
	assert.Nil(t, got.StartTimestamp)
}

func TestExtractWatchLiveMetadataAdversarialShapesFailSafe(t *testing.T) {
	truncated := `<script>var ytInitialPlayerResponse = {"videoDetails":{"isUpcoming":true,"isLiveContent":`

	got := ExtractWatchLiveMetadata(truncated)
	assert.Equal(t, LiveContentUnknown, got.LiveContent)
	assert.Nil(t, got.StartTimestamp)

	bracesInsideString := `<script>var ytInitialPlayerResponse = {"videoDetails":{"title":"\"}{","isLiveContent":true}};</script>`

	got = ExtractWatchLiveMetadata(bracesInsideString)
	assert.Equal(t, LiveContentTrue, got.LiveContent)
}

func TestExtractVideoMetadataFromHTMLClassifiesReplayAndPublication(t *testing.T) {
	html := `<meta itemprop="uploadDate" content="2026-07-12T10:00:00Z"><script>var ytInitialPlayerResponse={"videoDetails":{"isLiveContent":true},"microformat":{"playerMicroformatRenderer":{"liveBroadcastDetails":{}}}};</script>`

	metadata := ExtractVideoMetadataFromHTML(html)

	require.NotNil(t, metadata.PublishedAt)
	assert.Equal(t, time.Date(2026, time.July, 12, 10, 0, 0, 0, time.UTC), *metadata.PublishedAt)
	assert.Equal(t, ReplayStatusReplay, metadata.Replay)
}

func TestDetectReplayStatusRequiresConclusiveWatchMetadata(t *testing.T) {
	tests := []struct {
		name string
		html string
		want ReplayStatus
	}{
		{name: "ordinary upload", html: `{"videoDetails":{"isLiveContent":false}}`, want: ReplayStatusNotReplay},
		{name: "live content", html: `{"videoDetails":{"isLiveContent":true}}`, want: ReplayStatusReplay},
		{name: "premiere details", html: `{"liveBroadcastDetails":{"startTimestamp":"2026-07-12T10:00:00Z"}}`, want: ReplayStatusReplay},
		{name: "meta broadcast", html: `<meta content="True" itemprop="isLiveBroadcast">`, want: ReplayStatusReplay},
		{name: "unknown", html: `<meta itemprop="uploadDate" content="2026-07-12T10:00:00Z">`, want: ReplayStatusUnknown},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, DetectReplayStatus(test.html))
		})
	}
}
