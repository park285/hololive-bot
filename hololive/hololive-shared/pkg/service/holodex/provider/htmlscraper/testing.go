package htmlscraper

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
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

func (s *Service) OfficialScheduleBaseURLForTest() string {
	if s == nil {
		return ""
	}
	return s.officialSchedule.BaseURL
}

func (s *Service) OfficialScheduleMaxResponseBodyBytesForTest() int64 {
	if s == nil {
		return 0
	}
	return s.maxResponseBodyBytes
}

func (s *Service) SetOfficialScheduleIdentityForTest(members []*domain.Member) {
	if s == nil {
		return
	}
	s.identityIndex = buildOfficialScheduleIdentityIndex(officialScheduleTestMembers(members))
}

type officialScheduleTestMembers []*domain.Member

func (m officialScheduleTestMembers) GetAllMembers() []*domain.Member { return m }
func (officialScheduleTestMembers) FindMemberByChannelID(string) *domain.Member {
	return nil
}
func (officialScheduleTestMembers) FindMemberByName(string) *domain.Member { return nil }
func (officialScheduleTestMembers) FindMemberByAlias(string) *domain.Member {
	return nil
}
func (officialScheduleTestMembers) GetChannelIDs() []string { return nil }
func (m officialScheduleTestMembers) WithContext(context.Context) domain.MemberDataProvider {
	return m
}
func (officialScheduleTestMembers) FindMembersByName(string) []*domain.Member {
	return nil
}
func (officialScheduleTestMembers) FindMembersByAlias(string) []*domain.Member {
	return nil
}
