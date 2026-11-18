package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

type thumbnailRoundTripper func(*http.Request) (*http.Response, error)

func (f thumbnailRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	out, err := f(req)
	if err != nil {
		return nil, fmt.Errorf("f: %w", err)
	}

	return out, nil
}

type trackedThumbnailBody struct {
	reader   io.Reader
	closeErr error
	closed   bool
}

func (b *trackedThumbnailBody) Read(p []byte) (int, error) {
	out, err := b.reader.Read(p)
	if err == nil {
		return out, nil
	}

	if errors.Is(err, io.EOF) {
		return out, io.EOF
	}

	return out, fmt.Errorf("read: %w", err)
}

func (b *trackedThumbnailBody) Close() error {
	b.closed = true
	return b.closeErr
}

type failingThumbnailReader struct{}

func (failingThumbnailReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func TestBroadcastThumbnailCandidatesPromoteMaxres(t *testing.T) {
	entry := handlercore.BroadcastHistoryEntry{
		VideoID:      testVideoID,
		ThumbnailURL: "https://i.ytimg.com/vi/AqxEw3kXcgU/hqdefault.jpg",
	}

	got := broadcastThumbnailCandidates(&entry)
	if len(got) == 0 {
		t.Fatal("expected candidates")
	}

	if got[0] != "https://i.ytimg.com/vi/AqxEw3kXcgU/maxresdefault.jpg" {
		t.Fatalf("first candidate = %q", got[0])
	}
}

func TestBroadcastThumbnailCandidatesRejectMismatchedStoredURL(t *testing.T) {
	entry := handlercore.BroadcastHistoryEntry{
		VideoID:      testVideoID,
		ThumbnailURL: "https://i.ytimg.com/vi/OtherVideo1/hqdefault.jpg",
	}

	got := broadcastThumbnailCandidates(&entry)
	if len(got) == 0 {
		t.Fatal("expected fallback candidates")
	}

	for _, candidate := range got {
		if strings.Contains(candidate, "OtherVideo1") {
			t.Fatalf("candidate %q uses mismatched stored video id", candidate)
		}
	}

	if got[0] != "https://i.ytimg.com/vi/AqxEw3kXcgU/maxresdefault.jpg" {
		t.Fatalf("first candidate = %q, want canonical maxres for requested video", got[0])
	}
}

func TestYouTubeThumbnailDownloaderFallsBack(t *testing.T) {
	var paths []string

	client := &http.Client{Transport: thumbnailRoundTripper(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if strings.HasSuffix(req.URL.Path, "/sddefault.jpg") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{testContentTypeJPEG}},
				Body:       io.NopCloser(strings.NewReader("jpeg")),
			}, nil
		}

		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("not found")),
		}, nil
	})}
	downloader := NewYouTubeThumbnailDownloader(client)

	body, contentType, err := downloader.Download(t.Context(), &handlercore.BroadcastHistoryEntry{VideoID: testVideoID})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	if string(body) != "jpeg" || contentType != testContentTypeJPEG {
		t.Fatalf("Download() = %q %q", string(body), contentType)
	}

	if len(paths) < 2 || !strings.HasSuffix(paths[0], "/maxresdefault.jpg") || !strings.HasSuffix(paths[len(paths)-1], "/sddefault.jpg") {
		t.Fatalf("paths = %v", paths)
	}
}

