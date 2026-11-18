package handlers

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	alarmcmd "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/alarm"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func ambiguousMembersFixture() []*domain.Member {
	return []*domain.Member{
		{ChannelID: "ch-aqua-holo", Name: testMemberAqua, Org: "Hololive"},
		{ChannelID: "ch-aqua-indie", Name: testMemberAqua, Org: "Independents"},
	}
}

func newAmbiguousMatcher() *matcher.Matcher {
	provider := newContextAwareMemberProvider(ambiguousMembersFixture())
	return matcher.NewMatcher(provider, slog.New(slog.DiscardHandler))
}

func expectedAmbiguousMessage(t *testing.T, matcherService *matcher.Matcher, commandExample string) string {
	t.Helper()

	var captured string

	deps := &handlercore.Dependencies{
		Alarm:     &alarmListViewerStub{},
		Matcher:   matcherService,
		Formatter: newSeededTestFormatter(t),
		SendMessage: func(_ context.Context, _, message string) error {
			captured = message
			return nil
		},
		SendError: func(context.Context, string, string) error {
			t.Fatal("alarm ambiguous path should use SendMessage, not SendError")

			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	err := alarmcmd.NewAlarmCommand(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, map[string]any{
		testParamAction: testActionAdd,
		paramMember:     testMemberAqua,
	})
	require.NoError(t, err)
	require.NotEmpty(t, captured, "alarm should emit ambiguous-member message")

	for _, member := range ambiguousMembersFixture() {
		require.Contains(t, captured, member.DisplayName()+" ("+member.GetOrg()+")", "ambiguous-member message should list every candidate")
	}

	const alarmExample = "!알람 추가 "

	require.Equal(t, 1, strings.Count(captured, alarmExample), "alarm ambiguous-member message should carry one copy example")

	return strings.Replace(captured, alarmExample, "!"+commandExample+" ", 1)
}

func TestLiveCommand_Execute_AmbiguousMember_SendsSameMessageAsAlarm(t *testing.T) {
	want := expectedAmbiguousMessage(t, newAmbiguousMatcher(), "라이브")

	var (
		gotMessage  string
		sendErrSeen bool
	)

	deps := &handlercore.Dependencies{
		CollabMembers: collabMembersStub{},
		LiveQuery:     &liveQueryStub{},
		Matcher:       newAmbiguousMatcher(),
		Formatter:     newSeededTestFormatter(t),
		SendMessage: func(_ context.Context, _, message string) error {
			gotMessage = message
			return nil
		},
		SendError: func(context.Context, string, string) error {
			sendErrSeen = true
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	err := NewLiveCommand(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, map[string]any{
		paramMember: testMemberAqua,
	})
	require.NoError(t, err)
	assert.False(t, sendErrSeen, "live should not fall through to a not-found error on ambiguity")
	assert.Equal(t, want, gotMessage)
}

func TestScheduleCommand_Execute_AmbiguousMember_SendsSameMessageAsAlarm(t *testing.T) {
	want := expectedAmbiguousMessage(t, newAmbiguousMatcher(), "일정")

	var (
		gotMessage  string
		sendErrSeen bool
	)

	deps := &handlercore.Dependencies{
		CollabMembers: collabMembersStub{},
		Holodex:       &scheduleStreamProviderStub{},
		Matcher:       newAmbiguousMatcher(),
		Formatter:     newSeededTestFormatter(t),
		SendMessage: func(_ context.Context, _, message string) error {
			gotMessage = message
			return nil
		},
		SendError: func(context.Context, string, string) error {
			sendErrSeen = true
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	err := NewScheduleCommand(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, map[string]any{
		paramMember: testMemberAqua,
	})
	require.NoError(t, err)
	assert.False(t, sendErrSeen, "schedule should not fall through to a not-found error on ambiguity")
	assert.Equal(t, want, gotMessage)
}

func TestUpcomingCommand_Execute_AmbiguousMember_SendsSameMessageAsAlarm(t *testing.T) {
	want := expectedAmbiguousMessage(t, newAmbiguousMatcher(), "예정")

	var (
		gotMessage  string
		sendErrSeen bool
	)

	deps := &handlercore.Dependencies{
		CollabMembers: collabMembersStub{},
		Holodex:       &upcomingStreamProviderStub{},
		Matcher:       newAmbiguousMatcher(),
		Formatter:     newSeededTestFormatter(t),
		SendMessage: func(_ context.Context, _, message string) error {
			gotMessage = message
			return nil
		},
		SendError: func(context.Context, string, string) error {
			sendErrSeen = true
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	err := NewUpcomingCommand(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, map[string]any{
		paramMember: testMemberAqua,
	})
	require.NoError(t, err)
	assert.False(t, sendErrSeen, "upcoming should not fall through to a not-found error on ambiguity")
	assert.Equal(t, want, gotMessage)
}
