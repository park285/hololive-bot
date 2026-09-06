package handlers

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	alarmcmd "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/alarm"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const commandUnitBChannel = "UC3OH5FKQ3qtl4uRme_vZTgA"

type memberAlarmRecorder struct {
	alarmListViewerStub

	added   []domain.AddAlarmRequest
	removed []domain.Alarm
}

func (r *memberAlarmRecorder) AddAlarm(_ context.Context, req *domain.AddAlarmRequest) (bool, error) {
	r.added = append(r.added, *req)
	return true, nil
}

func (r *memberAlarmRecorder) RemoveAlarm(_ context.Context, roomID, channelID string, types domain.AlarmTypes) (bool, error) {
	r.removed = append(r.removed, domain.Alarm{RoomID: roomID, ChannelID: channelID, AlarmTypes: types})
	return true, nil
}

func (r *memberAlarmRecorder) RemoveHostAlarm(_ context.Context, roomID, channelID, hostID string, types domain.AlarmTypes) (bool, error) {
	r.removed = append(r.removed, domain.Alarm{RoomID: roomID, ChannelID: channelID, HostID: hostID, AlarmTypes: types})
	return true, nil
}

func (*memberAlarmRecorder) GetNextStreamInfo(context.Context, string) (*domain.NextStreamInfo, error) {
	return &domain.NextStreamInfo{Status: domain.NextStreamStatusUpcoming, Title: "#宵凪ネオン"}, nil
}

func TestAlarmCommand_UNITBMemberSubscriptionsRemainRoomScoped(t *testing.T) {
	recorder := &memberAlarmRecorder{}
	memberProvider := newContextAwareMemberProvider([]*domain.Member{{ChannelID: commandUnitBChannel, Name: "유닛 B"}})
	logger := slog.New(slog.DiscardHandler)
	sent := make([]string, 0, 5)
	deps := &handlercore.Dependencies{
		Alarm:     recorder,
		Matcher:   matcher.NewMatcher(nilBaseContext(), memberProvider, nil, nil, nil, logger),
		Formatter: formatter.NewResponseFormatter("!", setupAlarmCommandTestRenderer(t)),
		SendMessage: func(_ context.Context, roomID, message string) error {
			require.Equal(t, testRoomID, roomID)

			sent = append(sent, message)

			return nil
		},
		SendError: func(_ context.Context, _, message string) error {
			t.Errorf("unexpected command error: %s", message)

			return nil
		},
		Logger: logger,
	}
	command := alarmcmd.NewAlarmCommand(deps)
	cmdCtx := &domain.CommandContext{Room: testRoomID}

	for _, name := range []string{"미라", "네온"} {
		require.NoError(t, command.Execute(t.Context(), cmdCtx, map[string]any{"action": testActionAdd, "member": name}))
	}

	require.Len(t, recorder.added, 2)
	require.Len(t, sent, 2)
	require.Equal(t, "reimei-mira", recorder.added[0].HostID)
	require.Equal(t, "yoinagi-neon", recorder.added[1].HostID)

	for _, added := range recorder.added {
		require.Equal(t, testRoomID, added.RoomID)
		require.Equal(t, commandUnitBChannel, added.ChannelID)
		require.Empty(t, added.UserID)
	}

	require.NotContains(t, sent[0], "#宵凪ネオン")

	for _, name := range []string{"미라", "유닛 B"} {
		require.NoError(t, command.Execute(t.Context(), cmdCtx, map[string]any{"action": "remove", "member": name}))
	}

	require.Len(t, recorder.removed, 2)
	require.Equal(t, "reimei-mira", recorder.removed[0].HostID)
	require.Empty(t, recorder.removed[1].HostID)

	for _, removed := range recorder.removed {
		require.Equal(t, testRoomID, removed.RoomID)
		require.Equal(t, commandUnitBChannel, removed.ChannelID)
	}

	recorder.entries = []domain.AlarmListView{
		{ChannelID: commandUnitBChannel, HostID: "reimei-mira", MemberName: "미라", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}},
		{ChannelID: commandUnitBChannel, HostID: "yoinagi-neon", MemberName: "네온", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts}},
	}

	require.NoError(t, command.Execute(t.Context(), cmdCtx, map[string]any{"action": "list"}))
	require.Contains(t, sent[len(sent)-1], "미라")
	require.Contains(t, sent[len(sent)-1], "네온")
}
