package holodexprovider

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/internal/service/holodex/provider/htmlscraper"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// newScraperServiceForTest는 공식 일정 API만 쓰는 scraper facade를 만든다. Holodex 채널·일정·live-status 조회는
// scraper를 보조 원천으로 쓰지 않는다(DEC-20260926-hololive-source-fallbacks-retirement).
func newScraperServiceForTest(
	httpClient *http.Client,
	logger *slog.Logger,
	baseURL string,
) *htmlscraper.Service {
	config := settings.DefaultOfficialScheduleConfig()

	config.BaseURL = baseURL

	service, err := htmlscraper.NewService(
		context.Background(),
		testScraperMembers(nil),
		httpClient,
		logger,
		settings.OfficialScheduleRuntimeConfig{
			OfficialSchedule:     config,
			MaxResponseBodyBytes: settings.DefaultMaxResponseBodyBytes,
		},
	)
	if err != nil {
		// 고정 멤버 목록은 적재 오류가 없으므로 여기서 실패하면 test double 구성이 잘못된 것이다.
		panic(fmt.Sprintf("newScraperServiceForTest: %v", err))
	}

	return service
}

type testScraperMembers []*domain.Member

func (members testScraperMembers) LoadAllMembers(context.Context) ([]*domain.Member, error) {
	return members, nil
}

func (testScraperMembers) FindMemberByChannelID(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

func (testScraperMembers) FindMemberByName(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

func (testScraperMembers) FindMemberByAlias(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

func (testScraperMembers) GetChannelIDs(context.Context) ([]string, error) { return []string{}, nil }

func (testScraperMembers) FindMembersByName(context.Context, string) ([]*domain.Member, error) {
	return []*domain.Member{}, nil
}

func (testScraperMembers) FindMembersByAlias(context.Context, string) ([]*domain.Member, error) {
	return []*domain.Member{}, nil
}
