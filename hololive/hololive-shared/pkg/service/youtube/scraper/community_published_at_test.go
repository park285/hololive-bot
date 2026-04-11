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
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
)

func TestExtractPublishedAtFromHTML(t *testing.T) {
	t.Run("meta tag", func(t *testing.T) {
		publishedAt, err := extractPublishedAtFromHTML(`
			<html>
				<head>
					<meta itemprop="datePublished" content="2026-04-10T10:11:12+09:00">
				</head>
			</html>
		`)
		require.NoError(t, err)
		require.NotNil(t, publishedAt)
		assert.Equal(t, "2026-04-10T01:11:12Z", publishedAt.Format(time.RFC3339))
	})

	t.Run("json ld", func(t *testing.T) {
		publishedAt, err := extractPublishedAtFromHTML(`
			<html>
				<head>
					<script type="application/ld+json">
						{"datePublished":"2026-04-10T10:11:12+09:00"}
					</script>
				</head>
			</html>
		`)
		require.NoError(t, err)
		require.NotNil(t, publishedAt)
		assert.Equal(t, "2026-04-10T01:11:12Z", publishedAt.Format(time.RFC3339))
	})
}

func TestExtractCommunityPublishedAtFromHTML(t *testing.T) {
	publishedAt, err := extractCommunityPublishedAtFromHTML(`
		<html>
			<head>
				<meta itemprop="datePublished" content="2026-04-10T10:11:12+09:00">
			</head>
		</html>
	`)
	require.NoError(t, err)
	require.NotNil(t, publishedAt)
	assert.Equal(t, "2026-04-10T01:11:12Z", publishedAt.Format(time.RFC3339))
}

func TestResolveCommunityPostPublishedAt(t *testing.T) {
	client := NewClient(
		WithHTTPClient(&http.Client{
			Transport: communityRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, "/post/post-1", req.URL.Path)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`
						<html>
							<head>
								<meta itemprop="datePublished" content="2026-04-10T10:11:12+09:00">
							</head>
						</html>
					`)),
					Request: req,
				}, nil
			}),
		}),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	publishedAt, err := client.ResolveCommunityPostPublishedAt(context.Background(), "post-1")
	require.NoError(t, err)
	require.NotNil(t, publishedAt)
	assert.Equal(t, "2026-04-10T01:11:12Z", publishedAt.Format(time.RFC3339))
}

func TestResolveVideoPublishedAt(t *testing.T) {
	client := NewClient(
		WithHTTPClient(&http.Client{
			Transport: communityRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, "/watch", req.URL.Path)
				require.Equal(t, "video-1", req.URL.Query().Get("v"))
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`
						<html>
							<head>
								<meta itemprop="uploadDate" content="2026-04-10T10:11:12+09:00">
							</head>
						</html>
					`)),
					Request: req,
				}, nil
			}),
		}),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	publishedAt, err := client.ResolveVideoPublishedAt(context.Background(), "video-1")
	require.NoError(t, err)
	require.NotNil(t, publishedAt)
	assert.Equal(t, "2026-04-10T01:11:12Z", publishedAt.Format(time.RFC3339))
}
