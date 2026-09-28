package bootstrap

import (
	"context"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v3/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/service/activity"
	membermocks "github.com/kapu/hololive-api/internal/service/member/mocks"
	settingsmocks "github.com/kapu/hololive-api/internal/service/settings/mocks"
	configsettings "github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/settings"
)

// ACL seed(KAKAO_ROOMS)는 chatID만 받으므로 fixture도 signed i64 문자열이다.
const testRoomA = "1001"

func TestBuildBotDependencyModulesAndProvideBotDependenciesWireRuntimeObjects(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	cacheClient := cachemocks.NewLenientClient()
	postgres := &databasemocks.Client{}
	memberData := &membermocks.DataProvider{}
	alarmCRUD := &stubAlarmCRUD{targetMinutes: []int{15, 3, 1}}
	irisClient := &stubBotIrisClient{}
	messageAdapter := messaging.NewMessageAdapter("!", "@bot")
	formatter := messageformatter.NewResponseFormatter("!", nil)
	activityLogger := ProvideActivityLogger(logger)
	settingsService := &settingsmocks.ReadWriter{
		GetFunc: func() settings.Settings {
			return settings.Settings{AlarmAdvanceMinutes: 15, TargetMinutes: []int{15, 3, 1}}
		},
		UpdateFunc: func(settings.Settings) error {
			return nil
		},
	}
	commandBuilders := []orchcmd.CommandBuilder{stubCommandBuilderOne, stubCommandBuilderTwo}

	appConfig := &configsettings.Config{
		Bot: configsettings.BotConfig{
			SelfUser:              "bot-self",
			Prefix:                "!",
			MentionPrefix:         "@bot",
			CalendarImageCacheDir: "data/test-calendar-cache",
			CalendarEntryCacheTTL: time.Hour,
			MarkdownReplies:       true,
		},
		Iris: configsettings.IrisConfig{
			BaseURL: "http://iris.local",
		},
		Notification: configsettings.NotificationConfig{
			AdvanceMinutes: []int{15, 3, 1},
		},
	}

	modules := BuildBotDependencyModules(
		appConfig,
		(&sharedInfraForBootstrapTest{cacheClient: cacheClient, postgres: postgres}).module(),
		&ScraperHolodexFoundation{},
		&AlarmYouTubeStackComponents{
			AlarmMode: &AlarmModeComponents{
				AlarmCRUD:        alarmCRUD,
				MemberDataSource: memberData,
			},
			ActivityLogger:  activityLogger,
			SettingsService: settingsService,
		},
		&CoreIntegrationServices{CommandBuilders: commandBuilders},
		messageAdapter,
		formatter,
		nil,
		irisClient,
		logger,
	)

	commandBuilders[0] = stubCommandBuilderThree

	assertBotDependencyModulesWireRuntimeObjects(t, &modules, cacheClient, postgres, memberData, alarmCRUD, irisClient, messageAdapter, formatter)

	deps := ProvideBotDependencies(&modules)
	assertBotDependenciesWireRuntimeObjects(t, deps, cacheClient, postgres, memberData, alarmCRUD, activityLogger, settingsService)
}

