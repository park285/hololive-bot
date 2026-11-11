package handlers

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	alarmcmd "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/alarm"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
)

const (
	testActionRemove    = "remove"
	testUnitBMemberName = "유닛 B"
)

// !알람 추가와 삭제의 동명이인 안내는 모두 "알람 추가" 예시를 쓴다.
func TestAlarmAmbiguousMemberUsesAddExampleForEveryAction(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	renderer := serviceTemplate.NewRenderer(dbtest.NewPool(t), logger)

	for _, action := range []string{testActionAdd, testActionRemove} {
		t.Run(action, func(t *testing.T) {
			var sent []string

			deps := &handlercore.Dependencies{
				Alarm:     &alarmListViewerStub{},
				Matcher:   newAmbiguousMatcher(),
				Formatter: formatter.NewResponseFormatter("!", renderer),
				SendMessage: func(_ context.Context, _, message string) error {
					sent = append(sent, message)

					return nil
				},
				SendError: func(context.Context, string, string) error {
					t.Fatal("alarm ambiguous path should use SendMessage, not SendError")

					return nil
				},
				Logger: logger,
			}

			err := alarmcmd.NewAlarmCommand(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, map[string]any{
				testParamAction: action,
				paramMember:     testMemberAqua,
			})
			require.NoError(t, err)
			require.Len(t, sent, 1)
			require.Contains(t, sent[0], "!알람 추가 ")
		})
	}
}

// 졸업 차단은 알람 추가에만 적용하고, 구독 전용 host 대상도 그 채널 대표의 졸업 여부로 막는다. 삭제는 졸업 멤버도 진행한다.
func TestAlarmGraduationBlocksOnlyAdd(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		action, member string
		wantBlocked    bool
		wantRemoved    domain.Alarm
	}{
		"member add":    {action: testActionAdd, member: testMemberAqua, wantBlocked: true},
		"host add":      {action: testActionAdd, member: "미라", wantBlocked: true},
		"member remove": {action: testActionRemove, member: testMemberAqua, wantRemoved: domain.Alarm{ChannelID: testChannelAqua}},
		"host remove":   {action: testActionRemove, member: "미라", wantRemoved: domain.Alarm{ChannelID: commandUnitBChannel, HostID: "reimei-mira"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			recorder := &memberAlarmRecorder{}
			provider := newContextAwareMemberProvider([]*domain.Member{
				{ChannelID: testChannelAqua, Name: testMemberAqua, IsGraduated: true},
				{ChannelID: commandUnitBChannel, Name: testUnitBMemberName, IsGraduated: true},
			})
			logger := slog.New(slog.DiscardHandler)

			var errorReplies []string

			deps := &handlercore.Dependencies{
				Alarm:       recorder,
				Matcher:     matcher.NewMatcher(provider, logger),
				Formatter:   formatter.NewResponseFormatter("!", setupAlarmCommandTestRenderer(t)),
				SendMessage: func(context.Context, string, string) error { return nil },
				SendError: func(_ context.Context, _, message string) error {
					errorReplies = append(errorReplies, message)

					return nil
				},
				Logger: logger,
			}

			err := alarmcmd.NewAlarmCommand(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, map[string]any{
				testParamAction: tc.action,
				paramMember:     tc.member,
			})
			require.NoError(t, err)
			require.Empty(t, recorder.added)

			if tc.wantBlocked {
				require.Equal(t, []string{messaging.ErrGraduatedMemberBlocked}, errorReplies)
				require.Empty(t, recorder.removed)

				return
			}

			require.Empty(t, errorReplies)
			require.Len(t, recorder.removed, 1)
			require.Equal(t, tc.wantRemoved.ChannelID, recorder.removed[0].ChannelID)
			require.Equal(t, tc.wantRemoved.HostID, recorder.removed[0].HostID)
		})
	}
}
