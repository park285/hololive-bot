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
	"context"
	"fmt"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/tidwall/gjson"
)

func (c *Client) GetPlaylists(ctx context.Context, channelID string, maxResults int) ([]*Playlist, error) {
	if maxResults <= 0 {
		return []*Playlist{}, nil
	}

	url := fmt.Sprintf("https://www.youtube.com/channel/%s/playlists", channelID)
	html, err := c.fetchPage(ctx, url)
	if err != nil {
		return nil, err
	}

	jsonStr, err := extractYtInitialData(html)
	if err != nil {
		return nil, fmt.Errorf("failed to extract ytInitialData: %w", err)
	}
	if !json.Valid([]byte(jsonStr)) {
		return nil, fmt.Errorf("invalid ytInitialData JSON")
	}

	data := gjson.Parse(jsonStr)
	if err := checkAlerts(data); err != nil {
		return nil, err
	}

	playlistItems := collectPlaylistItemsFromBrowseData(data)

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

func collectPlaylistItemsFromBrowseData(data gjson.Result) []gjson.Result {
	tabPath := "contents.twoColumnBrowseResultsRenderer.tabs"
	playlistItems := make([]gjson.Result, 0)

	data.Get(tabPath).ForEach(func(_, tab gjson.Result) bool {
		if tab.Get("tabRenderer.title").String() != "Playlists" {
			return true
		}

		content := tab.Get("tabRenderer.content")
		content.Get("sectionListRenderer.contents").ForEach(func(_, section gjson.Result) bool {
			appendGridPlaylistRenderers(
				&playlistItems,
				section.Get("itemSectionRenderer.contents.0.gridRenderer.items"),
			)
			appendGridPlaylistRenderers(
				&playlistItems,
				section.Get("itemSectionRenderer.contents.0.shelfRenderer.content.horizontalListRenderer.items"),
			)
			return true
		})
		return false
	})

	return playlistItems
}

func appendGridPlaylistRenderers(target *[]gjson.Result, items gjson.Result) {
	if !items.Exists() {
		return
	}

	items.ForEach(func(_, item gjson.Result) bool {
		renderer := item.Get("gridPlaylistRenderer")
		if renderer.Exists() {
			*target = append(*target, renderer)
		}
		return true
	})
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