func assertBotDependencyModulesWireRuntimeObjects(
	t *testing.T,
	modules *BotDependencyModules,
	cacheClient *cachemocks.Client,
	postgres *databasemocks.Client,
	memberData *membermocks.DataProvider,
	alarmCRUD *stubAlarmCRUD,
	irisClient *stubBotIrisClient,
	messageAdapter *messaging.MessageAdapter,
	formatter *messageformatter.ResponseFormatter,
) {
	t.Helper()

	if modules.Core.BotSelfUser != "bot-self" {
		t.Fatalf("Core.BotSelfUser = %q, want bot-self", modules.Core.BotSelfUser)
	}

	if modules.Core.IrisBaseURL != "http://iris.local" {
		t.Fatalf("Core.IrisBaseURL = %q, want http://iris.local", modules.Core.IrisBaseURL)
	}

	if modules.Core.CalendarImageCacheDir != "data/test-calendar-cache" {
		t.Fatalf("Core.CalendarImageCacheDir = %q, want data/test-calendar-cache", modules.Core.CalendarImageCacheDir)
	}

	if modules.Core.CalendarEntryCacheTTL != time.Hour {
		t.Fatalf("Core.CalendarEntryCacheTTL = %s, want 1h", modules.Core.CalendarEntryCacheTTL)
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

	if !modules.Messaging.MarkdownReplies {
		t.Fatal("Messaging.MarkdownReplies did not preserve the bot markdown replies flag")
	}

	assertCommandBuilderPointers(t, modules.Feature.CommandBuilders, []orchcmd.CommandBuilder{stubCommandBuilderOne, stubCommandBuilderTwo})
}

func assertBotDependenciesWireRuntimeObjects(
	t *testing.T,
	deps *orchestration.Dependencies,
	cacheClient *cachemocks.Client,
	postgres *databasemocks.Client,
	memberData *membermocks.DataProvider,
	alarmCRUD *stubAlarmCRUD,
	activityLogger *activity.Logger,
	settingsService *settingsmocks.ReadWriter,
) {
	t.Helper()

	if deps.Cache != cacheClient {
		t.Fatal("Dependencies.Cache did not preserve the module cache client")
	}

	if deps.Postgres != postgres {
		t.Fatal("Dependencies.Postgres did not preserve the module postgres client")
	}

	if deps.CalendarImageCacheDir != "data/test-calendar-cache" {
		t.Fatalf("Dependencies.CalendarImageCacheDir = %q, want data/test-calendar-cache", deps.CalendarImageCacheDir)
	}

	if deps.CalendarEntryCacheTTL != time.Hour {
		t.Fatalf("Dependencies.CalendarEntryCacheTTL = %s, want 1h", deps.CalendarEntryCacheTTL)
	}

	if deps.MembersData != memberData {
		t.Fatal("Dependencies.MembersData did not preserve the module member data provider")
	}

	if deps.Alarm != alarmCRUD {
		t.Fatal("Dependencies.Alarm did not preserve the module alarm CRUD provider")
	}

	if deps.Activity != activityLogger {
		t.Fatal("Dependencies.Activity did not preserve the activity logger")
	}

	if deps.Settings != settingsService {
		t.Fatal("Dependencies.Settings did not preserve the settings service")
	}

	if !deps.MarkdownReplies {
		t.Fatal("Dependencies.MarkdownReplies did not preserve the module markdown replies flag")
	}

	assertCommandBuilderPointers(t, deps.CommandBuilders, []orchcmd.CommandBuilder{stubCommandBuilderOne, stubCommandBuilderTwo})
}

func TestProvideACLServiceWrapsInitializationError(t *testing.T) {
	t.Parallel()

	_, err := ProvideACLService(t.Context(), true, "whitelist", []string{testRoomA}, nil, slog.New(slog.DiscardHandler))
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
	return &iris.ReplyAcceptedResponse{}, nil
}

func (s *stubBotIrisClient) SendImage(context.Context, string, []byte, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return &iris.ReplyAcceptedResponse{}, nil
}

func (s *stubBotIrisClient) SendMultipleImages(context.Context, string, [][]byte, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return &iris.ReplyAcceptedResponse{}, nil
}

func (s *stubBotIrisClient) SendMarkdown(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return &iris.ReplyAcceptedResponse{}, nil
}

func (s *stubBotIrisClient) GetReplyStatus(context.Context, string) (*iris.ReplyStatusSnapshot, error) {
	return &iris.ReplyStatusSnapshot{}, nil
}

func (s *stubBotIrisClient) Ping(context.Context) bool {
	return true
}

func (s *stubBotIrisClient) GetConfig(context.Context) (*iris.ConfigResponse, error) {
	return &iris.ConfigResponse{}, nil
}

func (s *stubBotIrisClient) GetRooms(context.Context) (*iris.RoomListResponse, error) {
	return &iris.RoomListResponse{}, nil
}

type stubAlarmCRUD struct {
	targetMinutes []int
}

func (s *stubAlarmCRUD) AddAlarm(context.Context, *domain.AddAlarmRequest) (bool, error) {
	return false, nil
}

func (s *stubAlarmCRUD) RemoveAlarm(context.Context, string, string, domain.AlarmTypes) (bool, error) {
	return false, nil
}

func (s *stubAlarmCRUD) RemoveHostAlarm(context.Context, string, string, string, domain.AlarmTypes) (bool, error) {
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

func (s *stubAlarmCRUD) GetAllAlarmKeys(context.Context) ([]*domain.AlarmEntry, error) {
	return nil, nil
}

func (s *stubAlarmCRUD) WarmCacheFromDB(context.Context) error {
	return nil
}

func stubCommandBuilderOne(*handlercore.Dependencies) handlercore.Command {
	return nil
}

func stubCommandBuilderTwo(*handlercore.Dependencies) handlercore.Command {
	return nil
}

func stubCommandBuilderThree(*handlercore.Dependencies) handlercore.Command {
	return nil
}

func assertCommandBuilderPointers(t *testing.T, got, want []orchcmd.CommandBuilder) {
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
