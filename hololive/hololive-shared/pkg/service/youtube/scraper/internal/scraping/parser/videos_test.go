package parser

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestParseVideosFromInitialData_HappyPath(t *testing.T) {
	raw, err := os.ReadFile("testdata/videos_tab.json")
	require.NoError(t, err)

	videos, err := ParseVideosFromInitialData(gjson.ParseBytes(raw), "UC_X", 10, testVideoParser)
	require.NoError(t, err)
	require.Len(t, videos, 2)
	assert.Equal(t, "vid_aaa", videos[0].VideoID)
	assert.Equal(t, "First Video", videos[0].Title)
	assert.Equal(t, "UC_X", videos[0].ChannelID)
	assert.Equal(t, "vid_bbb", videos[1].VideoID)
}

func TestParseVideosFromInitialData_MaxResultsLimit(t *testing.T) {
	raw, err := os.ReadFile("testdata/videos_tab.json")
	require.NoError(t, err)

	videos, err := ParseVideosFromInitialData(gjson.ParseBytes(raw), "UC_X", 1, testVideoParser)
	require.NoError(t, err)
	require.Len(t, videos, 1)
	assert.Equal(t, "vid_aaa", videos[0].VideoID)
}

func TestParseVideosFromInitialData_NoVideosTab(t *testing.T) {
	data := gjson.Parse(`{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Home"}}]}}}`)
	videos, err := ParseVideosFromInitialData(data, "UC_X", 10, testVideoParser)
	require.NoError(t, err)
	assert.Empty(t, videos)
	assert.NotNil(t, videos)
}

func TestParseVideosFromInitialData_GarbageInput(t *testing.T) {
	data := gjson.Parse(`{"totally":"unexpected"}`)
	videos, err := ParseVideosFromInitialData(data, "UC_X", 10, testVideoParser)
	require.NoError(t, err)
	assert.Empty(t, videos)
}

func TestParseVideosFromInitialData_EmptyInput(t *testing.T) {
	videos, err := ParseVideosFromInitialData(gjson.Parse(`{}`), "UC_X", 10, testVideoParser)
	require.NoError(t, err)
	assert.Empty(t, videos)
}

func TestParseVideosFromInitialData_EndpointURLDetection(t *testing.T) {
	data := gjson.Parse(`{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[
		{"tabRenderer":{"title":"Unknown","endpoint":{"commandMetadata":{"webCommandMetadata":{"url":"/@x/videos"}}},"content":{"richGridRenderer":{"contents":[
			{"richItemRenderer":{"content":{"videoRenderer":{"videoId":"ep1","title":{"runs":[{"text":"Endpoint Video"}]}}}}}
		]}}}}
	]}}}`)
	videos, err := ParseVideosFromInitialData(data, "UC_X", 10, testVideoParser)
	require.NoError(t, err)
	require.Len(t, videos, 1)
	assert.Equal(t, "ep1", videos[0].VideoID)
}

func TestParseVideosFromInitialData_ContentsFallback(t *testing.T) {
	data := gjson.Parse(`{"contents":{"singleColumnBrowseResultsRenderer":{"items":[
		{"videoRenderer":{"videoId":"fb1","title":{"runs":[{"text":"Fallback"}]}}}
	]}}}`)
	videos, err := ParseVideosFromInitialData(data, "UC_X", 10, testVideoParser)
	require.NoError(t, err)
	require.Len(t, videos, 1)
	assert.Equal(t, "fb1", videos[0].VideoID)
}

func TestParseVideosFromInitialDataWithoutTabs_ResponseContextOnly(t *testing.T) {
	data := gjson.Parse(`{"responseContext":{"visitorData":"x"}}`)
	videos := ParseVideosFromInitialDataWithoutTabs(data, "UC_X", 10, testVideoParser)
	assert.Empty(t, videos)
	assert.NotNil(t, videos)
}

func TestParseVideosFromInitialDataWithoutTabs_ContentsNoRecovery(t *testing.T) {
	data := gjson.Parse(`{"contents":{"unknownRenderer":{}}}`)
	videos := ParseVideosFromInitialDataWithoutTabs(data, "UC_X", 10, testVideoParser)
	assert.Empty(t, videos)
}

func TestParseVideosFromInitialDataWithoutTabs_EmptyInput(t *testing.T) {
	videos := ParseVideosFromInitialDataWithoutTabs(gjson.Parse(`{}`), "UC_X", 10, testVideoParser)
	assert.Empty(t, videos)
}

func TestFindVideosTabContent_MatchByTitle(t *testing.T) {
	tabs := gjson.Parse(`[
		{"tabRenderer":{"title":"Home"}},
		{"tabRenderer":{"title":"동영상","content":{"marker":"here"}}}
	]`)
	content, titles := FindVideosTabContent(tabs)
	assert.True(t, content.Exists())
	assert.Equal(t, "here", content.Get("marker").String())
	assert.Equal(t, []string{"Home", "동영상"}, titles)
}

func TestFindVideosTabContent_NoMatch(t *testing.T) {
	tabs := gjson.Parse(`[{"tabRenderer":{"title":"Home"}},{"tabRenderer":{"title":"About"}}]`)
	content, titles := FindVideosTabContent(tabs)
	assert.False(t, content.Exists())
	assert.Equal(t, []string{"Home", "About"}, titles)
}

func TestFindVideosTabContent_EmptyInput(t *testing.T) {
	content, titles := FindVideosTabContent(gjson.Parse(`[]`))
	assert.False(t, content.Exists())
	assert.Nil(t, titles)
}

