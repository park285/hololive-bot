package scraper

import (
	"context"
	"fmt"

	"github.com/tidwall/gjson"
)

// GetPlaylists: 채널의 플레이리스트 목록 조회 (/channel/{id}/playlists)
func (c *Client) GetPlaylists(ctx context.Context, channelID string, maxResults int) ([]*Playlist, error) {
	url := fmt.Sprintf("https://www.youtube.com/channel/%s/playlists", channelID)
	html, err := c.fetchPage(ctx, url)
	if err != nil {
		return nil, err
	}

	jsonStr, err := extractYtInitialData(html)
	if err != nil {
		return nil, fmt.Errorf("failed to extract ytInitialData: %w", err)
	}

	data := gjson.Parse(jsonStr)
	if err := checkAlerts(data); err != nil {
		return nil, err
	}

	// Playlists 탭 찾기
	tabPath := "contents.twoColumnBrowseResultsRenderer.tabs"
	var playlistItems []gjson.Result

	data.Get(tabPath).ForEach(func(_, tab gjson.Result) bool {
		tabTitle := tab.Get("tabRenderer.title").String()
		if tabTitle == "Playlists" {
			// gridRenderer 또는 sectionListRenderer 탐색
			content := tab.Get("tabRenderer.content")

			// gridRenderer 먼저 시도
			content.Get("sectionListRenderer.contents").ForEach(func(_, section gjson.Result) bool {
				// gridRenderer
				gridItems := section.Get("itemSectionRenderer.contents.0.gridRenderer.items")
				if gridItems.Exists() {
					gridItems.ForEach(func(_, item gjson.Result) bool {
						if item.Get("gridPlaylistRenderer").Exists() {
							playlistItems = append(playlistItems, item.Get("gridPlaylistRenderer"))
						}
						return true
					})
				}
				// shelfRenderer (Created playlists 등)
				shelfItems := section.Get("itemSectionRenderer.contents.0.shelfRenderer.content.horizontalListRenderer.items")
				if shelfItems.Exists() {
					shelfItems.ForEach(func(_, item gjson.Result) bool {
						if item.Get("gridPlaylistRenderer").Exists() {
							playlistItems = append(playlistItems, item.Get("gridPlaylistRenderer"))
						}
						return true
					})
				}
				return true
			})
			return false
		}
		return true
	})

	playlists := make([]*Playlist, 0, min(len(playlistItems), maxResults))
	for i, item := range playlistItems {
		if i >= maxResults {
			break
		}
		playlist := c.parseGridPlaylistRenderer(item, channelID)
		if playlist != nil {
			playlists = append(playlists, playlist)
		}
	}

	return playlists, nil
}

// parseGridPlaylistRenderer: gridPlaylistRenderer JSON을 Playlist 구조체로 변환
func (c *Client) parseGridPlaylistRenderer(playlist gjson.Result, channelID string) *Playlist {
	playlistID := playlist.Get("playlistId").String()
	if playlistID == "" {
		return nil
	}

	var thumbnails []Thumbnail
	playlist.Get("thumbnail.thumbnails").ForEach(func(_, t gjson.Result) bool {
		thumbnails = append(thumbnails, Thumbnail{
			URL:    t.Get("url").String(),
			Width:  int(t.Get("width").Int()),
			Height: int(t.Get("height").Int()),
		})
		return true
	})

	title := playlist.Get("title.runs.0.text").String()
	if title == "" {
		title = playlist.Get("title.simpleText").String()
	}

	videoCountText := playlist.Get("videoCountText.runs.0.text").String()
	videoCount := parseVideoCount(videoCountText)

	return &Playlist{
		PlaylistID:   playlistID,
		Title:        title,
		Thumbnail:    thumbnails,
		VideoCount:   videoCount,
		ChannelID:    channelID,
		ChannelTitle: playlist.Get("shortBylineText.runs.0.text").String(),
	}
}
