package scraper

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestParseUpcomingEventsFromInitialData(t *testing.T) {
	data := gjson.Parse(`{
		"contents": {
			"twoColumnBrowseResultsRenderer": {
				"tabs": [{
					"tabRenderer": {
						"content": {
							"sectionListRenderer": {
								"contents": [{
									"itemSectionRenderer": {
										"contents": [{
											"channelFeaturedContentRenderer": {
												"items": [{
													"videoRenderer": {
														"videoId": "live123",
														"title": {"simpleText": "Live Now"},
														"thumbnail": {"thumbnails": [{"url": "https://example.com/live.jpg", "width": 120, "height": 90}]},
														"thumbnailOverlays": [{
															"thumbnailOverlayTimeStatusRenderer": {"style": "LIVE"}
														}],
														"shortBylineText": {"runs": [{"text": "Hololive"}]}
													}
												}]
											}
										}, {
											"shelfRenderer": {
												"content": {
													"horizontalListRenderer": {
														"items": [{
															"gridVideoRenderer": {
																"videoId": "live123",
																"title": {"runs": [{"text": "Duplicate Live"}]},
																"thumbnailOverlays": [{
																	"thumbnailOverlayTimeStatusRenderer": {"style": "LIVE"}
																}]
															}
														}, {
															"videoRenderer": {
																"videoId": "upcoming456",
																"title": {"runs": [{"text": "Upcoming Stream"}]},
																"thumbnail": {"thumbnails": [{"url": "https://example.com/upcoming.jpg", "width": 120, "height": 90}]},
																"upcomingEventData": {"startTime": 1772755200},
																"viewCountText": {"simpleText": "Waiting"},
																"shortBylineText": {"runs": [{"text": "Hololive DEV_IS"}]}
															}
														}, {
															"videoRenderer": {
																"videoId": "vod789",
																"title": {"runs": [{"text": "Archive"}]}
															}
														}]
													}
												}
											}
										}]
									}
								}]
							}
						}
					}
				}]
			}
		}
	}`)

	events, err := parseUpcomingEventsFromInitialData(data)
	require.NoError(t, err)
	require.Len(t, events, 2)

	assert.Equal(t, "live123", events[0].VideoID)
	assert.Equal(t, "LIVE", events[0].Status)
	assert.Equal(t, "upcoming456", events[1].VideoID)
	assert.Equal(t, "UPCOMING", events[1].Status)
	require.NotNil(t, events[1].StartTime)
	assert.Equal(t, int64(1772755200), *events[1].StartTime)
}

func TestParseUpcomingEventsFromInitialData_Alert(t *testing.T) {
	data := gjson.Parse(`{
		"alerts": [{
			"alertRenderer": {
				"type": "ERROR",
				"text": {"simpleText": "This channel does not exist."}
			}
		}]
	}`)

	events, err := parseUpcomingEventsFromInitialData(data)
	require.Error(t, err)
	assert.Nil(t, events)
	assert.True(t, errors.Is(err, ErrChannelNotFound))
}
