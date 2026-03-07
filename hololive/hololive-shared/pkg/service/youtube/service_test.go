package youtube

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func TestNewYouTubeService_RejectsPlaceholderAPIKey(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	_, err := NewYouTubeService(context.Background(), "your_youtube_api_key", nil, scraper.ProxyConfig{}, nil, logger)
	if err == nil {
		t.Fatal("NewYouTubeService() expected placeholder api key error, got nil")
	}
	if !strings.Contains(err.Error(), "placeholder value") {
		t.Fatalf("unexpected error: %v", err)
	}
}
