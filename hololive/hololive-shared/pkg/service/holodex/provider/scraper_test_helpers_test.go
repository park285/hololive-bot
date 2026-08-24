package holodexprovider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/holodex/provider/htmlscraper"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

func newScraperServiceForTest(
	httpClient *http.Client,
	logger *slog.Logger,
	baseURL string,
	fetchUpcoming func(ctx context.Context, channelID string) ([]*parser.UpcomingEvent, error),
	members ...*domain.Member,
) *htmlscraper.Service {
	config := settings.DefaultOfficialScheduleConfig()

	config.BaseURL = baseURL

	var youtubeClient htmlscraper.YouTubeClient

	if fetchUpcoming != nil {
		youtubeClient = testScraperYouTubeClient{fetchUpcoming: fetchUpcoming}
	}

	return htmlscraper.NewServiceWithDependencies(
		nil,
		testScraperMembers(members),
		htmlscraper.ServiceDependencies{YouTube: youtubeClient, HTTP: httpClient},
		logger,
		settings.OfficialScheduleRuntimeConfig{
			OfficialSchedule:     config,
			MaxResponseBodyBytes: settings.DefaultMaxResponseBodyBytes,
		},
	)
}

type testScraperYouTubeClient struct {
	fetchUpcoming func(context.Context, string) ([]*parser.UpcomingEvent, error)
}

func (client testScraperYouTubeClient) GetUpcomingEvents(ctx context.Context, channelID string) ([]*parser.UpcomingEvent, error) {
	out, err := client.fetchUpcoming(ctx, channelID)
	if err != nil {
		return out, fmt.Errorf("fetch upcoming: %w", err)
	}

	return out, nil
}

func (client testScraperYouTubeClient) GetUpcomingEventsWaitAdmission(ctx context.Context, channelID string) ([]*parser.UpcomingEvent, error) {
	out, err := client.fetchUpcoming(ctx, channelID)
	if err != nil {
		return out, fmt.Errorf("fetch upcoming: %w", err)
	}

	return out, nil
}

func (testScraperYouTubeClient) GetRecentVideos(context.Context, string, int) ([]*parser.Video, error) {
	return nil, nil
}

func (testScraperYouTubeClient) GetPopularVideos(context.Context, string, int) ([]*parser.Video, error) {
	return nil, nil
}

var errScraperStubUnsupported = errors.New("scraper test stub does not implement this call")

func (testScraperYouTubeClient) GetChannelStats(context.Context, string) (*parser.ChannelStats, error) {
	return nil, errScraperStubUnsupported
}

func (testScraperYouTubeClient) GetChannelSnippet(context.Context, string) (*parser.ChannelSnippet, error) {
	return nil, errScraperStubUnsupported
}
func (testScraperYouTubeClient) SetProxyEnabled(bool) bool { return false }
func (testScraperYouTubeClient) ProxyEnabled() bool        { return false }

type testScraperMembers []*domain.Member

func (members testScraperMembers) GetAllMembers() []*domain.Member     { return members }
func (testScraperMembers) FindMemberByChannelID(string) *domain.Member { return nil }
func (testScraperMembers) FindMemberByName(string) *domain.Member      { return nil }
func (testScraperMembers) FindMemberByAlias(string) *domain.Member     { return nil }
func (testScraperMembers) GetChannelIDs() []string                     { return nil }
func (members testScraperMembers) WithContext(context.Context) domain.MemberDataProvider {
	return members
}
func (testScraperMembers) FindMembersByName(string) []*domain.Member  { return nil }
func (testScraperMembers) FindMembersByAlias(string) []*domain.Member { return nil }
