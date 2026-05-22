package bootstrap

import (
	"context"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	membermocks "github.com/kapu/hololive-shared/pkg/service/member/mocks"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	settingsmocks "github.com/kapu/hololive-shared/pkg/service/settings/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
)

func TestBuildBotDependencyModulesAndProvideBotDependenciesWireRuntimeObjects(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	cacheClient := cachemocks.NewLenientClient()
	postgres := &databasemocks.Client{}
	memberData := &membermocks.DataProvider{}
	alarmCRUD := &stubAlarmCRUD{targetMinutes: []int{15, 3, 1}}
	irisClient := &stubBotIrisClient{}
	messageAdapter := adapter.NewMessageAdapter("!", "@bot")
	formatter := adapter.NewResponseFormatter("!", nil)
	activityLogger := ProvideActivityLogger(logger)
	settingsSvc := &settingsmocks.ReadWriter{
		GetFunc: func() settings.Settings {
			return settings.Settings{AlarmAdvanceMinutes: 15, TargetMinutes: []int{15, 3, 1}}
		},
		UpdateFunc: func(settings.Settings) error {
			return nil
		},
	}
	youTubeService := &stubYouTubeService{}
	statsRepo := &ytstats.StatsRepository{}
	commandBuilders := []bot.CommandBuilder{stubCommandBuilderOne, stubCommandBuilderTwo}

	cfg := &config.Config{
		Bot: config.BotConfig{
			SelfUser:      "bot-self",
			Prefix:        "!",
			MentionPrefix: "@bot",
		},
		Iris: config.IrisConfig{
			BaseURL: "http://iris.local",
		},
		Notification: config.NotificationConfig{
			AdvanceMinutes: []int{15, 3, 1},
		},
	}

	modules := BuildBotDependencyModules(
		cfg,
		(&sharedInfraForBootstrapTest{cacheClient: cacheClient, postgres: postgres}).module(),
		&AlarmModeComponents{
			AlarmCRUD:        alarmCRUD,
			MemberDataSource: memberData,
		},
		nil,
		messageAdapter,
		formatter,
		irisClient,
		nil,
		nil,
		&sharedproviders.YouTubeStack{Service: youTubeService, StatsRepo: statsRepo},
		activityLogger,
		settingsSvc,
		nil,
		nil,
		nil,
		commandBuilders,
		nil,
		logger,
	)
	commandBuilders[0] = stubCommandBuilderThree

	if modules.Core.BotSelfUser != "bot-self" {
		t.Fatalf("Core.BotSelfUser = %q, want bot-self", modules.Core.BotSelfUser)
	}
	if modules.Core.IrisBaseURL != "http://iris.local" {
		t.Fatalf("Core.IrisBaseURL = %q, want http://iris.local", modules.Core.IrisBaseURL)
	}
	if !slices.Equal(modules.Core.Notification.AdvanceMinutes, []int{15, 3, 1}) {
		t.Fatalf("Core.Notification.AdvanceMinutes = %v, want [15 3 1]", modules.Core.Notification.AdvanceMinutes)
	}
	if modules.Data.Cache != cacheClient {
		t.Fatal("Data.Cache did not preserve the injected cache client")
	}
	if modules.Data.Postgres != postgres {
		t.Fatal("Data.Postgres did not preserve the injected postgres client")
	}
	if modules.Data.MembersData != memberData {
		t.Fatal("Data.MembersData did not preserve the alarm member data provider")
	}
	if modules.Stream.Alarm != alarmCRUD {
		t.Fatal("Stream.Alarm did not preserve the alarm CRUD provider")
	}
	if modules.Messaging.Client != irisClient {
		t.Fatal("Messaging.Client did not preserve the Iris client")
	}
	if modules.Messaging.MessageAdapter != messageAdapter {
		t.Fatal("Messaging.MessageAdapter did not preserve the message adapter")
	}
	if modules.Messaging.Formatter != formatter {
		t.Fatal("Messaging.Formatter did not preserve the formatter")
	}
	assertCommandBuilderPointers(t, modules.Feature.CommandBuilders, []bot.CommandBuilder{stubCommandBuilderOne, stubCommandBuilderTwo})

	deps := ProvideBotDependencies(modules)
	if deps.Cache != cacheClient {
		t.Fatal("Dependencies.Cache did not preserve the module cache client")
	}
	if deps.Postgres != postgres {
		t.Fatal("Dependencies.Postgres did not preserve the module postgres client")
	}
	if deps.MembersData != memberData {
		t.Fatal("Dependencies.MembersData did not preserve the module member data provider")
	}
	if deps.Alarm != alarmCRUD {
		t.Fatal("Dependencies.Alarm did not preserve the module alarm CRUD provider")
	}
	if deps.Service != youtube.Service(youTubeService) {
		t.Fatal("Dependencies.Service did not preserve the YouTube service from the stack")
	}
	if deps.YouTubeStatsRepo != statsRepo {
		t.Fatal("Dependencies.YouTubeStatsRepo did not preserve the YouTube stats repository from the stack")
	}
	if deps.Activity != activityLogger {
		t.Fatal("Dependencies.Activity did not preserve the activity logger")
	}
	if deps.Settings != settingsSvc {
		t.Fatal("Dependencies.Settings did not preserve the settings service")
	}
	assertCommandBuilderPointers(t, deps.CommandBuilders, []bot.CommandBuilder{stubCommandBuilderOne, stubCommandBuilderTwo})
}

