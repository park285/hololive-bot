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

package scraping

import (
	"context"
	"fmt"

	"github.com/tidwall/gjson"
)

func (c *Client) GetShorts(ctx context.Context, channelID string, maxResults int) ([]*Short, error) {
	url := fmt.Sprintf("https://www.youtube.com/channel/%s/shorts", channelID)
	html, err := c.fetchChannelSourcePage(ctx, "shorts", channelID, url, FailureSourceHTML, HighFrequencyChannelFetchPolicy)
	if err != nil {
		return nil, err
	}

	jsonStr, err := extractYtInitialData(html)
	if err != nil {
		return nil, c.recordParserDrift(ctx, "shorts", "extract_yt_initial_data", channelID, url, FailureSourceHTML, html, err)
	}

	data := gjson.Parse(jsonStr)
	if err := checkAlerts(&data); err != nil {
		return nil, err
	}

	shortItems := extractShortsLockupViewModels(&data)
	shorts := c.parseShortsLockupViewModels(shortItems, maxResults)
	c.recordChannelSourceSuccess(ctx, channelID, FailureSourceHTML)
	return shorts, nil
}

func extractShortsLockupViewModels(data *gjson.Result) []gjson.Result {
	var shortItems []gjson.Result
	data.Get("contents.twoColumnBrowseResultsRenderer.tabs").ForEach(func(_, tab gjson.Result) bool {
		if tab.Get("tabRenderer.title").String() != "Shorts" {
			return true
		}
		contents := tab.Get("tabRenderer.content.richGridRenderer.contents")
		appendShortsLockupViewModels(&shortItems, &contents)
		return false
	})
	return shortItems
}

func appendShortsLockupViewModels(shortItems *[]gjson.Result, contents *gjson.Result) {
	contents.ForEach(func(_, item gjson.Result) bool {
		shortsRenderer := item.Get("richItemRenderer.content.shortsLockupViewModel")
		if shortsRenderer.Exists() {
			*shortItems = append(*shortItems, shortsRenderer)
		}
		return true
	})
}

func (c *Client) parseShortsLockupViewModels(shortItems []gjson.Result, maxResults int) []*Short {
	shorts := make([]*Short, 0, min(len(shortItems), maxResults))
	for i, item := range shortItems {
		if i >= maxResults {
			break
		}
		short := c.parseShortsLockupViewModel(&item)
		if short != nil {
			shorts = append(shorts, short)
		}
	}
	return shorts
}

// parseShortsLockupViewModel: shortsLockupViewModel JSON을 Short 구조체로 변환
func (c *Client) parseShortsLockupViewModel(short *gjson.Result) *Short {
	videoID := short.Get("onTap.innertubeCommand.reelWatchEndpoint.videoId").String()
	if videoID == "" {
		return nil
	}

	var thumbnails []Thumbnail
	short.Get("thumbnail.sources").ForEach(func(_, t gjson.Result) bool {
		thumbnails = append(thumbnails, Thumbnail{
			URL:    t.Get("url").String(),
			Width:  int(t.Get("width").Int()),
			Height: int(t.Get("height").Int()),
		})
		return true
	})

	viewCountText := short.Get("overlayMetadata.secondaryText.content").String()
	viewCount := parseShortNumber(viewCountText)

	return &Short{
		VideoID:   videoID,
		Title:     short.Get("overlayMetadata.primaryText.content").String(),
		Thumbnail: thumbnails,
		ViewCount: viewCount,
	}
}
