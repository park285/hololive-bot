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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tidwall/gjson"
)

func TestFindVideosTabContentMatchesAdditionalLocales(t *testing.T) {
	cases := []struct {
		name      string
		tabTitle  string
		tabURL    string
		shouldHit bool
	}{
		{name: "english", tabTitle: "Videos", tabURL: "/channel/UC_TEST/videos", shouldHit: true},
		{name: "korean", tabTitle: "동영상", tabURL: "/channel/UC_TEST/videos", shouldHit: true},
		{name: "japanese", tabTitle: "動画", tabURL: "/channel/UC_TEST/videos", shouldHit: true},
		{name: "simplified_chinese", tabTitle: "视频", tabURL: "/channel/UC_TEST/about", shouldHit: true},
		{name: "traditional_chinese", tabTitle: "影片", tabURL: "/channel/UC_TEST/about", shouldHit: true},
		{name: "portuguese", tabTitle: "Vídeos", tabURL: "/channel/UC_TEST/about", shouldHit: true},
		{name: "russian", tabTitle: "Видео", tabURL: "/channel/UC_TEST/about", shouldHit: true},
		{name: "french", tabTitle: "Vidéos", tabURL: "/channel/UC_TEST/about", shouldHit: true},
		{name: "url_only", tabTitle: "Unknown", tabURL: "/channel/UC_TEST/videos", shouldHit: true},
		{name: "non_videos_tab", tabTitle: "Home", tabURL: "/channel/UC_TEST", shouldHit: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := `{"tabs":[{"tabRenderer":{"title":` + jsonQuote(tc.tabTitle) +
				`,"endpoint":{"commandMetadata":{"webCommandMetadata":{"url":` + jsonQuote(tc.tabURL) +
				`}}},"content":{"richGridRenderer":{"contents":[]}}}}]}`
			content, _ := findVideosTabContent(gjson.Parse(input).Get("tabs"))
			assert.Equal(t, tc.shouldHit, content.Exists(), "tab=%q url=%q", tc.tabTitle, tc.tabURL)
		})
	}
}

func jsonQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func TestParseLockupVideoViewModelHandlesSwappedMetadataParts(t *testing.T) {
	// Case 1: 기존 순서 (viewCount=0, published=1)
	lockupOrdered := `{
        "contentType": "LOCKUP_CONTENT_TYPE_VIDEO",
        "contentId": "vidABC",
        "contentImage": {"thumbnailViewModel": {"image": {"sources": []}}},
        "metadata": {"lockupMetadataViewModel": {
            "title": {"content": "Title A"},
            "metadata": {"contentMetadataViewModel": {
                "metadataRows": [{"metadataParts": [
                    {"text": {"content": "12K views"}},
                    {"text": {"content": "2 days ago"}}
                ]}]
            }}
        }}
    }`
	got := parseLockupVideoViewModel(gjson.Parse(lockupOrdered), "UC_X")
	assert.NotNil(t, got)
	assert.Equal(t, int64(12_000), got.ViewCount)
	assert.Equal(t, "2 days ago", got.PublishedText)

	// Case 2: 순서가 swap된 응답 (viewCount=1, published=0)
	lockupSwapped := `{
        "contentType": "LOCKUP_CONTENT_TYPE_VIDEO",
        "contentId": "vidXYZ",
        "contentImage": {"thumbnailViewModel": {"image": {"sources": []}}},
        "metadata": {"lockupMetadataViewModel": {
            "title": {"content": "Title B"},
            "metadata": {"contentMetadataViewModel": {
                "metadataRows": [{"metadataParts": [
                    {"text": {"content": "3 weeks ago"}},
                    {"text": {"content": "1.2M views"}}
                ]}]
            }}
        }}
    }`
	got = parseLockupVideoViewModel(gjson.Parse(lockupSwapped), "UC_X")
	assert.NotNil(t, got)
	assert.Equal(t, int64(1_200_000), got.ViewCount, "viewCount는 위치와 무관하게 숫자 패턴으로 식별되어야 함")
	assert.Equal(t, "3 weeks ago", got.PublishedText)
}

func TestCollectVideoRenderers_BoundedScan(t *testing.T) {
	var builder strings.Builder
	builder.WriteString(`{"contents":`)
	for range maxVideoRendererFallbackNodes + 32 {
		builder.WriteString(`{"child":`)
	}
	builder.WriteString(`{"videoRenderer":{"videoId":"too-deep","title":{"runs":[{"text":"Too Deep"}]}}}`)
	for range maxVideoRendererFallbackNodes + 32 {
		builder.WriteString(`}`)
	}
	builder.WriteString(`}`)

	renderers := collectVideoRenderers(gjson.Parse(builder.String()).Get("contents"), 1)
	assert.Empty(t, renderers)
}
