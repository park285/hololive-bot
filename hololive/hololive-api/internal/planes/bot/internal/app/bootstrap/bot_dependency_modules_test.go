package bootstrap

import (
	"context"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/apifoundation"
	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	membermocks "github.com/kapu/hololive-api/internal/service/member/mocks"
	configsettings "github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

// ACL seed(KAKAO_ROOMS)는 chatID만 받으므로 fixture도 signed i64 문자열이다.
const testRoomA = "1001"

func TestBuildBotDependenciesPreservesRuntimeInputs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	cacheClient := cachemocks.NewLenientClient()
	postgres := &databasemocks.Client{}
	memberData := &membermocks.DataProvider{}
	alarmCRUD := &stubAlarmCRUD{targetMinutes: []int{15, 3, 1}}
	irisClient := &stubBotIrisClient{}
	messageAdapter := messaging.NewMessageAdapter("!", "@bot")
	formatter := messageformatter.NewResponseFormatter("!", nil)
	commandBuilders := []orchcmd.CommandBuilder{stubCommandBuilderOne, stubCommandBuilderTwo}

	appConfig := &apiconfig.BotPlaneConfig{
		Bot: configsettings.BotConfig{
			SelfUser:              "bot-self",
			Prefix:                "!",
			MentionPrefix:         "@bot",
			CalendarImageCacheDir: "data/test-calendar-cache",
			CalendarEntryCacheTTL: time.Hour,
			MarkdownReplies:       true,
		},
		Iris:         configsettings.IrisConfig{BaseURL: "http://iris.local"},
		Notification: configsettings.NotificationConfig{AdvanceMinutes: []int{15, 3, 1}},
	}

	deps := BuildBotDependencies(appConfig,
		&sharedmodules.InfraModule{Cache: cacheClient, Postgres: postgres},
		&apifoundation.ScraperHolodexFoundation{},
		&AlarmYouTubeStackComponents{AlarmMode: &AlarmModeComponents{AlarmCRUD: alarmCRUD, MemberDataSource: memberData}},
		&CoreIntegrationServices{CommandBuilders: commandBuilders}, messageAdapter, formatter, nil, irisClient, logger,
	)

	commandBuilders[0] = stubCommandBuilderThree

	require.Equal(t, "bot-self", deps.BotSelfUser)
	require.Equal(t, "http://iris.local", deps.IrisBaseURL)
	require.Equal(t, "data/test-calendar-cache", deps.CalendarImageCacheDir)
	require.Equal(t, time.Hour, deps.CalendarEntryCacheTTL)
	require.Equal(t, []int{15, 3, 1}, deps.Notification.AdvanceMinutes)
	require.Same(t, cacheClient, deps.Cache)
	require.Same(t, postgres, deps.Postgres)
	require.Same(t, memberData, deps.MembersData)
	require.Same(t, alarmCRUD, deps.Alarm)
	require.Same(t, irisClient, deps.Client)
	require.Same(t, messageAdapter, deps.MessageAdapter)
	require.Same(t, formatter, deps.Formatter)
	require.True(t, deps.MarkdownReplies)
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

func (s *stubAlarmCRUD) UpdateAlarmAdvanceMinutes(_ context.Context, minutes int) (domain.AdvanceMinutesResult, error) {
	s.targetMinutes = []int{minutes}
	return domain.AdvanceMinutesResult{RequestedMinutes: minutes, Outcome: domain.ApplyConfirmed, TargetMinutes: append([]int(nil), s.targetMinutes...)}, nil
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
