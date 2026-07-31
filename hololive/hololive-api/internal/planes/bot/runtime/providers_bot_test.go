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

	configsettings "github.com/kapu/hololive-shared/pkg/config/settings"

	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"

	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/activity"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
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

type stubMajorEventRepository struct{}

func (s *stubMajorEventRepository) IsSubscribed(ctx context.Context, roomID string) (bool, error) {
	return false, nil
}
func (s *stubMajorEventRepository) Subscribe(ctx context.Context, roomID, roomName string) error {
	return nil
}
func (s *stubMajorEventRepository) Unsubscribe(ctx context.Context, roomID string) error { return nil }

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
	messageAdapter := &messaging.MessageAdapter{}
	formatter := &messageformatter.ResponseFormatter{}

	cacheService := &cache.Service{}
	postgres := &database.PostgresService{}
	memberRepository := &member.Repository{}
	memberCache := &member.Cache{}
	holodexService := &holodexprovider.Service{}
	chzzkClient := &chzzk.Client{}
	twitchClient := &twitch.Client{}
	profiles := &member.ProfileService{}
	memberMatcher := &matcher.Matcher{}

	var ytService youtube.Service = &mockYouTubeService{}
	ytStack := &providers.YouTubeStack{Service: ytService}
	activityLogger := &activity.Logger{}
	settingsService := &settings.Service{}
	aclService := &acl.Service{}
	majorEventRepository := &stubMajorEventRepository{}
	memberNewsService := &stubMemberNewsService{}
	commandBuilder := orchcmd.CommandBuilder(func(_ *handlercore.Dependencies) handlercore.Command { return nil })

	deps := appbootstrap.ProvideBotDependencies(&appbootstrap.BotDependencyModules{
		Core: appbootstrap.BotCoreModule{
			BotSelfUser:  "bot-self",
			IrisBaseURL:  "https://iris.internal",
			Notification: configsettings.NotificationConfig{},
			Logger:       logger,
		},
		Messaging: appbootstrap.BotMessagingModule{
			Client:         nil,
			MessageAdapter: messageAdapter,
			Formatter:      formatter,
		},
		Data: appbootstrap.BotDataModule{
			Cache:            cacheService,
			Postgres:         postgres,
			MemberRepository: memberRepository,
			MemberCache:      memberCache,
			Profiles:         profiles,
			MembersData:      nil,
		},
		Stream: appbootstrap.BotStreamModule{
			Holodex:      holodexService,
			ChzzkClient:  chzzkClient,
			TwitchClient: twitchClient,
			Alarm:        nil,
			MemberMatch:  memberMatcher,
			YTStack:      ytStack,
		},
		Support: appbootstrap.BotSupportModule{
			ActivityLogger: activityLogger,
			Settings:       settingsService,
			ACL:            aclService,
		},
		Feature: appbootstrap.BotFeatureModule{
			MajorEventRepository: majorEventRepository,
			MemberNews:           memberNewsService,
			CommandBuilders:      []orchcmd.CommandBuilder{commandBuilder},
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
	if deps.Cache != cacheService || deps.Postgres != postgres {
		t.Fatal("infra wiring mismatch")
	}
	if deps.MemberRepository != memberRepository || deps.MemberCache != memberCache {
		t.Fatal("member wiring mismatch")
	}
	if deps.Holodex != holodexService || deps.Chzzk != chzzkClient || deps.Twitch != twitchClient {
		t.Fatal("stream client wiring mismatch")
	}
	if deps.Service != ytService {
		t.Fatal("youtube stack wiring mismatch")
	}
	if deps.Activity != activityLogger || deps.Settings != settingsService || deps.ACL != aclService {
		t.Fatal("runtime support wiring mismatch")
	}
	if deps.MajorEventRepository != majorEventRepository || deps.MemberNews != memberNewsService {
		t.Fatal("event/news wiring mismatch")
	}
	if len(deps.CommandBuilders) != 1 || deps.CommandBuilders[0] == nil {
		t.Fatal("command builder wiring mismatch")
	}
}

func TestProvideBotDependencies_NilYouTubeStackIsSafe(t *testing.T) {
	t.Parallel()

	deps := appbootstrap.ProvideBotDependencies(&appbootstrap.BotDependencyModules{
		Stream: appbootstrap.BotStreamModule{YTStack: nil},
	})
	if deps == nil {
		t.Fatal("ProvideBotDependencies() returned nil")
	}
	if deps.Service != nil {
		t.Fatal("Service must be nil when ytStack is nil")
	}
}