func TestYouTubeThumbnailDownloaderReportsNotFoundOnlyWhenEveryCandidateIs404(t *testing.T) {
	t.Parallel()

	download := func(status func(path string) int) error {
		client := &http.Client{Transport: thumbnailRoundTripper(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: status(req.URL.Path),
				Header:     http.Header{"Content-Type": []string{"text/plain"}},
				Body:       io.NopCloser(strings.NewReader("error")),
			}, nil
		})}

		_, _, err := (&youTubeThumbnailDownloader{client: client}).Download(t.Context(), &handlercore.BroadcastHistoryEntry{VideoID: testVideoID})

		return err
	}

	err := download(func(string) int { return http.StatusNotFound })
	if !errors.Is(err, handlercore.ErrBroadcastThumbnailNotFound) || !strings.Contains(err.Error(), "thumbnail status 404") {
		t.Fatalf("Download() error = %v, want ErrBroadcastThumbnailNotFound with status", err)
	}

	err = download(func(path string) int {
		if strings.HasSuffix(path, "/sddefault.jpg") {
			return http.StatusInternalServerError
		}

		return http.StatusNotFound
	})
	if err == nil || errors.Is(err, handlercore.ErrBroadcastThumbnailNotFound) {
		t.Fatalf("Download() error = %v, want a failure that is not ErrBroadcastThumbnailNotFound", err)
	}
}

func TestYouTubeThumbnailDownloaderRejectsNilEntry(t *testing.T) {
	downloader := NewYouTubeThumbnailDownloader(&http.Client{})

	_, _, err := downloader.Download(t.Context(), nil)
	if err == nil || !strings.Contains(err.Error(), "entry is required") {
		t.Fatalf("Download() error = %v", err)
	}
}

func TestYouTubeThumbnailDownloaderRejectsLooseVideoID(t *testing.T) {
	downloader := NewYouTubeThumbnailDownloader(&http.Client{})

	_, _, err := downloader.Download(t.Context(), &handlercore.BroadcastHistoryEntry{VideoID: "short1"})
	if err == nil || !strings.Contains(err.Error(), "invalid youtube video id") {
		t.Fatalf("Download() error = %v, want invalid video id", err)
	}
}

func TestValidateThumbnailResponseRejectsNil(t *testing.T) {
	_, err := validateThumbnailResponse(nil)
	if err == nil || !strings.Contains(err.Error(), "response is nil") {
		t.Fatalf("validateThumbnailResponse() error = %v", err)
	}
}

func TestYouTubeThumbnailDownloaderClosesEveryResponseBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		statusCode  int
		contentType string
		reader      io.Reader
		closeErr    error
		wantErr     bool
	}{
		{
			name:        "non-2xx",
			statusCode:  http.StatusNotFound,
			contentType: "text/plain",
			reader:      strings.NewReader("not found"),
			wantErr:     true,
		},
		{
			name:        "invalid content type",
			statusCode:  http.StatusOK,
			contentType: "text/plain",
			reader:      strings.NewReader("not an image"),
			wantErr:     true,
		},
		{
			name:        "oversized body",
			statusCode:  http.StatusOK,
			contentType: testContentTypeJPEG,
			reader:      io.LimitReader(zeroReader{}, maxBroadcastThumbnailBytes+1),
			wantErr:     true,
		},
		{
			name:        "read failure",
			statusCode:  http.StatusOK,
			contentType: testContentTypeJPEG,
			reader:      failingThumbnailReader{},
			wantErr:     true,
		},
		{
			name:        "success",
			statusCode:  http.StatusOK,
			contentType: testContentTypeJPEG,
			reader:      strings.NewReader("jpeg"),
		},
		{
			name:        "close failure",
			statusCode:  http.StatusOK,
			contentType: testContentTypeJPEG,
			reader:      strings.NewReader("jpeg"),
			closeErr:    errors.New("close failed"),
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := &trackedThumbnailBody{reader: tt.reader, closeErr: tt.closeErr}
			client := &http.Client{Transport: thumbnailRoundTripper(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tt.statusCode,
					Header:     http.Header{"Content-Type": []string{tt.contentType}},
					Body:       body,
				}, nil
			})}
			downloader := &youTubeThumbnailDownloader{client: client}

			_, _, err := downloader.downloadCandidate(t.Context(), "https://i.ytimg.com/vi/AqxEw3kXcgU/maxresdefault.jpg")
			if (err != nil) != tt.wantErr {
				t.Fatalf("downloadCandidate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !body.closed {
				t.Fatal("response body was not closed")
			}
		})
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}

	return len(p), nil
}
