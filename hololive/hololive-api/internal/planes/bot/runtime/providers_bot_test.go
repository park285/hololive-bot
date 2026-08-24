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

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	configsettings "github.com/kapu/hololive-shared/pkg/config/settings"
	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/activity"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/database"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
)

type mockYouTubeService struct{}

func (s *mockYouTubeService) SetScraperProxyEnabled(bool) bool { return false }
func (s *mockYouTubeService) ScraperProxyEnabled() bool        { return false }
func (s *mockYouTubeService) GetChannelStatistics(context.Context, []string) (map[string]*youtube.ChannelStats, error) {
	return map[string]*youtube.ChannelStats{}, nil
}

func (s *mockYouTubeService) GetRecentVideos(context.Context, string, int64) ([]string, error) {
	return nil, nil
}

type stubMajorEventRepository struct{}

func (s *stubMajorEventRepository) IsSubscribed(context.Context, string) (bool, error) {
	return false, nil
}

func (s *stubMajorEventRepository) Subscribe(context.Context, string, string) error {
	return nil
}

func (s *stubMajorEventRepository) Unsubscribe(context.Context, string) error { return nil }

type stubMemberNewsService struct{}

func (s *stubMemberNewsService) GenerateRoomDigest(context.Context, string, membernewscontracts.Period) (*membernewscontracts.Digest, error) {
	return &membernewscontracts.Digest{}, nil
}

func (s *stubMemberNewsService) SubscribeRoom(context.Context, string, string) error {
	return nil
}

func (s *stubMemberNewsService) UnsubscribeRoom(context.Context, string) error { return nil }

func (s *stubMemberNewsService) IsRoomSubscribed(context.Context, string) (bool, error) {
	return false, nil
}

type botWiringFixture struct {
	logger           *slog.Logger
	messageAdapter   *messaging.MessageAdapter
	formatter        *messageformatter.ResponseFormatter
	cache            *cache.Service
	postgres         *database.PostgresService
	memberRepository *member.Repository
	memberCache      *member.Cache
	profiles         *member.ProfileService
	holodex          *holodexprovider.Service
	chzzk            *chzzk.Client
	twitch           *twitch.Client
	memberMatch      *matcher.Matcher
	youtube          youtube.Service
	activity         *activity.Logger
	settings         *settings.Service
	acl              *acl.Service
	majorEvents      *stubMajorEventRepository
	memberNews       *stubMemberNewsService
	commandBuilder   orchcmd.CommandBuilder
}

type botWiringCheck struct {
	name string
	ok   bool
}

func newBotWiringFixture() *botWiringFixture {
	return &botWiringFixture{
		logger:           slog.New(slog.DiscardHandler),
		messageAdapter:   &messaging.MessageAdapter{},
		formatter:        &messageformatter.ResponseFormatter{},
		cache:            &cache.Service{},
		postgres:         &database.PostgresService{},
		memberRepository: &member.Repository{},
		memberCache:      &member.Cache{},
		profiles:         &member.ProfileService{},
		holodex:          &holodexprovider.Service{},
		chzzk:            &chzzk.Client{},
		twitch:           &twitch.Client{},
		memberMatch:      &matcher.Matcher{},
		youtube:          &mockYouTubeService{},
		activity:         &activity.Logger{},
		settings:         &settings.Service{},
		acl:              &acl.Service{},
		majorEvents:      &stubMajorEventRepository{},
		memberNews:       &stubMemberNewsService{},
		commandBuilder:   func(_ *handlercore.Dependencies) handlercore.Command { return nil },
	}
}

func (f *botWiringFixture) modules() *appbootstrap.BotDependencyModules {
	return &appbootstrap.BotDependencyModules{
		Core: appbootstrap.BotCoreModule{
			BotSelfUser:  "bot-self",
			IrisBaseURL:  "https://iris.internal",
			Notification: configsettings.NotificationConfig{},
			Logger:       f.logger,
		},
		Messaging: appbootstrap.BotMessagingModule{
			Client:         nil,
			MessageAdapter: f.messageAdapter,
			Formatter:      f.formatter,
		},
		Data: appbootstrap.BotDataModule{
			Cache:            f.cache,
			Postgres:         f.postgres,
			MemberRepository: f.memberRepository,
			MemberCache:      f.memberCache,
			Profiles:         f.profiles,
			MembersData:      nil,
		},
		Stream: appbootstrap.BotStreamModule{
			Holodex:      f.holodex,
			ChzzkClient:  f.chzzk,
			TwitchClient: f.twitch,
			Alarm:        nil,
			MemberMatch:  f.memberMatch,
			YTStack:      &providers.YouTubeStack{Service: f.youtube},
		},
		Support: appbootstrap.BotSupportModule{
			ActivityLogger: f.activity,
			Settings:       f.settings,
			ACL:            f.acl,
		},
		Feature: appbootstrap.BotFeatureModule{
			MajorEventRepository: f.majorEvents,
			MemberNews:           f.memberNews,
			CommandBuilders:      []orchcmd.CommandBuilder{f.commandBuilder},
		},
	}
}

func (f *botWiringFixture) checks(deps *orchestration.Dependencies) []botWiringCheck {
	return []botWiringCheck{
		{name: "MessageAdapter", ok: deps.MessageAdapter == f.messageAdapter},
		{name: "Formatter", ok: deps.Formatter == f.formatter},
		{name: "Cache", ok: deps.Cache == f.cache},
		{name: "Postgres", ok: deps.Postgres == f.postgres},
		{name: "MemberRepository", ok: deps.MemberRepository == f.memberRepository},
		{name: "MemberCache", ok: deps.MemberCache == f.memberCache},
		{name: "Holodex", ok: deps.Holodex == f.holodex},
		{name: "Chzzk", ok: deps.Chzzk == f.chzzk},
		{name: "Twitch", ok: deps.Twitch == f.twitch},
		{name: "YouTubeService", ok: deps.Service == f.youtube},
		{name: "Activity", ok: deps.Activity == f.activity},
		{name: "Settings", ok: deps.Settings == f.settings},
		{name: "ACL", ok: deps.ACL == f.acl},
		{name: "MajorEventRepository", ok: deps.MajorEventRepository == f.majorEvents},
		{name: "MemberNews", ok: deps.MemberNews == f.memberNews},
		{name: "CommandBuilders", ok: len(deps.CommandBuilders) == 1 && deps.CommandBuilders[0] != nil},
	}
}

func TestProvideBotDependencies_WiringSmoke(t *testing.T) {
	t.Parallel()

	fixture := newBotWiringFixture()

	deps := appbootstrap.ProvideBotDependencies(fixture.modules())
	if deps == nil {
		t.Fatal("ProvideBotDependencies() returned nil")
	}

	if deps.BotSelfUser != "bot-self" {
		t.Fatalf("BotSelfUser = %q, want %q", deps.BotSelfUser, "bot-self")
	}

	for _, check := range fixture.checks(deps) {
		if !check.ok {
			t.Fatalf("%s wiring mismatch", check.name)
		}
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