func TestProvideBotDependenciesAcceptsDisabledYouTubeStack(t *testing.T) {
	t.Parallel()

	deps := ProvideBotDependencies(BotDependencyModules{
		Stream: BotStreamModule{YTStack: nil},
	})
	if deps.Service != nil {
		t.Fatalf("Service = %T, want nil for disabled YouTube stack", deps.Service)
	}
	if deps.YouTubeStatsRepo != nil {
		t.Fatalf("YouTubeStatsRepo = %T, want nil for disabled YouTube stack", deps.YouTubeStatsRepo)
	}
}

func TestProvideAlarmWorkerPoolUsesDispatchCapacity(t *testing.T) {
	t.Parallel()

	pool, err := ProvideAlarmWorkerPool()
	if err != nil {
		t.Fatalf("ProvideAlarmWorkerPool() error = %v", err)
	}
	t.Cleanup(pool.Shutdown)

	if pool.Cap() != 10 {
		t.Fatalf("Cap() = %d, want 10", pool.Cap())
	}
}

func TestPersistedTargetMinutesKeepsConfiguredTargetsBeforeRuntimeFallback(t *testing.T) {
	t.Parallel()

	if got := PersistedTargetMinutes(15, []int{3, 15, 3, 0}); !slices.Equal(got, []int{15, 3}) {
		t.Fatalf("PersistedTargetMinutes configured = %v, want [15 3]", got)
	}
	if got := PersistedTargetMinutes(15, nil); !slices.Equal(got, []int{15, 3, 1}) {
		t.Fatalf("PersistedTargetMinutes fallback = %v, want [15 3 1]", got)
	}
}

func TestProvideACLServiceWrapsInitializationError(t *testing.T) {
	t.Parallel()

	_, err := ProvideACLService(context.Background(), true, "whitelist", []string{"room-a"}, nil, cachemocks.NewLenientClient(), slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("ProvideACLService() error = nil, want initialization error")
	}
	if !strings.Contains(err.Error(), "failed to create ACL service") || !strings.Contains(err.Error(), "postgres service is nil") {
		t.Fatalf("ProvideACLService() error = %q, want wrapped postgres initialization failure", err)
	}
}

type sharedInfraForBootstrapTest struct {
	cacheClient *cachemocks.Client
	postgres    *databasemocks.Client
}

func (s *sharedInfraForBootstrapTest) module() *sharedmodules.InfraModule {
	return &sharedmodules.InfraModule{
		Cache:    s.cacheClient,
		Postgres: s.postgres,
	}
}

