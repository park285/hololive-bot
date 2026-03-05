package scraper

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// -- parseGridPlaylistRenderer 단위 테스트 --

func TestParseGridPlaylistRenderer_Normal(t *testing.T) {
	t.Parallel()

	jsonStr := `{
		"playlistId": "PLtest123",
		"title": {"runs": [{"text": "Test Playlist"}]},
		"thumbnail": {"thumbnails": [
			{"url": "https://example.com/thumb.jpg", "width": 196, "height": 110}
		]},
		"videoCountText": {"runs": [{"text": "42 videos"}]},
		"shortBylineText": {"runs": [{"text": "Test Channel"}]}
	}`

	client := NewClient()
	playlist := client.parseGridPlaylistRenderer(gjson.Parse(jsonStr), "UC_TEST")

	require.NotNil(t, playlist)
	assert.Equal(t, "PLtest123", playlist.PlaylistID)
	assert.Equal(t, "Test Playlist", playlist.Title)
	assert.Equal(t, "UC_TEST", playlist.ChannelID)
	assert.Equal(t, "Test Channel", playlist.ChannelTitle)
	assert.Equal(t, int64(42), playlist.VideoCount)
	require.Len(t, playlist.Thumbnail, 1)
	assert.Equal(t, "https://example.com/thumb.jpg", playlist.Thumbnail[0].URL)
	assert.Equal(t, 196, playlist.Thumbnail[0].Width)
	assert.Equal(t, 110, playlist.Thumbnail[0].Height)
}

func TestParseGridPlaylistRenderer_EmptyPlaylistID(t *testing.T) {
	t.Parallel()

	jsonStr := `{"playlistId": ""}`
	client := NewClient()
	playlist := client.parseGridPlaylistRenderer(gjson.Parse(jsonStr), "UC_TEST")
	assert.Nil(t, playlist)
}

func TestParseGridPlaylistRenderer_SimpleTextTitle(t *testing.T) {
	t.Parallel()

	jsonStr := `{
		"playlistId": "PLtest",
		"title": {"simpleText": "Simple Title"},
		"videoCountText": {"runs": [{"text": "5 videos"}]}
	}`

	client := NewClient()
	playlist := client.parseGridPlaylistRenderer(gjson.Parse(jsonStr), "UC_TEST")

	require.NotNil(t, playlist)
	assert.Equal(t, "Simple Title", playlist.Title)
}

func TestParseGridPlaylistRenderer_NoThumbnails(t *testing.T) {
	t.Parallel()

	jsonStr := `{
		"playlistId": "PLtest",
		"title": {"runs": [{"text": "No Thumb Playlist"}]}
	}`

	client := NewClient()
	playlist := client.parseGridPlaylistRenderer(gjson.Parse(jsonStr), "UC_TEST")

	require.NotNil(t, playlist)
	assert.Empty(t, playlist.Thumbnail)
}

func TestParseGridPlaylistRenderer_MultipleThumbnails(t *testing.T) {
	t.Parallel()

	jsonStr := `{
		"playlistId": "PLtest",
		"title": {"runs": [{"text": "Multi Thumb"}]},
		"thumbnail": {"thumbnails": [
			{"url": "https://t1.jpg", "width": 100, "height": 56},
			{"url": "https://t2.jpg", "width": 200, "height": 112}
		]}
	}`

	client := NewClient()
	playlist := client.parseGridPlaylistRenderer(gjson.Parse(jsonStr), "UC_TEST")

	require.NotNil(t, playlist)
	assert.Len(t, playlist.Thumbnail, 2)
}

func TestParseGridPlaylistRenderer_VideoCountFormats(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		countText string
		want      int64
	}{
		"복수형":    {countText: "42 videos", want: 42},
		"단수형":    {countText: "1 video", want: 1},
		"콤마 포함":  {countText: "1,234 videos", want: 1234},
		"텍스트 없음": {countText: "", want: 0},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			jsonStr := `{
				"playlistId": "PLtest",
				"title": {"runs": [{"text": "Test"}]},
				"videoCountText": {"runs": [{"text": "` + tc.countText + `"}]}
			}`
			client := NewClient()
			playlist := client.parseGridPlaylistRenderer(gjson.Parse(jsonStr), "UC_TEST")
			require.NotNil(t, playlist)
			assert.Equal(t, tc.want, playlist.VideoCount)
		})
	}
}

