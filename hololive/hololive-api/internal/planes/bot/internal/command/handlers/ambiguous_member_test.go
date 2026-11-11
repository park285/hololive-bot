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

// expectedAmbiguousMessage는 alarm의 동명이인 응답을 기준으로 삼는다. 명령마다 다른 것은 복사 예시의 명령어뿐이다.
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
