package parser

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const aboutChannelData = `{
	"onResponseReceivedEndpoints":[{"showEngagementPanelEndpoint":{"engagementPanel":{"engagementPanelSectionListRenderer":{"content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"aboutChannelRenderer":{"metadata":{"aboutChannelViewModel":{
		"subscriberCountText":"2.76M subscribers",
		"viewCountText":"1,056,229,686 views",
		"videoCountText":"2,429 videos",
		"joinedDateText":{"content":"Joined Jul 2, 2019"},
		"description":"rabbit hole",
		"country":"Japan"
	}}}}]}}]}}}}}}],
	"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"endpoint":{"browseEndpoint":{"canonicalBaseUrl":"/@pekora"}}}}]}}
}`

func TestParseChannelStatsFromInitialData_HappyPath(t *testing.T) {
	stats := ParseChannelStatsFromInitialData(parseGJSONResultPtr(aboutChannelData), "UC_X")
	require.NotNil(t, stats)
	assert.Equal(t, "UC_X", stats.ChannelID)
	assert.Equal(t, int64(2_760_000), stats.SubscriberCount)
	assert.Equal(t, int64(1_056_229_686), stats.ViewCount)
	assert.Equal(t, int64(2429), stats.VideoCount)
	assert.Equal(t, time.Date(2019, time.July, 2, 0, 0, 0, 0, time.UTC).Unix(), stats.JoinedDate)
	assert.Equal(t, "rabbit hole", stats.Description)
	assert.Equal(t, "Japan", stats.Country)
	assert.Equal(t, "@pekora", stats.Handle)
}

func TestParseChannelStatsFromInitialData_EmptyInput(t *testing.T) {
	stats := ParseChannelStatsFromInitialData(parseGJSONResultPtr(`{}`), "UC_X")
	require.NotNil(t, stats)
	assert.Equal(t, "UC_X", stats.ChannelID)
	assert.Equal(t, int64(0), stats.SubscriberCount)
	assert.Equal(t, int64(0), stats.ViewCount)
	assert.Equal(t, int64(0), stats.VideoCount)
	assert.Equal(t, int64(0), stats.JoinedDate)
	assert.Empty(t, stats.Description)
	assert.Empty(t, stats.Handle)
}

func TestParseChannelStatsFromInitialData_GarbageInput(t *testing.T) {
	stats := ParseChannelStatsFromInitialData(parseGJSONResultPtr(`{"random":"junk"}`), "UC_X")
	require.NotNil(t, stats)
	assert.Equal(t, int64(0), stats.SubscriberCount)
	assert.Empty(t, stats.Handle)
}

func TestParseChannelSnippetFromInitialData_HappyPath(t *testing.T) {
	data := gjson.Parse(`{"header":{"pageHeaderRenderer":{"content":{"pageHeaderViewModel":{
		"image":{"decoratedAvatarViewModel":{"avatar":{"avatarViewModel":{"image":{"sources":[{"url":"https://a/avatar.jpg","width":100,"height":100}]}}}}},
		"banner":{"imageBannerViewModel":{"image":{"sources":[{"url":"https://a/banner.jpg","width":1280,"height":351}]}}}
	}}}}}`)
	snippet := ParseChannelSnippetFromInitialData(&data)
	require.Len(t, snippet.Avatar, 1)
	require.Len(t, snippet.Banner, 1)
	assert.Equal(t, "https://a/avatar.jpg", snippet.Avatar[0].URL)
	assert.Equal(t, "https://a/banner.jpg", snippet.Banner[0].URL)
}

func TestParseChannelSnippetFromInitialData_EmptyInput(t *testing.T) {
	snippet := ParseChannelSnippetFromInitialData(parseGJSONResultPtr(`{}`))
	assert.Empty(t, snippet.Avatar)
	assert.Empty(t, snippet.Banner)
	assert.NotNil(t, snippet.Avatar)
	assert.NotNil(t, snippet.Banner)
}

