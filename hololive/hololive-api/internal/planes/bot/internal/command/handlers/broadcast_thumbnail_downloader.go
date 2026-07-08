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

package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/net/imagehost"
	"github.com/park285/shared-go/pkg/netguard"
)

type youTubeThumbnailDownloader struct {
	client *http.Client
}

const maxBroadcastThumbnailBytes = 12 << 20

var youtubeVideoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func NewYouTubeThumbnailDownloader(client *http.Client) BroadcastThumbnailDownloader {
	if client == nil {
		policy := netguard.Policy{
			Timeout:      5 * time.Second,
			AllowHost:    imagehost.ThumbnailHosts.AllowsHost,
			AllowedPorts: []string{"443"},
			Schemes:      []string{"https"},
		}
		client = netguard.GuardedClient(&http.Client{Timeout: 10 * time.Second}, policy)
		client.CheckRedirect = netguard.RedirectPolicy(netguard.RedirectConfig{Policy: policy, MaxRedirects: 3})
	}
	return &youTubeThumbnailDownloader{client: client}
}

func (d *youTubeThumbnailDownloader) Download(ctx context.Context, entry *handlercore.BroadcastHistoryEntry) (image []byte, contentType string, err error) {
	if d == nil || d.client == nil {
		return nil, "", errors.New("thumbnail downloader not configured")
	}
	if entry == nil {
		return nil, "", errors.New("broadcast history entry is required")
	}
	target := *entry
	target.VideoID = strings.TrimSpace(target.VideoID)
	if !validYouTubeVideoID(target.VideoID) {
		return nil, "", fmt.Errorf("invalid youtube video id: %q", target.VideoID)
	}

	var lastErr error
	for _, candidate := range broadcastThumbnailCandidates(&target) {
		image, contentType, err := d.downloadCandidate(ctx, candidate)
		if err == nil {
			return image, contentType, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no thumbnail candidates")
	}
	return nil, "", lastErr
}

func (d *youTubeThumbnailDownloader) downloadCandidate(ctx context.Context, rawURL string) (image []byte, contentType string, err error) {
	if err := imagehost.ThumbnailHosts.ValidateURL(rawURL); err != nil {
		return nil, "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, "", fmt.Errorf("create thumbnail request: %w", err)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request thumbnail: %w", err)
	}
	bodyReader, err := thumbnailResponseBody(resp)
	if err != nil {
		return nil, "", err
	}

	contentType, err = validateThumbnailResponse(resp)
	if err != nil {
		return nil, "", err
	}
	body, err := readAndCloseBroadcastThumbnailBody(bodyReader)
	if err != nil {
		return nil, "", err
	}
	return body, contentType, nil
}

func thumbnailResponseBody(resp *http.Response) (io.ReadCloser, error) {
	if resp == nil {
		return nil, errors.New("thumbnail response is nil")
	}
	if resp.Body == nil {
		return nil, errors.New("thumbnail response body is nil")
	}
	return resp.Body, nil
}

func closeThumbnailBody(body io.Closer) error {
	if body == nil {
		return nil
	}
	if closeErr := body.Close(); closeErr != nil {
		return fmt.Errorf("close thumbnail body: %w", closeErr)
	}
	return nil
}

func readAndCloseBroadcastThumbnailBody(body io.ReadCloser) (data []byte, err error) {
	defer func() {
		if closeErr := closeThumbnailBody(body); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	return readBroadcastThumbnailBody(body)
}

func validateThumbnailResponse(resp *http.Response) (string, error) {
	if resp == nil {
		return "", errors.New("thumbnail response is nil")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("thumbnail status %d", resp.StatusCode)
	}
	contentType := normalizeThumbnailContentType(resp.Header.Get("Content-Type"))
	if contentType == "" {
		return "", fmt.Errorf("unsupported thumbnail content type %q", resp.Header.Get("Content-Type"))
	}
	return contentType, nil
}

func readBroadcastThumbnailBody(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxBroadcastThumbnailBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read thumbnail: %w", err)
	}
	if len(data) == 0 {
		return nil, errors.New("thumbnail body is empty")
	}
	if len(data) > maxBroadcastThumbnailBytes {
		return nil, errors.New("thumbnail body exceeds size limit")
	}
	return data, nil
}

func broadcastThumbnailCandidates(entry *handlercore.BroadcastHistoryEntry) []string {
	candidates := make([]string, 0, 6)
	if thumbnailURLMatchesVideo(entry.ThumbnailURL, entry.VideoID) {
		if promoted := promoteYouTubeThumbnailURL(entry.ThumbnailURL, "maxresdefault.jpg"); promoted != "" {
			candidates = append(candidates, promoted)
		}
		candidates = append(candidates, strings.TrimSpace(entry.ThumbnailURL))
	}
	for _, name := range []string{"maxresdefault.jpg", "sddefault.jpg", "hqdefault.jpg", "mqdefault.jpg"} {
		candidates = append(candidates, fmt.Sprintf("https://i.ytimg.com/vi/%s/%s", entry.VideoID, name))
	}
	return uniqueStrings(candidates)
}

func promoteYouTubeThumbnailURL(rawURL, filename string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" || !imagehost.ThumbnailHosts.AllowsURL(trimmed) {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	parsed.Path = path.Join(path.Dir(parsed.Path), filename)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func thumbnailURLMatchesVideo(rawURL, videoID string) bool {
	videoID = strings.TrimSpace(videoID)
	if videoID == "" || !validYouTubeVideoID(videoID) {
		return false
	}
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" || !imagehost.ThumbnailHosts.AllowsURL(trimmed) {
		return false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	return thumbnailPathContainsVideoID(parsed.EscapedPath(), videoID)
}

func thumbnailPathContainsVideoID(escapedPath, videoID string) bool {
	if escapedPath == "" {
		return false
	}
	for segment := range strings.SplitSeq(escapedPath, "/") {
		unescaped, err := url.PathUnescape(segment)
		if err != nil {
			unescaped = segment
		}
		if unescaped == videoID {
			return true
		}
	}
	return false
}

func normalizeThumbnailContentType(raw string) string {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return ""
	}
	switch strings.ToLower(mediaType) {
	case "image/jpeg", "image/png", "image/webp":
		return strings.ToLower(mediaType)
	default:
		return ""
	}
}

func validYouTubeVideoID(videoID string) bool {
	return youtubeVideoIDPattern.MatchString(strings.TrimSpace(videoID))
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
