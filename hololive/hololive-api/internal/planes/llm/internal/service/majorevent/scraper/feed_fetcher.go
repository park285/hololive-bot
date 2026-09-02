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
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/park285/shared-go/v2/pkg/httputil"
)

// FeedFetcher는 RSS 원문을 HTTP로 가져온다.
type FeedFetcher struct {
	client     *http.Client
	userAgent  string
	maxBodyLen int64
}

// NewFeedFetcher는 FeedFetcher를 생성한다.
func NewFeedFetcher(client *http.Client, config FeedFetcherConfig) *FeedFetcher {
	if client == nil {
		client = httputil.NewExternalAPIClient(defaultFeedHTTPTimeout)
	}

	userAgent := strings.TrimSpace(config.UserAgent)
	if userAgent == "" {
		userAgent = defaultFeedUserAgent
	}

	maxBodyLen := config.MaxBodySize
	if maxBodyLen <= 0 {
		maxBodyLen = defaultMaxBodyBytes
	}

	return &FeedFetcher{
		client:     client,
		userAgent:  userAgent,
		maxBodyLen: maxBodyLen,
	}
}

// Fetch는 지정 URL의 RSS 원문을 조회한다.
func (f *FeedFetcher) Fetch(ctx context.Context, feedURL string) ([]byte, error) {
	if f == nil || f.client == nil {
		return nil, errors.New("fetch feed: fetcher is nil")
	}

	resp, err := f.fetchResponse(ctx, feedURL) //nolint:bodyclose // readResponseBody가 모든 반환 경로에서 resp.Body를 닫고 close 오류도 보존한다.
	if err != nil {
		return nil, fmt.Errorf("fetch response: %w", err)
	}

	body, err := f.readResponseBody(resp)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	return body, nil
}

func (f *FeedFetcher) readResponseBody(resp *http.Response) ([]byte, error) {
	if err := f.validateResponseBody(resp); err != nil {
		return nil, fmt.Errorf("validate response body: %w", err)
	}

	out, err := f.readAndCloseBody(resp.Body)
	if err != nil {
		return out, fmt.Errorf("read and close body: %w", err)
	}

	return out, nil
}

func (f *FeedFetcher) validateResponseBody(resp *http.Response) error {
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if closeErr := resp.Body.Close(); closeErr != nil {
			return fmt.Errorf("fetch feed: close error response body: %w", closeErr)
		}

		return fmt.Errorf("fetch feed: unexpected status %d", resp.StatusCode)
	}

	if resp.ContentLength > f.maxBodyLen {
		return f.closeOversizedBody(resp.Body)
	}

	return nil
}

func (f *FeedFetcher) readAndCloseBody(responseBody io.ReadCloser) ([]byte, error) {
	body, err := httputil.ReadAllAndCloseWithDrainLimit(responseBody, f.maxBodyLen, 64<<10)
	if err != nil {
		if errors.Is(err, httputil.ErrResponseBodyTooLarge) {
			return nil, fmt.Errorf("fetch feed: body exceeds %d bytes: %w", f.maxBodyLen, err)
		}

		return nil, fmt.Errorf("fetch feed: read or close body: %w", err)
	}

	return body, nil
}

func (f *FeedFetcher) closeOversizedBody(responseBody io.ReadCloser) error {
	if closeErr := responseBody.Close(); closeErr != nil {
		return fmt.Errorf("fetch feed: body exceeds %d bytes; close response body: %w", f.maxBodyLen, closeErr)
	}

	return fmt.Errorf("fetch feed: body exceeds %d bytes", f.maxBodyLen)
}

func (f *FeedFetcher) fetchResponse(ctx context.Context, feedURL string) (*http.Response, error) {
	trimmedURL := strings.TrimSpace(feedURL)
	if trimmedURL == "" {
		return nil, errors.New("fetch feed: feed url is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmedURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("fetch feed: build request: %w", err)
	}

	req.Header.Set("User-Agent", f.userAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch feed: do request: %w", err)
	}

	out, err := requireFeedResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("require feed response: %w", err)
	}

	return out, nil
}

func requireFeedResponse(resp *http.Response) (*http.Response, error) {
	if resp == nil {
		return nil, errors.New("fetch feed: response is nil")
	}

	if resp.Body == nil {
		return nil, errors.New("fetch feed: response body is nil")
	}

	return resp, nil
}
