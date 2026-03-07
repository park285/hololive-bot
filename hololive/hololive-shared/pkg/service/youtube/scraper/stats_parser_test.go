// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package scraper

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tidwall/gjson"
)

func TestParseChannelStatsFromInitialData(t *testing.T) {
	data := gjson.Parse(`{
		"onResponseReceivedEndpoints": [{
			"showEngagementPanelEndpoint": {
				"engagementPanel": {
					"engagementPanelSectionListRenderer": {
						"content": {
							"sectionListRenderer": {
								"contents": [{
									"itemSectionRenderer": {
										"contents": [{
											"aboutChannelRenderer": {
												"metadata": {
													"aboutChannelViewModel": {
														"subscriberCountText": "2.76M subscribers",
														"viewCountText": "1,056,229,686 views",
														"videoCountText": "2,429 videos",
														"joinedDateText": {"content": "Joined Jul 2, 2019"},
														"description": "rabbit hole",
														"country": "Japan"
													}
												}
											}
										}]
									}
								}]
							}
						}
					}
				}
			}
		}],
		"contents": {
			"twoColumnBrowseResultsRenderer": {
				"tabs": [{
					"tabRenderer": {
						"endpoint": {
							"browseEndpoint": {
								"canonicalBaseUrl": "/@pekora"
							}
						}
					}
				}]
			}
		}
	}`)

	stats := parseChannelStatsFromInitialData(data, "UC_TEST")

	assert.Equal(t, "UC_TEST", stats.ChannelID)
	assert.Equal(t, int64(2_760_000), stats.SubscriberCount)
	assert.Equal(t, int64(1_056_229_686), stats.ViewCount)
	assert.Equal(t, int64(2429), stats.VideoCount)
	assert.Equal(t, time.Date(2019, time.July, 2, 0, 0, 0, 0, time.UTC).Unix(), stats.JoinedDate)
	assert.Equal(t, "rabbit hole", stats.Description)
	assert.Equal(t, "Japan", stats.Country)
	assert.Equal(t, "@pekora", stats.Handle)
}

func TestParseChannelSnippetFromInitialData(t *testing.T) {
	data := gjson.Parse(`{
		"header": {
			"pageHeaderRenderer": {
				"content": {
					"pageHeaderViewModel": {
						"image": {
							"decoratedAvatarViewModel": {
								"avatar": {
									"avatarViewModel": {
										"image": {
											"sources": [
												{"url": "https://example.com/avatar.jpg", "width": 100, "height": 100}
											]
										}
									}
								}
							}
						},
						"banner": {
							"imageBannerViewModel": {
								"image": {
									"sources": [
										{"url": "https://example.com/banner.jpg", "width": 1280, "height": 351}
									]
								}
							}
						}
					}
				}
			}
		}
	}`)

	snippet := parseChannelSnippetFromInitialData(data)

	assert.Len(t, snippet.Avatar, 1)
	assert.Len(t, snippet.Banner, 1)
	assert.Equal(t, "https://example.com/avatar.jpg", snippet.Avatar[0].URL)
	assert.Equal(t, "https://example.com/banner.jpg", snippet.Banner[0].URL)
}
