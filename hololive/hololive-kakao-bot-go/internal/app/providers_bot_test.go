package app

import (
	"io"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/majorevent"
	"github.com/kapu/hololive-shared/pkg/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/membernews"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
)

func TestProvideBotDependencies_WiringSmoke(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messageAdapter := &adapter.MessageAdapter{}
	formatter := &adapter.ResponseFormatter{}
	msgStack := &providers.MessageStack{
		Adapter:   messageAdapter,
		Formatter: formatter,
	}

	cacheSvc := &cache.Service{}
	postgres := &database.PostgresService{}
	memberRepo := &member.Repository{}
	memberCache := &member.Cache{}
	holodexSvc := &holodex.Service{}
	chzzkClient := &chzzk.Client{}
	twitchClient := &twitch.Client{}
	profiles := &member.ProfileService{}
	memberMatcher := &matcher.MemberMatcher{}
	ytService := &youtube.Service{}
	ytScheduler := &youtube.Scheduler{}
	ytStatsRepo := &youtube.StatsRepository{}
	ytStack := &providers.YouTubeStack{
		Service:   ytService,
		Scheduler: ytScheduler,
		StatsRepo: ytStatsRepo,
	}
	activityLogger := &activity.Logger{}
	settingsSvc := &settings.Service{}
	aclSvc := &acl.Service{}
	majorEventRepo := &majorevent.Repository{}
	memberNewsSvc := &membernews.Service{}
	workerPool := &workerpool.Pool{}

	deps := ProvideBotDependencies(
		"bot-self",
		"https://iris.internal",
		config.NotificationConfig{},
		logger,
		nil, // irisClient
		msgStack,
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
