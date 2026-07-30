package holodexprovider

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/service/holodex/provider/htmlscraper"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

func newScraperServiceForTest(
	httpClient *http.Client,
	logger *slog.Logger,
	baseURL string,
	fetchUpcoming func(ctx context.Context, channelID string) ([]*parser.UpcomingEvent, error),
) *htmlscraper.Service {
	return htmlscraper.NewTestServiceWithHTTPClient(httpClient, logger, baseURL, fetchUpcoming)
}