func TestParseChannelHandle(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{"leading slash stripped", `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"endpoint":{"browseEndpoint":{"canonicalBaseUrl":"/@pekora"}}}}]}}}`, "@pekora"},
		{"no leading slash kept", `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"endpoint":{"browseEndpoint":{"canonicalBaseUrl":"plainhandle"}}}}]}}}`, "plainhandle"},
		{"bare slash stripped to empty", `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"endpoint":{"browseEndpoint":{"canonicalBaseUrl":"/"}}}}]}}}`, ""},
		{"empty input", `{}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseChannelHandle(parseGJSONResultPtr(tt.json)))
		})
	}
}

func TestParseThumbnailSources(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		thumbs := ParseThumbnailSources(parseGJSONResultPtr(`[{"url":"u1","width":120,"height":90},{"url":"u2","width":480,"height":360}]`))
		require.Len(t, thumbs, 2)
		assert.Equal(t, Thumbnail{URL: "u1", Width: 120, Height: 90}, thumbs[0])
		assert.Equal(t, Thumbnail{URL: "u2", Width: 480, Height: 360}, thumbs[1])
	})

	t.Run("empty array", func(t *testing.T) {
		thumbs := ParseThumbnailSources(parseGJSONResultPtr(`[]`))
		assert.Empty(t, thumbs)
		assert.NotNil(t, thumbs)
	})

	t.Run("non-existent source returns empty", func(t *testing.T) {
		thumbs := ParseThumbnailSources(&gjson.Result{})
		assert.Empty(t, thumbs)
	})

	t.Run("null literal yields empty slice", func(t *testing.T) {
		thumbs := ParseThumbnailSources(parseGJSONResultPtr(`null`))
		assert.Empty(t, thumbs)
		assert.NotNil(t, thumbs)
	})

	t.Run("scalar yields empty slice", func(t *testing.T) {
		thumbs := ParseThumbnailSources(parseGJSONResultPtr(`42`))
		assert.Empty(t, thumbs)
		assert.NotNil(t, thumbs)
	})
}

func TestParseShortNumber(t *testing.T) {
	tests := []struct {
		text string
		want int64
	}{
		{"1.5K", 1500},
		{"2.76M subscribers", 0},
		{"5B", 5_000_000_000},
		{"1,234", 1234},
		{"2.3", 2},
		{"No", 0},
		{"", 0},
		{"garbage", 0},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseShortNumber(tt.text))
		})
	}
}

func TestParseSubscriberCount(t *testing.T) {
	tests := []struct {
		text string
		want int64
	}{
		{"2.76M subscribers", 2_760_000},
		{"1 subscriber", 1},
		{"100 subscribers", 100},
		{"garbage", 0},
		{"", 0},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseSubscriberCount(tt.text))
		})
	}
}

func TestParseViewCount(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int64
	}{
		{"english full number", "1,056,229,686 views", 1_056_229_686},
		{"english K suffix", "1.5K views", 1500},
		{"english M suffix bare", "2.76M", 2_760_000},
		{"japanese man", "69万回視聴", 690_000},
		{"korean man", "조회수 1.2만회", 12_000},
		{"korean cheon", "조회수 5천회", 5_000},
		{"korean plain count", "1,234회", 1234},
		{"singular view", "10 view", 10},
		{"no views", "No views", 0},
		{"empty", "", 0},
		{"garbage", "garbage", 0},
		{"korean eok", "3.4억", 340_000_000},
		{"korean jo", "조회수 1.5조회", 1_500_000_000_000},
		{"korean jo bare", "2조", 2_000_000_000_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseViewCount(tt.text))
		})
	}
}

func TestParseVideoCount(t *testing.T) {
	tests := []struct {
		text string
		want int64
	}{
		{"2,429 videos", 2429},
		{"1 video", 1},
		{"42", 42},
		{"garbage", 0},
		{"", 0},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseVideoCount(tt.text))
		})
	}
}

func TestParseJoinedDate(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int64
	}{
		{"joined prefix jan abbrev", "Joined Jul 2, 2019", time.Date(2019, time.July, 2, 0, 0, 0, 0, time.UTC).Unix()},
		{"full month name", "January 2, 2020", time.Date(2020, time.January, 2, 0, 0, 0, 0, time.UTC).Unix()},
		{"day-month-year", "2 Jan 2021", time.Date(2021, time.January, 2, 0, 0, 0, 0, time.UTC).Unix()},
		{"iso date", "2022-03-04", time.Date(2022, time.March, 4, 0, 0, 0, 0, time.UTC).Unix()},
		{"empty", "", 0},
		{"joined garbage", "Joined garbage", 0},
		{"plain garbage", "garbage", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseJoinedDate(tt.text))
		})
	}
}
