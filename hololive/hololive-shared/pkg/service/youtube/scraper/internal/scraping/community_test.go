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
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
)

type communityRoundTripFunc func(req *http.Request) (*http.Response, error)

func (f communityRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGetCommunityPosts_404TreatAsEmpty(t *testing.T) {
	var attempts atomic.Int32

	httpClient := &http.Client{
		Transport: communityRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts.Add(1)
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	}

	client := NewClient(
		WithHTTPClient(httpClient),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	posts, err := client.GetCommunityPosts(context.Background(), "UC_TEST", 5)
	require.NoError(t, err)
	require.Empty(t, posts)
	require.Equal(t, int32(1), attempts.Load())

	posts, err = client.GetCommunityPosts(context.Background(), "UC_TEST", 5)
	require.NoError(t, err)
	require.Empty(t, posts)
	require.Equal(t, int32(1), attempts.Load(), "community missing cache should skip second network call")
}

func TestGetCommunityPosts_404DoesNotRecordHTMLCooldown(t *testing.T) {
	httpClient := &http.Client{
		Transport: communityRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	}

	client := NewClient(
		WithHTTPClient(httpClient),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
		WithStateStore(newChannelHealthTestStore()),
		WithChannelHealthPolicy(ChannelHealthPolicy{
			HTTPStatusBase: time.Hour,
			HTTPStatusMax:  time.Hour,
		}),
	)

	posts, err := client.GetCommunityPosts(context.Background(), "UC_TEST", 5)
	require.NoError(t, err)
	require.Empty(t, posts)

	wait, skip := client.channelHealth.ShouldSkip(context.Background(), "UC_TEST", FailureSourceHTML, time.Now())
	require.False(t, skip, "community /posts 404 should not cooldown the shared HTML source; wait=%s", wait)
}

func TestParseBackstagePostIncludesUpstreamPostID(t *testing.T) {
	client := &Client{}
	post := client.parseBackstagePost(gjson.Parse(`{
		"postId": "UgkxDirect123",
		"publishedTimeText": {"simpleText": "2026-04-10T10:11:12+09:00"}
	}`))

	require.NotNil(t, post)
	require.Equal(t, "UgkxDirect123", post.PostID)
	require.Equal(t, "UgkxDirect123", post.UpstreamPostID)
}

func TestParseBackstagePostFallsBackToPostURLForUpstreamPostID(t *testing.T) {
	client := &Client{}
	post := client.parseBackstagePost(gjson.Parse(`{
		"actionButtons": {
			"commentActionButtonsRenderer": {
				"replyButton": {
					"buttonRenderer": {
						"navigationEndpoint": {
							"commandMetadata": {
								"webCommandMetadata": {
									"url": "/post/UgkxFallback456?lc=1"
								}
							}
						}
					}
				}
			}
		},
		"publishedTimeText": {"simpleText": "2026-04-10T10:11:12+09:00"}
	}`))

	require.NotNil(t, post)
	require.Equal(t, "UgkxFallback456", post.PostID)
	require.Equal(t, "UgkxFallback456", post.UpstreamPostID)
}