// -- GetPlaylists 통합 테스트 (HTTP mock) --

func TestGetPlaylists_GridRenderer(t *testing.T) {
	t.Parallel()

	ytInitialData := `{
		"contents": {
			"twoColumnBrowseResultsRenderer": {
				"tabs": [
					{"tabRenderer": {"title": "Home"}},
					{"tabRenderer": {
						"title": "Playlists",
						"content": {
							"sectionListRenderer": {
								"contents": [
									{"itemSectionRenderer": {
										"contents": [
											{"gridRenderer": {
												"items": [
													{"gridPlaylistRenderer": {
														"playlistId": "PL001",
														"title": {"runs": [{"text": "Playlist 1"}]},
														"videoCountText": {"runs": [{"text": "10 videos"}]}
													}},
													{"gridPlaylistRenderer": {
														"playlistId": "PL002",
														"title": {"runs": [{"text": "Playlist 2"}]},
														"videoCountText": {"runs": [{"text": "20 videos"}]}
													}}
												]
											}}
										]
									}}
								]
							}
						}
					}}
				]
			}
		}
	}`

	htmlBody := "<script>var ytInitialData = " + ytInitialData + ";</script>"

	client := NewClient(
		WithRateLimiter(NewRateLimiter(0)),
		WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(htmlBody)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}),
		}),
	)

	playlists, err := client.GetPlaylists(context.Background(), "UC_TEST", 10)
	require.NoError(t, err)
	require.Len(t, playlists, 2)
	assert.Equal(t, "PL001", playlists[0].PlaylistID)
	assert.Equal(t, "Playlist 1", playlists[0].Title)
	assert.Equal(t, int64(10), playlists[0].VideoCount)
	assert.Equal(t, "PL002", playlists[1].PlaylistID)
}

func TestGetPlaylists_ShelfRenderer(t *testing.T) {
	t.Parallel()

	ytInitialData := `{
		"contents": {
			"twoColumnBrowseResultsRenderer": {
				"tabs": [
					{"tabRenderer": {
						"title": "Playlists",
						"content": {
							"sectionListRenderer": {
								"contents": [
									{"itemSectionRenderer": {
										"contents": [
											{"shelfRenderer": {
												"content": {
													"horizontalListRenderer": {
														"items": [
															{"gridPlaylistRenderer": {
																"playlistId": "PLshelf1",
																"title": {"runs": [{"text": "Shelf PL"}]},
																"videoCountText": {"runs": [{"text": "5 videos"}]}
															}}
														]
													}
												}
											}}
										]
									}}
								]
							}
						}
					}}
				]
			}
		}
	}`

	htmlBody := "<script>var ytInitialData = " + ytInitialData + ";</script>"

	client := NewClient(
		WithRateLimiter(NewRateLimiter(0)),
		WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(htmlBody)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}),
		}),
	)

	playlists, err := client.GetPlaylists(context.Background(), "UC_TEST", 10)
	require.NoError(t, err)
	require.Len(t, playlists, 1)
	assert.Equal(t, "PLshelf1", playlists[0].PlaylistID)
}

