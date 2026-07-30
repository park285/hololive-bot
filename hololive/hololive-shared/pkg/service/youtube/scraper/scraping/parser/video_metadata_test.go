package parser

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractVideoMetadataFromHTMLClassifiesReplayAndPublication(t *testing.T) {
	html := `<meta itemprop="uploadDate" content="2026-07-12T10:00:00Z"><script>var ytInitialPlayerResponse={"videoDetails":{"isLiveContent":true},"microformat":{"playerMicroformatRenderer":{"liveBroadcastDetails":{}}}};</script>`

	metadata := ExtractVideoMetadataFromHTML(html)

	require.NotNil(t, metadata.PublishedAt)
	assert.Equal(t, time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC), *metadata.PublishedAt)
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
