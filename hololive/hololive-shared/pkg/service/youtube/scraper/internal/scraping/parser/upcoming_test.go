package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const upcomingData = `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[
	{"channelFeaturedContentRenderer":{"items":[
		{"videoRenderer":{
			"videoId":"live123",
			"title":{"simpleText":"Live Now"},
			"thumbnail":{"thumbnails":[{"url":"https://e/live.jpg","width":120,"height":90}]},
			"thumbnailOverlays":[{"thumbnailOverlayTimeStatusRenderer":{"style":"LIVE"}}],
			"shortBylineText":{"runs":[{"text":"Hololive"}]},
			"viewCountText":{"runs":[{"text":"1,234 watching"}]}
		}}
	]}},
	{"shelfRenderer":{"content":{"horizontalListRenderer":{"items":[
		{"gridVideoRenderer":{
			"videoId":"live123",
			"title":{"runs":[{"text":"Duplicate"}]},
			"thumbnailOverlays":[{"thumbnailOverlayTimeStatusRenderer":{"style":"LIVE"}}]
		}},
		{"videoRenderer":{
			"videoId":"upcoming456",
			"title":{"runs":[{"text":"Upcoming Stream"}]},
			"upcomingEventData":{"startTime":1772755200},
			"viewCountText":{"simpleText":"Waiting"},
			"shortBylineText":{"runs":[{"text":"Hololive DEV_IS"}]}
		}},
		{"videoRenderer":{
			"videoId":"vod789",
			"title":{"runs":[{"text":"Archive"}]}
		}}
	]}}}}
]}}]}}}}]}}}`

func TestParseUpcomingEventsFromInitialData_HappyPath(t *testing.T) {
	events := ParseUpcomingEventsFromInitialData(gjson.Parse(upcomingData))
	require.Len(t, events, 2)

	assert.Equal(t, "live123", events[0].VideoID)
	assert.Equal(t, "Live Now", events[0].Title)
	assert.Equal(t, "LIVE", events[0].Status)
	assert.Equal(t, "Hololive", events[0].ChannelTitle)
	assert.Equal(t, "1,234 watching", events[0].ViewCountText)
	assert.Nil(t, events[0].StartTime)
	require.Len(t, events[0].Thumbnail, 1)

	assert.Equal(t, "upcoming456", events[1].VideoID)
	assert.Equal(t, "UPCOMING", events[1].Status)
	assert.Equal(t, "Waiting", events[1].ViewCountText)
	require.NotNil(t, events[1].StartTime)
	assert.Equal(t, int64(1772755200), *events[1].StartTime)
}

func TestParseUpcomingEventsFromInitialData_FiltersNonLiveUpcoming(t *testing.T) {
	data := gjson.Parse(`{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[
		{"channelFeaturedContentRenderer":{"items":[
			{"videoRenderer":{"videoId":"vod1","title":{"runs":[{"text":"Past Stream"}]}}}
		]}}
	]}}]}}}}]}}}`)
	events := ParseUpcomingEventsFromInitialData(data)
	assert.Empty(t, events)
	assert.NotNil(t, events)
}

func TestParseUpcomingEventsFromInitialData_UpcomingEventDataImpliesUpcoming(t *testing.T) {
	data := gjson.Parse(`{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[
		{"channelFeaturedContentRenderer":{"items":[
			{"videoRenderer":{"videoId":"up1","title":{"runs":[{"text":"Scheduled"}]},"upcomingEventData":{"startTime":1772755200}}}
		]}}
	]}}]}}}}]}}}`)
	events := ParseUpcomingEventsFromInitialData(data)
	require.Len(t, events, 1)
	assert.Equal(t, "UPCOMING", events[0].Status)
	require.NotNil(t, events[0].StartTime)
	assert.Equal(t, int64(1772755200), *events[0].StartTime)
}

func TestParseUpcomingEventsFromInitialData_TitleAndViewCountRunsFallback(t *testing.T) {
	data := gjson.Parse(`{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[
		{"channelFeaturedContentRenderer":{"items":[
			{"videoRenderer":{
				"videoId":"runs1",
				"title":{"runs":[{"text":"Runs Title"}]},
				"thumbnailOverlays":[{"thumbnailOverlayTimeStatusRenderer":{"style":"UPCOMING"}}],
				"viewCountText":{"runs":[{"text":"42 waiting"}]}
			}}
		]}}
	]}}]}}}}]}}}`)
	events := ParseUpcomingEventsFromInitialData(data)
	require.Len(t, events, 1)
	assert.Equal(t, "Runs Title", events[0].Title)
	assert.Equal(t, "UPCOMING", events[0].Status)
	assert.Equal(t, "42 waiting", events[0].ViewCountText)
}

func TestParseUpcomingEventsFromInitialData_VideoWithoutID(t *testing.T) {
	data := gjson.Parse(`{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[
		{"channelFeaturedContentRenderer":{"items":[
			{"videoRenderer":{"title":{"runs":[{"text":"No ID"}]},"thumbnailOverlays":[{"thumbnailOverlayTimeStatusRenderer":{"style":"LIVE"}}]}}
		]}}
	]}}]}}}}]}}}`)
	events := ParseUpcomingEventsFromInitialData(data)
	assert.Empty(t, events)
}

func TestParseUpcomingEventsFromInitialData_GarbageInput(t *testing.T) {
	events := ParseUpcomingEventsFromInitialData(gjson.Parse(`{"unexpected":[1,2,3]}`))
	assert.Empty(t, events)
	assert.NotNil(t, events)
}

func TestParseUpcomingEventsFromInitialData_EmptyInput(t *testing.T) {
	events := ParseUpcomingEventsFromInitialData(gjson.Parse(`{}`))
	assert.Empty(t, events)
	assert.NotNil(t, events)
}
