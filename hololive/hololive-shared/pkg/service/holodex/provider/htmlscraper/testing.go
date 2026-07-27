package htmlscraper

import (
	"context"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	"log/slog"
	"net/http"
)

func NewTestServiceWithHTTPClient(
	httpClient *http.Client,
	logger *slog.Logger,
	baseURL string,
	fetchUpcoming func(ctx context.Context, channelID string) ([]*parser.UpcomingEvent, error),
) *Service {
	return &Service{
		httpClient:    httpClient,
		logger:        logger,
		baseURL:       baseURL,
		fetchUpcoming: fetchUpcoming,
		memberNameMap: make(map[string]string),
	}
}
