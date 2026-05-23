package htmlscraper

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type TestOption func(*Service)

func WithTestHTTPClient(c *http.Client) TestOption {
	return func(s *Service) { s.httpClient = c }
}

func WithTestBaseURL(url string) TestOption {
	return func(s *Service) { s.baseURL = url }
}

func WithTestFetchUpcoming(fn func(ctx context.Context, channelID string) ([]*scraper.UpcomingEvent, error)) TestOption {
	return func(s *Service) { s.fetchUpcoming = fn }
}

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

func NewTestServiceMinimal(logger *slog.Logger, opts ...TestOption) *Service {
	s := &Service{
		logger:        logger,
		memberNameMap: make(map[string]string),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