func TestCollectVideoRenderers_NestedDedup(t *testing.T) {
	root := gjson.Parse(`{"a":{"videoRenderer":{"videoId":"x"}},"b":[{"videoRenderer":{"videoId":"x"}},{"videoRenderer":{"videoId":"y"}}]}`)
	results := CollectVideoRenderers(root, 5)
	require.Len(t, results, 2)
	ids := []string{results[0].Get("videoId").String(), results[1].Get("videoId").String()}
	assert.ElementsMatch(t, []string{"x", "y"}, ids)
}

func TestCollectVideoRenderers_SkipsEmptyVideoID(t *testing.T) {
	root := gjson.Parse(`{"a":{"videoRenderer":{"title":"no id"}}}`)
	results := CollectVideoRenderers(root, 5)
	assert.Empty(t, results)
}

func TestCollectVideoRenderers_MaxResultsZeroReturnsNil(t *testing.T) {
	root := gjson.Parse(`{"videoRenderer":{"videoId":"x"}}`)
	results := CollectVideoRenderers(root, 0)
	assert.Nil(t, results)
}

func TestCollectVideoRenderers_GarbageInput(t *testing.T) {
	results := CollectVideoRenderers(gjson.Parse(`"a string"`), 5)
	assert.Empty(t, results)
}

func TestCollectVideoRenderers_RespectsMaxResults(t *testing.T) {
	root := gjson.Parse(`{"list":[{"videoRenderer":{"videoId":"a"}},{"videoRenderer":{"videoId":"b"}},{"videoRenderer":{"videoId":"c"}}]}`)
	results := CollectVideoRenderers(root, 2)
	require.Len(t, results, 2)
}

func TestParseLockupVideoViewModel_HappyPath(t *testing.T) {
	lockup := gjson.Parse(`{
		"contentId":"lk1",
		"contentType":"LOCKUP_CONTENT_TYPE_VIDEO",
		"contentImage":{"thumbnailViewModel":{
			"image":{"sources":[{"url":"https://t/lk1.jpg","width":336,"height":188}]},
			"overlays":[{"thumbnailBottomOverlayViewModel":{"badges":[{"thumbnailBadgeViewModel":{"text":"4:23"}}]}}]
		}},
		"metadata":{"lockupMetadataViewModel":{
			"title":{"content":"Lockup Title"},
			"metadata":{"contentMetadataViewModel":{"metadataRows":[{"metadataParts":[
				{"text":{"content":"69万回視聴"}},
				{"text":{"content":"1 month ago"}}
			]}]}}
		}}
	}`)
	video := ParseLockupVideoViewModel(lockup, "UC_X")
	require.NotNil(t, video)
	assert.Equal(t, "lk1", video.VideoID)
	assert.Equal(t, "Lockup Title", video.Title)
	assert.Equal(t, "4:23", video.Duration)
	assert.Equal(t, int64(690000), video.ViewCount)
	assert.Equal(t, "1 month ago", video.PublishedText)
	require.Len(t, video.Thumbnail, 1)
	assert.Equal(t, "https://t/lk1.jpg", video.Thumbnail[0].URL)
	assert.Equal(t, "UC_X", video.ChannelID)
}

func TestParseLockupVideoViewModel_VideoIDFromWatchEndpoint(t *testing.T) {
	lockup := gjson.Parse(`{
		"contentType":"LOCKUP_CONTENT_TYPE_VIDEO",
		"rendererContext":{"commandContext":{"onTap":{"innertubeCommand":{"watchEndpoint":{"videoId":"watch99"}}}}}
	}`)
	video := ParseLockupVideoViewModel(lockup, "UC_X")
	require.NotNil(t, video)
	assert.Equal(t, "watch99", video.VideoID)
}

func TestParseLockupVideoViewModel_WrongContentType(t *testing.T) {
	lockup := gjson.Parse(`{"contentId":"x","contentType":"LOCKUP_CONTENT_TYPE_PLAYLIST"}`)
	assert.Nil(t, ParseLockupVideoViewModel(lockup, "UC_X"))
}

func TestParseLockupVideoViewModel_NoVideoID(t *testing.T) {
	lockup := gjson.Parse(`{"contentType":"LOCKUP_CONTENT_TYPE_VIDEO"}`)
	assert.Nil(t, ParseLockupVideoViewModel(lockup, "UC_X"))
}

func TestParseLockupVideoViewModel_EmptyInput(t *testing.T) {
	assert.Nil(t, ParseLockupVideoViewModel(gjson.Parse(`{}`), "UC_X"))
}

func TestParseVideosFromInitialData_LockupViewModelInRichGrid(t *testing.T) {
	data := gjson.Parse(`{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[
		{"tabRenderer":{"title":"Videos","content":{"richGridRenderer":{"contents":[
			{"richItemRenderer":{"content":{"lockupViewModel":{
				"contentId":"lk2",
				"contentType":"LOCKUP_CONTENT_TYPE_VIDEO",
				"metadata":{"lockupMetadataViewModel":{"title":{"content":"Grid Lockup"}}}
			}}}}
		]}}}}
	]}}}`)
	videos, err := ParseVideosFromInitialData(data, "UC_X", 10, testVideoParser)
	require.NoError(t, err)
	require.Len(t, videos, 1)
	assert.Equal(t, "lk2", videos[0].VideoID)
	assert.Equal(t, "Grid Lockup", videos[0].Title)
}
