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

//go:build integration

package scraping

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 통합 테스트 - 실제 YouTube 호출
// 실행: go test -tags=integration -v ./internal/service/youtube/scraper/...

func newIntegrationClient(t *testing.T) *Client {
	t.Helper()

	return NewClient(testYouTubeConfig())
}

func TestGetShorts_Integration(t *testing.T) {
	client := newIntegrationClient(t)
	ctx := context.Background()

	// Pekora 채널 (쇼츠 콘텐츠가 있는 채널)
	shorts, err := client.GetShorts(ctx, "UC1DCedRgGHBdm81E1llLhOQ", 10)
	require.NoError(t, err)

	t.Logf("Found %d shorts", len(shorts))
	for i, s := range shorts {
		t.Logf("  [%d] %s (ID: %s, Views: %d)", i+1, s.Title, s.VideoID, s.ViewCount)
	}

	if len(shorts) > 0 {
		assert.NotEmpty(t, shorts[0].VideoID, "VideoID should not be empty")
		assert.NotEmpty(t, shorts[0].Title, "Title should not be empty")
	}
}

func TestGetRecentVideos_Integration(t *testing.T) {
	client := newIntegrationClient(t)
	ctx := context.Background()

	t.Run("Pekora_MustHaveVideos", func(t *testing.T) {
		// Pekora: 비디오 있음이 보장되는 채널
		videos, err := client.GetRecentVideos(ctx, "UC1DCedRgGHBdm81E1llLhOQ", 10)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(videos), 1, "Pekora 채널은 최소 1개 비디오가 있어야 함")
		assert.NotEmpty(t, videos[0].VideoID)
		assert.NotEmpty(t, videos[0].Title)
		t.Logf("Found %d videos, first: %s", len(videos), videos[0].Title)
	})

	// 실패했던 채널들 (에러 없음만 검증)
	failedChannels := []struct {
		name string
		id   string
	}{
		{"FailedChannel1", "UCp_3ej2br9l9L1DSoHVDZGw"},
		{"FailedChannel2", "UCeCWj-SiJG9SWN6wGORiLmw"},
		{"FailedChannel3", "UC2xXx1m1jeL0W84_0jTg-Yw"},
		{"FailedChannel4", "UCoW8qQy80mKH0RJTKAK-nNA"},
	}

	for _, ch := range failedChannels {
		t.Run(ch.name, func(t *testing.T) {
			videos, err := client.GetRecentVideos(ctx, ch.id, 10)
			require.NoError(t, err, "채널 %s에서 에러 발생", ch.name)
			t.Logf("[%s] Found %d videos", ch.name, len(videos))
		})
	}
}

func TestGetRecentVideos_Integration_ResponseContextOnlyRegression(t *testing.T) {
	client := newIntegrationClient(t)
	ctx := context.Background()

	// 2026-02-17 재현 이슈 채널:
	// ytInitialData가 responseContext-only로 선택되어 tabs 경로를 못 찾던 케이스를 회귀 검증한다.
	channelID := "UCu2n3qHuOuQIygREMnWeQWg"

	videos, err := client.GetRecentVideos(ctx, channelID, 10)
	require.NoError(t, err, "재현 채널에서 tabs missing 에러/빈 파싱 실패가 없어야 함")
	require.GreaterOrEqual(t, len(videos), 1, "재현 채널은 최소 1개 이상의 비디오를 파싱해야 함")

	t.Logf("Regression channel=%s videos=%d first_video=%s", channelID, len(videos), videos[0].VideoID)
}

func TestGetCommunityPosts_Integration(t *testing.T) {
	client := newIntegrationClient(t)
	ctx := context.Background()

	channels := []struct {
		name string
		id   string
	}{
		{"Pekora", "UC1DCedRgGHBdm81E1llLhOQ"},
		{"Marine", "UCCzUftO8KOVkV4wQG1vkUvg"},
		{"Miko", "UC-hM6YJuNYVAmUWxeIr9FeA"},
		{"Suisei", "UC5CwaMl1eIgY8h02uZw7u8A"},
	}

	var totalPosts int
	for _, ch := range channels {
		posts, err := client.GetCommunityPosts(ctx, ch.id, 5)
		require.NoError(t, err, "channel: %s", ch.name)

		t.Logf("[%s] Found %d community posts", ch.name, len(posts))
		for i, p := range posts {
			preview := p.ContentText
			if len(preview) > 60 {
				preview = preview[:60] + "..."
			}
			t.Logf("  [%d] PostID: %s", i+1, p.PostID)
			t.Logf("       Content: %s", preview)
			t.Logf("       Published: %s, Likes: %d", p.PublishedText, p.LikeCount)
		}
		totalPosts += len(posts)
	}

	t.Logf("Total community posts found across all channels: %d", totalPosts)
}
