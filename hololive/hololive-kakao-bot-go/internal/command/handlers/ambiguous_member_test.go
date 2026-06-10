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
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
)

type subscriberHolodexStub struct {
	subscriberCount int
}

func (s *subscriberHolodexStub) GetLiveStreams(context.Context) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *subscriberHolodexStub) GetUpcomingStreams(context.Context, int) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *subscriberHolodexStub) GetChannelSchedule(context.Context, string, int, bool) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *subscriberHolodexStub) GetChannel(_ context.Context, channelID string) (*domain.Channel, error) {
	count := s.subscriberCount
	return &domain.Channel{ID: channelID, Name: "Aqua", SubscriberCount: &count}, nil
}

func ambiguousMembersFixture() []*domain.Member {
	return []*domain.Member{
		{ChannelID: "ch-aqua-holo", Name: "Aqua", Org: "Hololive"},
		{ChannelID: "ch-aqua-indie", Name: "Aqua", Org: "Independents"},
	}
}

func newAmbiguousMatcher() *matcher.Matcher {
	provider := newContextAwareMemberProvider(ambiguousMembersFixture())
	return matcher.NewMatcher(nilBaseContext(), provider, nil, nil, nil, slog.New(slog.DiscardHandler))
}

// alarm은 이미 동명이인 응답을 보내므로 목표 메시지의 기준점이다.
func expectedAmbiguousMessage(t *testing.T, matcherService *matcher.Matcher, query string) string {
	t.Helper()

	var captured string
	deps := &Dependencies{
		Alarm:     &alarmListViewerStub{},
		Matcher:   matcherService,
		Formatter: adapter.NewResponseFormatter("!", nil),
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

	err := NewAlarmCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"action": "add",
		"member": query,
	})
	require.NoError(t, err)
	require.NotEmpty(t, captured, "alarm should emit ambiguous-member message")

	return captured
}

func TestLiveCommand_Execute_AmbiguousMember_SendsSameMessageAsAlarm(t *testing.T) {
	want := expectedAmbiguousMessage(t, newAmbiguousMatcher(), "Aqua")

	var (
		gotMessage  string
		sendErrSeen bool
	)
	deps := &Dependencies{
		Holodex:   &liveStreamProviderStub{},
		Matcher:   newAmbiguousMatcher(),
		Formatter: adapter.NewResponseFormatter("!", nil),
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

	err := NewLiveCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"member": "Aqua",
	})
	require.NoError(t, err)
	assert.False(t, sendErrSeen, "live should not fall through to a not-found error on ambiguity")
	assert.Equal(t, want, gotMessage)
}

func TestScheduleCommand_Execute_AmbiguousMember_SendsSameMessageAsAlarm(t *testing.T) {
	want := expectedAmbiguousMessage(t, newAmbiguousMatcher(), "Aqua")

	var (
		gotMessage  string
		sendErrSeen bool
	)
	deps := &Dependencies{
		Holodex:   &scheduleStreamProviderStub{},
		Matcher:   newAmbiguousMatcher(),
		Formatter: adapter.NewResponseFormatter("!", nil),
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

	err := NewScheduleCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"member": "Aqua",
	})
	require.NoError(t, err)
	assert.False(t, sendErrSeen, "schedule should not fall through to a not-found error on ambiguity")
	assert.Equal(t, want, gotMessage)
}

func TestUpcomingCommand_Execute_AmbiguousMember_SendsSameMessageAsAlarm(t *testing.T) {
	want := expectedAmbiguousMessage(t, newAmbiguousMatcher(), "Aqua")

	var (
		gotMessage  string
		sendErrSeen bool
	)
	deps := &Dependencies{
		Holodex:   &upcomingStreamProviderStub{},
		Matcher:   newAmbiguousMatcher(),
		Formatter: adapter.NewResponseFormatter("!", nil),
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

	err := NewUpcomingCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"member": "Aqua",
	})
	require.NoError(t, err)
	assert.False(t, sendErrSeen, "upcoming should not fall through to a not-found error on ambiguity")
	assert.Equal(t, want, gotMessage)
}

func TestSubscriberCommand_Execute_AmbiguousMember_SendsSameMessageAsAlarm(t *testing.T) {
	want := expectedAmbiguousMessage(t, newAmbiguousMatcher(), "Aqua")

	var (
		gotMessage  string
		sendErrSeen bool
	)
	deps := &Dependencies{
		Holodex:     &subscriberHolodexStub{subscriberCount: 12345},
		Matcher:     newAmbiguousMatcher(),
		MembersData: newContextAwareMemberProvider(ambiguousMembersFixture()),
		Formatter:   adapter.NewResponseFormatter("!", nil),
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

	err := NewSubscriberCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"member": "Aqua",
	})
	require.NoError(t, err)
	assert.False(t, sendErrSeen, "subscriber should not fall through to a not-found error on ambiguity")
	assert.Equal(t, want, gotMessage)
}