type stubBotIrisClient struct{}

func (s *stubBotIrisClient) SendMessage(context.Context, string, string, ...iris.SendOption) error {
	return nil
}

func (s *stubBotIrisClient) SendMessageAccepted(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, nil
}

func (s *stubBotIrisClient) SendImage(context.Context, string, []byte, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, nil
}

func (s *stubBotIrisClient) SendMultipleImages(context.Context, string, [][]byte, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, nil
}

func (s *stubBotIrisClient) SendMarkdown(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, nil
}

func (s *stubBotIrisClient) GetReplyStatus(context.Context, string) (*iris.ReplyStatusSnapshot, error) {
	return nil, nil
}

func (s *stubBotIrisClient) Ping(context.Context) bool {
	return true
}

func (s *stubBotIrisClient) GetConfig(context.Context) (*iris.ConfigResponse, error) {
	return nil, nil
}

type stubYouTubeService struct{}

func (s *stubYouTubeService) SetScraperProxyEnabled(bool) bool {
	return true
}

func (s *stubYouTubeService) ScraperProxyEnabled() bool {
	return true
}

func (s *stubYouTubeService) GetChannelStatistics(context.Context, []string) (map[string]*youtube.ChannelStats, error) {
	return nil, nil
}

func (s *stubYouTubeService) GetRecentVideos(context.Context, string, int64) ([]string, error) {
	return nil, nil
}

type stubAlarmCRUD struct {
	targetMinutes []int
}

func (s *stubAlarmCRUD) AddAlarm(context.Context, domain.AddAlarmRequest) (bool, error) {
	return false, nil
}

func (s *stubAlarmCRUD) RemoveAlarm(context.Context, string, string, domain.AlarmTypes) (bool, error) {
	return false, nil
}

func (s *stubAlarmCRUD) GetRoomAlarms(context.Context, string) ([]string, error) {
	return nil, nil
}

func (s *stubAlarmCRUD) GetRoomAlarmsWithTypes(context.Context, string) ([]*domain.Alarm, error) {
	return nil, nil
}

func (s *stubAlarmCRUD) ListRoomAlarmsView(context.Context, string) ([]domain.AlarmListView, error) {
	return nil, nil
}

func (s *stubAlarmCRUD) ClearRoomAlarms(context.Context, string) (int, error) {
	return 0, nil
}

func (s *stubAlarmCRUD) GetNextStreamInfo(context.Context, string) (*domain.NextStreamInfo, error) {
	return nil, nil
}

func (s *stubAlarmCRUD) UpdateAlarmAdvanceMinutes(_ context.Context, minutes int) []int {
	s.targetMinutes = []int{minutes}
	return append([]int(nil), s.targetMinutes...)
}

func (s *stubAlarmCRUD) GetTargetMinutes() []int {
	return append([]int(nil), s.targetMinutes...)
}

func (s *stubAlarmCRUD) SetRoomName(context.Context, string, string) error {
	return nil
}

func (s *stubAlarmCRUD) SetUserName(context.Context, string, string) error {
	return nil
}

func (s *stubAlarmCRUD) GetAllAlarmKeys(context.Context) ([]*domain.AlarmEntry, error) {
	return nil, nil
}

func (s *stubAlarmCRUD) WarmCacheFromDB(context.Context) error {
	return nil
}

func stubCommandBuilderOne(*command.Dependencies) command.Command {
	return nil
}

func stubCommandBuilderTwo(*command.Dependencies) command.Command {
	return nil
}

func stubCommandBuilderThree(*command.Dependencies) command.Command {
	return nil
}

func assertCommandBuilderPointers(t *testing.T, got, want []bot.CommandBuilder) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("CommandBuilders len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if reflect.ValueOf(got[i]).Pointer() != reflect.ValueOf(want[i]).Pointer() {
			t.Fatalf("CommandBuilders[%d] pointer mismatch", i)
		}
	}
}
