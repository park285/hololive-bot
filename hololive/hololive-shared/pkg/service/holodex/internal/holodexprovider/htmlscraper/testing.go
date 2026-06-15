package htmlscraper

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func NewTestServiceWithHTTPClient(
	httpClient *http.Client,
	logger *slog.Logger,
	baseURL string,
	fetchUpcoming func(ctx context.Context, channelID string) ([]*scraper.UpcomingEvent, error),
) *Service {
	return &Service{
		httpClient:    httpClient,
		logger:        logger,
		baseURL:       baseURL,
		fetchUpcoming: fetchUpcoming,
		memberNameMap: make(map[string]string),
	}
}
