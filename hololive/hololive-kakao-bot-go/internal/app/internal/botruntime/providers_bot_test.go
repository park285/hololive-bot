// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package botruntime

import (
	"context"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/activity"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
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
	logger := slog.New(slog.DiscardHandler)
	messageAdapter := &adapter.MessageAdapter{}
	formatter := &adapter.ResponseFormatter{}

	cache := &cache.Service{}
	postgres := &database.PostgresService{}
	memberRepo := &member.Repository{}
	memberCache := &member.Cache{}
	holodexSvc := &holodex.Service{}
	chzzkClient := &chzzk.Client{}
	twitchClient := &twitch.Client{}
	profiles := &member.ProfileService{}
	memberMatcher := &matcher.MemberMatcher{}

	var ytService youtube.Service = &mockYouTubeService{}
	ytStatsRepo := &stats.StatsRepository{}
	ytStack := &providers.YouTubeStack{Service: ytService, StatsRepo: ytStatsRepo}
	activityLogger := &activity.Logger{}
	settingsSvc := &settings.Service{}
	aclSvc := &acl.Service{}
	majorEventRepo := &stubMajorEventRepo{}
	memberNewsSvc := &stubMemberNewsService{}
	workerPool := &workerpool.Pool{}
	commandBuilder := bot.CommandBuilder(func(_ *command.Dependencies) command.Command { return nil })

	deps := appbootstrap.ProvideBotDependencies(appbootstrap.BotDependencyModules{
		Core: appbootstrap.BotCoreModule{
			BotSelfUser:  "bot-self",
			IrisBaseURL:  "https://iris.internal",
			Notification: config.NotificationConfig{},
			Logger:       logger,
		},
		Messaging: appbootstrap.BotMessagingModule{
			Client:         nil,
			MessageAdapter: messageAdapter,
			Formatter:      formatter,
		},
		Data: appbootstrap.BotDataModule{
			CacheSvc:    cache,
			Postgres:    postgres,
			MemberRepo:  memberRepo,
			MemberCache: memberCache,
			Profiles:    profiles,
			MembersData: nil,
		},
		Stream: appbootstrap.BotStreamModule{
			HolodexSvc:   holodexSvc,
			ChzzkClient:  chzzkClient,
			TwitchClient: twitchClient,
			AlarmSvc:     nil,
			MemberMatch:  memberMatcher,
			YTStack:      ytStack,
		},
		Support: appbootstrap.BotSupportModule{
			ActivityLogger: activityLogger,
			SettingsSvc:    settingsSvc,
			ACLSvc:         aclSvc,
			WorkerPool:     workerPool,
		},
		Feature: appbootstrap.BotFeatureModule{
			MajorEventRepo:  majorEventRepo,
			MemberNewsSvc:   memberNewsSvc,
			CommandBuilders: []bot.CommandBuilder{commandBuilder},
		},
	})

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
	if deps.Cache != cache || deps.Postgres != postgres {
		t.Fatal("infra wiring mismatch")
	}
	if deps.MemberRepo != memberRepo || deps.MemberCache != memberCache {
		t.Fatal("member wiring mismatch")
	}
	if deps.Holodex != holodexSvc || deps.Chzzk != chzzkClient || deps.Twitch != twitchClient {
		t.Fatal("stream client wiring mismatch")
	}
	if deps.Service != ytService || deps.YouTubeStatsRepo != ytStatsRepo {
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
	if len(deps.CommandBuilders) != 1 || deps.CommandBuilders[0] == nil {
		t.Fatal("command builder wiring mismatch")
	}
}

func TestProvideBotDependencies_NilYouTubeStackIsSafe(t *testing.T) {
	t.Parallel()

	deps := appbootstrap.ProvideBotDependencies(appbootstrap.BotDependencyModules{
		Stream: appbootstrap.BotStreamModule{YTStack: nil},
	})
	if deps == nil {
		t.Fatal("ProvideBotDependencies() returned nil")
	}
	if deps.Service != nil {
		t.Fatal("Service must be nil when ytStack is nil")
	}
	if deps.YouTubeStatsRepo != nil {
		t.Fatal("YouTubeStatsRepo must be nil when ytStack is nil")
	}
}
