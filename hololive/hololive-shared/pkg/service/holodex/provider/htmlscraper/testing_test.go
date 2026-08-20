package htmlscraper

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

type testYouTubeClient struct {
	fetchUpcoming func(context.Context, string) ([]*parser.UpcomingEvent, error)
}

func (c testYouTubeClient) GetUpcomingEvents(ctx context.Context, channelID string) ([]*parser.UpcomingEvent, error) {
	return c.fetchUpcoming(ctx, channelID)
}

func (c testYouTubeClient) GetUpcomingEventsWaitAdmission(ctx context.Context, channelID string) ([]*parser.UpcomingEvent, error) {
	return c.fetchUpcoming(ctx, channelID)
}

func (testYouTubeClient) GetRecentVideos(context.Context, string, int) ([]*parser.Video, error) {
	return nil, nil
}

func (testYouTubeClient) GetPopularVideos(context.Context, string, int) ([]*parser.Video, error) {
	return nil, nil
}

func (testYouTubeClient) GetChannelStats(context.Context, string) (*parser.ChannelStats, error) {
	return nil, nil
}

func (testYouTubeClient) GetChannelSnippet(context.Context, string) (*parser.ChannelSnippet, error) {
	return nil, nil
}

func (testYouTubeClient) SetProxyEnabled(bool) bool { return false }
func (testYouTubeClient) ProxyEnabled() bool        { return false }

func newTestServiceWithHTTPClient(
	httpClient *http.Client,
	logger *slog.Logger,
	baseURL string,
	fetchUpcoming func(context.Context, string) ([]*parser.UpcomingEvent, error),
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	config := settings.DefaultOfficialScheduleConfig()
	config.BaseURL = baseURL
	var youtubeClient YouTubeClient
	if fetchUpcoming != nil {
		youtubeClient = testYouTubeClient{fetchUpcoming: fetchUpcoming}
	}
	return NewServiceWithDependencies(
		nil,
		nil,
		ServiceDependencies{YouTube: youtubeClient, HTTP: httpClient},
		logger,
		settings.OfficialScheduleRuntimeConfig{
			OfficialSchedule:     config,
			MaxResponseBodyBytes: settings.DefaultMaxResponseBodyBytes,
		},
	)
}

func TestServiceRejectsNilYouTubeObjectResults(t *testing.T) {
	service := &Service{youtubeClient: testYouTubeClient{}, logger: slog.Default()}

	if stats, err := service.GetChannelStats(t.Context(), "channel"); stats != nil || err == nil {
		t.Fatalf("GetChannelStats() = (%v, %v), want (nil, error)", stats, err)
	}
	if snippet, err := service.GetChannelSnippet(t.Context(), "channel"); snippet != nil || err == nil {
		t.Fatalf("GetChannelSnippet() = (%v, %v), want (nil, error)", snippet, err)
	}
}