func TestGetPlaylists_MalformedPlaylistJSON_TableDriven(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		ytInitialData string
		wantCount     int
		wantErr       bool
	}{
		"ytInitialData가 깨진 JSON인 경우": {
			ytInitialData: `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[`,
			wantCount:     0,
			wantErr:       true,
		},
		"gridPlaylistRenderer가 문자열인 경우": {
			ytInitialData: `{
				"contents": {
					"twoColumnBrowseResultsRenderer": {
						"tabs": [
							{"tabRenderer": {
								"title": "Playlists",
								"content": {
									"sectionListRenderer": {
										"contents": [
											{"itemSectionRenderer": {
												"contents": [
													{"gridRenderer": {
														"items": [
															{"gridPlaylistRenderer": "invalid-string"}
														]
													}}
												]
											}}
										]
									}
								}
							}}
						]
					}
				}
			}`,
			wantCount: 0,
			wantErr:   false,
		},
		"playlistId 누락 객체": {
			ytInitialData: `{
				"contents": {
					"twoColumnBrowseResultsRenderer": {
						"tabs": [
							{"tabRenderer": {
								"title": "Playlists",
								"content": {
									"sectionListRenderer": {
										"contents": [
											{"itemSectionRenderer": {
												"contents": [
													{"gridRenderer": {
														"items": [
																{"gridPlaylistRenderer": {"title": {"runs": [{"text": "No ID"}]}}}
														]
													}}
												]
											}}
										]
									}
								}
							}}
						]
					}
				}
			}`,
			wantCount: 0,
			wantErr:   false,
		},
		"정상/비정상 혼합": {
			ytInitialData: `{
				"contents": {
					"twoColumnBrowseResultsRenderer": {
						"tabs": [
							{"tabRenderer": {
								"title": "Playlists",
								"content": {
									"sectionListRenderer": {
										"contents": [
											{"itemSectionRenderer": {
												"contents": [
													{"gridRenderer": {
														"items": [
															{"gridPlaylistRenderer": {"title": {"runs": [{"text": "No ID"}]}}},
															{"gridPlaylistRenderer": {"playlistId": "PL_VALID", "title": {"runs": [{"text": "Valid"}]}}}
														]
													}}
												]
											}}
										]
									}
								}
							}}
						]
					}
				}
			}`,
			wantCount: 1,
			wantErr:   false,
		},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := newPlaylistMockClient("<script>var ytInitialData = " + tc.ytInitialData + ";</script>")

			playlists, err := client.GetPlaylists(context.Background(), "UC_TEST", 10)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, playlists, tc.wantCount)
		})
	}
}

func TestGetPlaylists_NoPlaylistsTab(t *testing.T) {
	t.Parallel()

	ytInitialData := `{
		"contents": {
			"twoColumnBrowseResultsRenderer": {
				"tabs": [
					{"tabRenderer": {"title": "Home"}},
					{"tabRenderer": {"title": "Videos"}}
				]
			}
		}
	}`

	htmlBody := "<script>var ytInitialData = " + ytInitialData + ";</script>"

	client := NewClient(
		WithRateLimiter(NewRateLimiter(0)),
		WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(htmlBody)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}),
		}),
	)

	playlists, err := client.GetPlaylists(context.Background(), "UC_TEST", 10)
	require.NoError(t, err)
	assert.Empty(t, playlists)
}

func TestGetPlaylists_MaxResultsLimit(t *testing.T) {
	t.Parallel()

	// 3개 플레이리스트, maxResults=2
	ytInitialData := `{
		"contents": {
			"twoColumnBrowseResultsRenderer": {
				"tabs": [
					{"tabRenderer": {
						"title": "Playlists",
						"content": {
							"sectionListRenderer": {
								"contents": [
									{"itemSectionRenderer": {
										"contents": [
											{"gridRenderer": {
												"items": [
													{"gridPlaylistRenderer": {"playlistId": "PL1", "title": {"runs": [{"text": "PL1"}]}}},
													{"gridPlaylistRenderer": {"playlistId": "PL2", "title": {"runs": [{"text": "PL2"}]}}},
													{"gridPlaylistRenderer": {"playlistId": "PL3", "title": {"runs": [{"text": "PL3"}]}}}
												]
											}}
										]
									}}
								]
							}
						}
					}}
				]
			}
		}
	}`

	htmlBody := "<script>var ytInitialData = " + ytInitialData + ";</script>"

	client := NewClient(
		WithRateLimiter(NewRateLimiter(0)),
		WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(htmlBody)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}),
		}),
	)

	playlists, err := client.GetPlaylists(context.Background(), "UC_TEST", 2)
	require.NoError(t, err)
	assert.Len(t, playlists, 2)
}

