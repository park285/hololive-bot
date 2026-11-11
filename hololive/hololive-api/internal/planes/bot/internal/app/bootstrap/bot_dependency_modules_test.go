package bootstrap

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/stretchr/testify/require"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	configsettings "github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

func TestBuildBotDependenciesPreservesRuntimeInputs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	cacheClient := cachemocks.NewLenientClient()
	postgres := &databasemocks.Client{}
	memberCache := &member.Cache{}
	alarmCRUD := &stubAlarmCRUD{targetMinutes: []int{15, 3, 1}}
	irisClient := &stubBotIrisClient{}
	messageAdapter := messaging.NewMessageAdapter("!")
	formatter := messageformatter.NewResponseFormatter("!", nil)

	appConfig := &apiconfig.BotPlaneConfig{
		Bot: configsettings.BotConfig{
			SelfUser:              "bot-self",
			Prefix:                "!",
			CalendarEntryCacheTTL: time.Hour,
			MarkdownReplies:       true,
		},
		Iris: configsettings.IrisConfig{BaseURL: "http://iris.local"},
	}

	deps := BuildBotDependencies(appConfig,
		&sharedmodules.InfraModule{Cache: cacheClient, Postgres: postgres, MemberCache: memberCache},
		nil,
		alarmCRUD,
		&CoreIntegrationServices{}, messageAdapter, formatter, nil, irisClient, logger,
	)

	require.Equal(t, "bot-self", deps.BotSelfUser)
	require.Equal(t, "http://iris.local", deps.IrisBaseURL)
	require.Equal(t, time.Hour, deps.CalendarEntryCacheTTL)
	require.Same(t, cacheClient, deps.Cache)
	require.Same(t, postgres, deps.Postgres)
	require.Same(t, memberCache, deps.MembersData)
	require.NotNil(t, deps.Matcher)
	require.Same(t, alarmCRUD, deps.Alarm)
	require.Same(t, irisClient, deps.Client)
	require.Same(t, messageAdapter, deps.MessageAdapter)
	require.Same(t, formatter, deps.Formatter)
	require.True(t, deps.MarkdownReplies)
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

func (s *stubBotIrisClient) SendRichText(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return &iris.ReplyAcceptedResponse{}, nil
}

func (s *stubBotIrisClient) GetReplyStatus(context.Context, string) (*iris.ReplyStatusSnapshot, error) {
	return &iris.ReplyStatusSnapshot{}, nil
}

func (s *stubBotIrisClient) Ping(context.Context) error {
	return nil
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
