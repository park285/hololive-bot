package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
	"github.com/kapu/hololive-shared/pkg/config"
	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
)

type mockYouTubeService struct{}

func (s *mockYouTubeService) SetScraperProxyEnabled(enabled bool) bool { return false }
func (s *mockYouTubeService) ScraperProxyEnabled() bool                { return false }
func (s *mockYouTubeService) GetChannelStatistics(ctx context.Context, channelIDs []string) (map[string]*youtube.ChannelStats, error) {
	return nil, nil
}
func (s *mockYouTubeService) GetRecentVideos(ctx context.Context, channelID string, maxResults int64) ([]string, error) {
	return nil, nil
}

type mockYouTubeScheduler struct{}

func (s *mockYouTubeScheduler) Start(ctx context.Context) {}
func (s *mockYouTubeScheduler) Stop()                     {}

type stubMajorEventRepo struct{}

func (s *stubMajorEventRepo) IsSubscribed(ctx context.Context, roomID string) (bool, error) {
	return false, nil
}
func (s *stubMajorEventRepo) Subscribe(ctx context.Context, roomID, roomName string) error {
	return nil
}
func (s *stubMajorEventRepo) Unsubscribe(ctx context.Context, roomID string) error { return nil }

type stubMemberNewsService struct{}

func (s *stubMemberNewsService) GenerateRoomDigest(ctx context.Context, roomID string, period membernewscontracts.Period) (*membernewscontracts.Digest, error) {
	return nil, nil
}
func (s *stubMemberNewsService) SubscribeRoom(ctx context.Context, roomID, roomName string) error {
	return nil
}
func (s *stubMemberNewsService) UnsubscribeRoom(ctx context.Context, roomID string) error { return nil }
func (s *stubMemberNewsService) IsRoomSubscribed(ctx context.Context, roomID string) (bool, error) {
	return false, nil
}

func TestProvideBotDependencies_WiringSmoke(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messageAdapter := &adapter.MessageAdapter{}
	formatter := &adapter.ResponseFormatter{}

	cacheSvc := &cache.Service{}
	postgres := &database.PostgresService{}
	memberRepo := &member.Repository{}
	memberCache := &member.Cache{}
	holodexSvc := &holodex.Service{}
	chzzkClient := &chzzk.Client{}
	twitchClient := &twitch.Client{}
	profiles := &member.ProfileService{}
	memberMatcher := &matcher.MemberMatcher{}
	var ytService youtube.Service = &mockYouTubeService{}
	var ytScheduler youtube.Scheduler = &mockYouTubeScheduler{}
	ytStatsRepo := &youtube.StatsRepository{}
	ytStack := &providers.YouTubeStack{
		Service:   ytService,
		Scheduler: ytScheduler,
		StatsRepo: ytStatsRepo,
	}
	activityLogger := &activity.Logger{}
	settingsSvc := &settings.Service{}
	aclSvc := &acl.Service{}
	majorEventRepo := &stubMajorEventRepo{}
	memberNewsSvc := &stubMemberNewsService{}
	workerPool := &workerpool.Pool{}

	deps := ProvideBotDependencies(
		"bot-self",
		"https://iris.internal",
		config.NotificationConfig{},
		logger,
		nil, // irisClient
		messageAdapter,
		formatter,
		cacheSvc,
		postgres,
		memberRepo,
		memberCache,
		holodexSvc,
		chzzkClient,
		twitchClient,
		profiles,
		nil, // alarmSvc
		memberMatcher,
		nil, // membersData
		ytStack,
		activityLogger,
		settingsSvc,
		aclSvc,
		majorEventRepo,
		memberNewsSvc,
		workerPool,
	)

	if deps == nil {
		t.Fatal("ProvideBotDependencies() returned nil")
	}
	if deps.BotSelfUser != "bot-self" {
		t.Fatalf("BotSelfUser = %q, want %q", deps.BotSelfUser, "bot-self")
	}
	if deps.MessageAdapter != messageAdapter {
		t.Fatal("MessageAdapter wiring mismatch")
	}
	if deps.Formatter != formatter {
		t.Fatal("Formatter wiring mismatch")
	}
	if deps.Cache != cacheSvc || deps.Postgres != postgres {
		t.Fatal("infra wiring mismatch")
	}
	if deps.MemberRepo != memberRepo || deps.MemberCache != memberCache {
		t.Fatal("member wiring mismatch")
	}
	if deps.Holodex != holodexSvc || deps.Chzzk != chzzkClient || deps.Twitch != twitchClient {
		t.Fatal("stream client wiring mismatch")
	}
	if deps.Service != ytService || deps.Scheduler != ytScheduler || deps.YouTubeStatsRepo != ytStatsRepo {
		t.Fatal("youtube stack wiring mismatch")
	}
	if deps.Activity != activityLogger || deps.Settings != settingsSvc || deps.ACL != aclSvc {
		t.Fatal("runtime support wiring mismatch")
	}
	if deps.MajorEventRepo != majorEventRepo || deps.MemberNews != memberNewsSvc {
		t.Fatal("event/news wiring mismatch")
	}
	if deps.WorkerPool != workerPool {
		t.Fatal("worker pool wiring mismatch")
	}
}