func TestGetPlaylists_PaginationEdgeCases_TableDriven(t *testing.T) {
	t.Parallel()

	ytInitialData := `{
		"contents": {
			"twoColumnBrowseResultsRenderer": {
				"tabs": [
					{"tabRenderer": {
						"title": "Playlists",
						"content": {
							"sectionListRenderer": {
								"contents": [
									{"itemSectionRenderer": {
										"contents": [
											{"gridRenderer": {
												"items": [
													{"gridPlaylistRenderer": {"playlistId": "PL1", "title": {"runs": [{"text": "PL1"}]}}},
													{"gridPlaylistRenderer": {"playlistId": "PL2", "title": {"runs": [{"text": "PL2"}]}}},
													{"gridPlaylistRenderer": {"playlistId": "PL3", "title": {"runs": [{"text": "PL3"}]}}}
												]
											}}
										]
									}}
								]
							}
						}
					}}
				]
			}
		}
	}`

	tests := map[string]struct {
		maxResults int
		wantIDs    []string
	}{
		"음수면 빈 결과":   {maxResults: -1, wantIDs: []string{}},
		"0이면 빈 결과":   {maxResults: 0, wantIDs: []string{}},
		"1이면 첫 페이지만": {maxResults: 1, wantIDs: []string{"PL1"}},
		"2이면 두 개만":   {maxResults: 2, wantIDs: []string{"PL1", "PL2"}},
		"크면 전체 반환":   {maxResults: 10, wantIDs: []string{"PL1", "PL2", "PL3"}},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := newPlaylistMockClient("<script>var ytInitialData = " + ytInitialData + ";</script>")

			playlists, err := client.GetPlaylists(context.Background(), "UC_TEST", tc.maxResults)
			require.NoError(t, err)
			require.Len(t, playlists, len(tc.wantIDs))
			for idx, wantID := range tc.wantIDs {
				assert.Equal(t, wantID, playlists[idx].PlaylistID)
			}
		})
	}
}

func TestGetPlaylists_EmptyGrid(t *testing.T) {
	t.Parallel()

	ytInitialData := `{
		"contents": {
			"twoColumnBrowseResultsRenderer": {
				"tabs": [
					{"tabRenderer": {
						"title": "Playlists",
						"content": {
							"sectionListRenderer": {
								"contents": [
									{"itemSectionRenderer": {
										"contents": [
											{"gridRenderer": {"items": []}}
										]
									}}
								]
							}
						}
					}}
				]
			}
		}
	}`

	htmlBody := "<script>var ytInitialData = " + ytInitialData + ";</script>"

	client := NewClient(
		WithRateLimiter(NewRateLimiter(0)),
		WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(htmlBody)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}),
		}),
	)

	playlists, err := client.GetPlaylists(context.Background(), "UC_TEST", 10)
	require.NoError(t, err)
	assert.Empty(t, playlists)
}

func TestGetPlaylists_ChannelNotFound(t *testing.T) {
	t.Parallel()

	ytInitialData := `{"alerts":[{"alertRenderer":{"type":"ERROR","text":{"simpleText":"This channel does not exist."}}}]}`
	htmlBody := "<script>var ytInitialData = " + ytInitialData + ";</script>"

	client := NewClient(
		WithRateLimiter(NewRateLimiter(0)),
		WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(htmlBody)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}),
		}),
	)

	playlists, err := client.GetPlaylists(context.Background(), "UC_INVALID", 10)
	require.Error(t, err)
	assert.Nil(t, playlists)
	assert.ErrorIs(t, err, ErrChannelNotFound)
}

func newPlaylistMockClient(htmlBody string) *Client {
	return NewClient(
		WithRateLimiter(NewRateLimiter(0)),
		WithHTTPClient(&http.Client{
			Timeout: 5 * time.Second,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(htmlBody)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}),
		}),
	)
}

// roundTripFunc: retry_test.go에 이미 정의되어 있음
