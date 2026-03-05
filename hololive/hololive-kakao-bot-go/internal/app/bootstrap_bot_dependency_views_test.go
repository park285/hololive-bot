package app

import (
	"context"
	"testing"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
)

type stubIrisClient struct{}

func (s *stubIrisClient) SendMessage(ctx context.Context, room, message string, opts ...iris.SendOption) error {
	return nil
}
func (s *stubIrisClient) SendImage(ctx context.Context, room, imageBase64 string) error { return nil }
func (s *stubIrisClient) Ping(ctx context.Context) bool                                 { return true }
func (s *stubIrisClient) GetConfig(ctx context.Context) (*iris.Config, error)           { return nil, nil }
func (s *stubIrisClient) Decrypt(ctx context.Context, data string) (string, error)      { return data, nil }

type stubMemberDataProvider struct{}

func (s *stubMemberDataProvider) FindMemberByChannelID(channelID string) *domain.Member { return nil }
func (s *stubMemberDataProvider) FindMemberByName(name string) *domain.Member           { return nil }
func (s *stubMemberDataProvider) FindMemberByAlias(alias string) *domain.Member         { return nil }
func (s *stubMemberDataProvider) GetChannelIDs() []string                               { return nil }
func (s *stubMemberDataProvider) GetAllMembers() []*domain.Member                       { return nil }
func (s *stubMemberDataProvider) WithContext(ctx context.Context) domain.MemberDataProvider {
	return s
}
func (s *stubMemberDataProvider) FindMembersByName(name string) []*domain.Member   { return nil }
func (s *stubMemberDataProvider) FindMembersByAlias(alias string) []*domain.Member { return nil }

type stubYouTubeScheduler struct{}

func (s *stubYouTubeScheduler) Start(ctx context.Context) {}
func (s *stubYouTubeScheduler) Stop()                     {}

type stubSettingsReadWriter struct{}

func (s *stubSettingsReadWriter) Get() settings.Settings { return settings.Settings{} }
func (s *stubSettingsReadWriter) Update(newSettings settings.Settings) error {
	return nil
}

func TestBuildBotIngestionRuntimeDependencies(t *testing.T) {
	t.Run("nil dependencies", func(t *testing.T) {
		view := buildBotIngestionRuntimeDependencies(nil)
		if view.cache != nil || view.postgres != nil || view.irisClient != nil || view.members != nil || view.scheduler != nil {
			t.Fatal("nil deps must yield zero-value ingestion dependency view")
		}
	})

	t.Run("maps required fields only", func(t *testing.T) {
		cacheSvc := &cache.Service{}
		postgresSvc := &database.PostgresService{}
		irisClient := &stubIrisClient{}
		membersData := &stubMemberDataProvider{}
		scheduler := &stubYouTubeScheduler{}

		deps := &bot.Dependencies{
			Cache:       cacheSvc,
			Postgres:    postgresSvc,
			Client:      irisClient,
			MembersData: membersData,
			Scheduler:   scheduler,
		}

		view := buildBotIngestionRuntimeDependencies(deps)
		if view.cache != cacheSvc {
			t.Fatal("cache mapping mismatch")
		}
		if view.postgres != postgresSvc {
			t.Fatal("postgres mapping mismatch")
		}
		if view.irisClient != irisClient {
			t.Fatal("iris client mapping mismatch")
		}
		if view.members != membersData {
			t.Fatal("member data mapping mismatch")
		}
		if view.scheduler != scheduler {
			t.Fatal("scheduler mapping mismatch")
		}
	})
}

func TestBuildBotConfigSubscriberDependencies(t *testing.T) {
	t.Run("nil dependencies", func(t *testing.T) {
		view := buildBotConfigSubscriberDependencies(nil)
		if view.cache != nil || view.settings != nil {
			t.Fatal("nil deps must yield zero-value config subscriber view")
		}
	})

	t.Run("maps settings and cache", func(t *testing.T) {
		cacheSvc := &cache.Service{}
		settingsSvc := &stubSettingsReadWriter{}
		deps := &bot.Dependencies{
			Cache:    cacheSvc,
			Settings: settingsSvc,
		}

		view := buildBotConfigSubscriberDependencies(deps)
		if view.cache != cacheSvc {
			t.Fatal("cache mapping mismatch")
		}
		if view.settings != settingsSvc {
			t.Fatal("settings mapping mismatch")
		}
	})
}

var _ member.DataProvider = (*stubMemberDataProvider)(nil)
var _ youtube.Scheduler = (*stubYouTubeScheduler)(nil)
var _ settings.ReadWriter = (*stubSettingsReadWriter)(nil)
