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
