package parser

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVideosFromRSSFeed_HappyPath(t *testing.T) {
	raw, err := os.ReadFile("testdata/feed.xml")
	require.NoError(t, err)

	videos, err := ParseVideosFromRSSFeed(string(raw), "UC_FEED", 10)
	require.NoError(t, err)
	require.Len(t, videos, 2)

	assert.Equal(t, "feed_001", videos[0].VideoID)
	assert.Equal(t, "Feed First", videos[0].Title)
	assert.Equal(t, "UC_FEED", videos[0].ChannelID)
	assert.Equal(t, "Feed Channel", videos[0].ChannelTitle)
	assert.Equal(t, "2026-03-01T09:08:07Z", videos[0].PublishedText)
	require.Len(t, videos[0].Thumbnail, 1)
	assert.Equal(t, "https://i.ytimg.com/vi/feed_001/hqdefault.jpg", videos[0].Thumbnail[0].URL)
	assert.Equal(t, 480, videos[0].Thumbnail[0].Width)

	assert.Equal(t, "feed_002", videos[1].VideoID)
	assert.Equal(t, "2026-03-01T16:02:03Z", videos[1].PublishedText)
	require.Len(t, videos[1].Thumbnail, 1)
}

func TestParseVideosFromRSSFeed_MaxResultsLimit(t *testing.T) {
	raw, err := os.ReadFile("testdata/feed.xml")
	require.NoError(t, err)

	videos, err := ParseVideosFromRSSFeed(string(raw), "UC_FEED", 1)
	require.NoError(t, err)
	require.Len(t, videos, 1)
	assert.Equal(t, "feed_001", videos[0].VideoID)
}

func TestParseVideosFromRSSFeed_SkipsInvalidAndDuplicateEntries(t *testing.T) {
	feed := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015">
  <entry><yt:videoId>v1</yt:videoId><title>First</title></entry>
  <entry><yt:videoId></yt:videoId><title>No ID</title></entry>
  <entry><yt:videoId>v2</yt:videoId><title></title></entry>
  <entry><yt:videoId>v1</yt:videoId><title>Duplicate</title></entry>
  <entry><yt:videoId>v3</yt:videoId><title>Third</title></entry>
</feed>`
	videos, err := ParseVideosFromRSSFeed(feed, "UC_FEED", 10)
	require.NoError(t, err)
	require.Len(t, videos, 2)
	assert.Equal(t, "v1", videos[0].VideoID)
	assert.Equal(t, "First", videos[0].Title)
	assert.Equal(t, "v3", videos[1].VideoID)
}

func TestParseVideosFromRSSFeed_MediaThumbsFallback(t *testing.T) {
	feed := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/">
  <entry>
    <yt:videoId>tv1</yt:videoId>
    <title>Thumb Video</title>
    <media:thumbnail url="https://i/thumb.jpg" width="200" height="150" />
  </entry>
</feed>`
	videos, err := ParseVideosFromRSSFeed(feed, "UC_FEED", 10)
	require.NoError(t, err)
	require.Len(t, videos, 1)
	require.Len(t, videos[0].Thumbnail, 1)
	assert.Equal(t, "https://i/thumb.jpg", videos[0].Thumbnail[0].URL)
	assert.Equal(t, 200, videos[0].Thumbnail[0].Width)
}

func TestParseVideosFromRSSFeed_NonRFC3339PublishedKeptVerbatim(t *testing.T) {
	feed := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015">
  <entry><yt:videoId>p1</yt:videoId><title>Odd Date</title><published>not a date</published></entry>
</feed>`
	videos, err := ParseVideosFromRSSFeed(feed, "UC_FEED", 10)
	require.NoError(t, err)
	require.Len(t, videos, 1)
	assert.Equal(t, "not a date", videos[0].PublishedText)
}

func TestParseVideosFromRSSFeed_MalformedXMLReturnsWrappedError(t *testing.T) {
	videos, err := ParseVideosFromRSSFeed("<feed><entry", "UC_FEED", 10)
	require.Error(t, err)
	assert.Nil(t, videos)
	assert.Contains(t, err.Error(), "parse rss feed xml:")
}

func TestParseVideosFromRSSFeed_BlankInput(t *testing.T) {
	videos, err := ParseVideosFromRSSFeed("   ", "UC_FEED", 10)
	require.NoError(t, err)
	assert.Empty(t, videos)
	assert.NotNil(t, videos)
}

func TestParseVideosFromRSSFeed_MaxResultsZero(t *testing.T) {
	feed := `<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015"><entry><yt:videoId>v1</yt:videoId><title>One</title></entry></feed>`
	videos, err := ParseVideosFromRSSFeed(feed, "UC_FEED", 0)
	require.NoError(t, err)
	assert.Empty(t, videos)
	assert.NotNil(t, videos)
}

func TestParseVideosFromRSSFeed_EmptyFeed(t *testing.T) {
	videos, err := ParseVideosFromRSSFeed("<feed></feed>", "UC_FEED", 10)
	require.NoError(t, err)
	assert.Empty(t, videos)
}
