package holodexprovider

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/service/holodex/internal/holodexprovider/htmlscraper"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func newScraperServiceForTest(
	httpClient *http.Client,
	logger *slog.Logger,
	baseURL string,
	fetchUpcoming func(ctx context.Context, channelID string) ([]*scraper.UpcomingEvent, error),
) *ScraperService {
	return htmlscraper.NewTestServiceWithHTTPClient(httpClient, logger, baseURL, fetchUpcoming)
}
