package handlers

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

type thumbnailRoundTripper func(*http.Request) (*http.Response, error)

func (f thumbnailRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestBroadcastThumbnailCandidatesPromoteMaxres(t *testing.T) {
	entry := handlercore.BroadcastHistoryEntry{
		VideoID:      "AqxEw3kXcgU",
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
		VideoID:      "AqxEw3kXcgU",
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
				Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
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

	body, contentType, err := downloader.Download(t.Context(), &handlercore.BroadcastHistoryEntry{VideoID: "AqxEw3kXcgU"})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if string(body) != "jpeg" || contentType != "image/jpeg" {
		t.Fatalf("Download() = %q %q", string(body), contentType)
	}
	if len(paths) < 2 || !strings.HasSuffix(paths[0], "/maxresdefault.jpg") || !strings.HasSuffix(paths[len(paths)-1], "/sddefault.jpg") {
		t.Fatalf("paths = %v", paths)
	}
}
