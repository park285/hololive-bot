package htmlscraper

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

func NewTestServiceWithHTTPClient(
	httpClient *http.Client,
	logger *slog.Logger,
	baseURL string,
	fetchUpcoming func(ctx context.Context, channelID string) ([]*parser.UpcomingEvent, error),
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	config := settings.DefaultOfficialScheduleConfig()
	config.BaseURL = baseURL
	return &Service{
		httpClient:           httpClient,
		logger:               logger,
		officialSchedule:     config,
		maxResponseBodyBytes: settings.DefaultMaxResponseBodyBytes,
		fetchUpcoming:        fetchUpcoming,
		identityIndex:        officialScheduleIdentityIndex{},
	}
}
