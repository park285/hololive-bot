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
	"slices"
	"sync"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
)

func newCommandTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

type commandContextKey struct{}

// staticcheck literal nil 경고를 피하면서 nil base context 계약을 유지한다.
func nilBaseContext() context.Context {
	return nil
}

type trackedContextState struct {
	mu   sync.Mutex
	seen []context.Context
}

func (s *trackedContextState) record(ctx context.Context) {
	if ctx == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.seen = append(s.seen, ctx)
}

func (s *trackedContextState) assertAll(t *testing.T, want context.Context) {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()

	require.NotEmpty(t, s.seen)

	for _, got := range s.seen {
		assert.True(t, got == want)
	}
}

func (s *trackedContextState) assertContains(t *testing.T, want context.Context) {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()

	require.NotEmpty(t, s.seen)

	if slices.Contains(s.seen, want) {
		return
	}

	t.Fatalf("request context not observed in tracked contexts")
}

type trackedMemberProvider struct {
	state      *trackedContextState
	currentCtx context.Context
	members    []*domain.Member
	byChannel  map[string]*domain.Member
}

func newTrackedMemberProvider(members ...*domain.Member) *trackedMemberProvider {
	byChannel := make(map[string]*domain.Member, len(members))
	for _, member := range members {
		if member == nil || member.ChannelID == "" {
			continue
		}

		byChannel[member.ChannelID] = member
	}

	return &trackedMemberProvider{
		state:     &trackedContextState{},
		members:   members,
		byChannel: byChannel,
	}
}

func (p *trackedMemberProvider) FindMemberByChannelID(channelID string) *domain.Member {
	p.state.record(p.currentCtx)
	return p.byChannel[channelID]
}

func (p *trackedMemberProvider) FindMemberByName(string) *domain.Member {
	return nil
}

func (p *trackedMemberProvider) FindMemberByAlias(string) *domain.Member {
	return nil
}

func (p *trackedMemberProvider) GetChannelIDs() []string {
	ids := make([]string, 0, len(p.byChannel))
	for id := range p.byChannel {
		ids = append(ids, id)
	}

	return ids
}

func (p *trackedMemberProvider) GetAllMembers() []*domain.Member {
	p.state.record(p.currentCtx)
	return p.members
}

func (p *trackedMemberProvider) WithContext(ctx context.Context) domain.MemberDataProvider {
	return &trackedMemberProvider{
		state:      p.state,
		currentCtx: ctx,
		members:    p.members,
		byChannel:  p.byChannel,
	}
}

func (p *trackedMemberProvider) FindMembersByName(string) []*domain.Member {
	return nil
}

func (p *trackedMemberProvider) FindMembersByAlias(string) []*domain.Member {
	return nil
}

type trackedStreamProvider struct {
	seenCtx context.Context
	streams []*domain.Stream
}

func (p *trackedStreamProvider) GetLiveStreams(ctx context.Context) ([]*domain.Stream, error) {
	p.seenCtx = ctx
	return p.streams, nil
}

func (p *trackedStreamProvider) GetUpcomingStreams(context.Context, int) ([]*domain.Stream, error) {
	return nil, nil
}

func (p *trackedStreamProvider) GetChannelSchedule(context.Context, string, int, bool) ([]*domain.Stream, error) {
	return nil, nil
}

func (p *trackedStreamProvider) GetChannel(context.Context, string) (*domain.Channel, error) {
	return nil, nil
}

func TestFindActiveMemberOrError_UsesRequestContextForMatcher(t *testing.T) {
	t.Parallel()

	reqCtx := context.WithValue(t.Context(), commandContextKey{}, "request")
	provider := newTrackedMemberProvider(&domain.Member{
		ChannelID: "ch-aqua",
		Name:      "Aqua",
	})
	var baseCtx context.Context

	matcherService := matcher.NewMatcher(baseCtx, provider, nil, nil, nil, newCommandTestLogger())

	deps := &Dependencies{
		Matcher:   matcherService,
		Formatter: adapter.NewResponseFormatter("!", nil),
		SendError: func(context.Context, string, string) error {
			t.Fatal("unexpected SendError call")
			return nil
		},
	}

	channel, err := FindActiveMemberOrError(reqCtx, deps, "room-1", "Aqua")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, "ch-aqua", channel.ID)
	provider.state.assertContains(t, reqCtx)
}

func TestAlarmCommand_HandleAdd_UsesRequestContextForMatcher(t *testing.T) {
	t.Parallel()

	reqCtx := context.WithValue(t.Context(), commandContextKey{}, "request")
	provider := newTrackedMemberProvider(&domain.Member{
		ChannelID:   "ch-aqua",
		Name:        "Aqua",
		IsGraduated: true,
		Org:         "Hololive",
	})
	matcherService := matcher.NewMatcher(context.Background(), provider, nil, nil, nil, newCommandTestLogger())

	var (
		sendErrorCtx context.Context
		sendErrorMsg string
	)

	cmd := NewAlarmCommand(&Dependencies{
		Alarm:     &alarmListViewerStub{},
		Matcher:   matcherService,
		Formatter: adapter.NewResponseFormatter("!", nil),
		SendMessage: func(context.Context, string, string) error {
			t.Fatal("unexpected SendMessage call")
			return nil
		},
		SendError: func(ctx context.Context, _, message string) error {
			sendErrorCtx = ctx
			sendErrorMsg = message

			return nil
		},
		Logger: newCommandTestLogger(),
	})

	err := cmd.Execute(reqCtx, &domain.CommandContext{Room: "room-1"}, map[string]any{
		"action": "add",
		"member": "Aqua",
	})
	require.NoError(t, err)
	assert.True(t, sendErrorCtx == reqCtx)
	assert.Equal(t, adapter.ErrGraduatedMemberBlocked, sendErrorMsg)
	provider.state.assertContains(t, reqCtx)
}

func TestLiveCommand_Execute_UsesRequestContextForMatcher(t *testing.T) {
	t.Parallel()

	reqCtx := context.WithValue(t.Context(), commandContextKey{}, "request")
	provider := newTrackedMemberProvider(&domain.Member{
		ChannelID: "ch-aqua",
		Name:      "Aqua",
	})
	var baseCtx context.Context

	matcherService := matcher.NewMatcher(baseCtx, provider, nil, nil, nil, newCommandTestLogger())
	streamProvider := &trackedStreamProvider{}

	var (
		sendMessageCtx context.Context
		sendMessageMsg string
	)

	cmd := NewLiveCommand(&Dependencies{
		Holodex:   streamProvider,
		Matcher:   matcherService,
		Formatter: adapter.NewResponseFormatter("!", nil),
		SendMessage: func(ctx context.Context, _, message string) error {
			sendMessageCtx = ctx
			sendMessageMsg = message

			return nil
		},
		SendError: func(context.Context, string, string) error {
			t.Fatal("unexpected SendError call")
			return nil
		},
		Logger: newCommandTestLogger(),
	})

	err := cmd.Execute(reqCtx, &domain.CommandContext{Room: "room-1"}, map[string]any{
		"member": "Aqua",
	})
	require.NoError(t, err)
	assert.True(t, streamProvider.seenCtx == reqCtx)
	assert.True(t, sendMessageCtx == reqCtx)
	assert.Equal(t, cmd.Deps().Formatter.FormatMemberNotLive(reqCtx, "Aqua"), sendMessageMsg)
	provider.state.assertAll(t, reqCtx)
}

func TestLiveCommand_Execute_UsesRequestContextForMembersData(t *testing.T) {
	t.Parallel()

	reqCtx := context.WithValue(t.Context(), commandContextKey{}, "request")
	provider := newTrackedMemberProvider(&domain.Member{
		ChannelID: "ch-aqua",
		Name:      "Aqua",
	})
	streamProvider := &trackedStreamProvider{}

	cmd := NewLiveCommand(&Dependencies{
		Holodex: streamProvider,
		Chzzk: chzzk.NewClientWithConfig(&chzzk.ClientConfig{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			Logger:       newCommandTestLogger(),
		}),
		MembersData: provider,
		Matcher:     matcher.NewMatcher(nilBaseContext(), provider, nil, nil, nil, newCommandTestLogger()),
		Formatter:   adapter.NewResponseFormatter("!", setupAlarmCommandTestRenderer(t)),
		SendMessage: func(context.Context, string, string) error {
			return nil
		},
		SendError: func(context.Context, string, string) error {
			t.Fatal("unexpected SendError call")
			return nil
		},
		Logger: newCommandTestLogger(),
	})

	err := cmd.Execute(reqCtx, &domain.CommandContext{Room: "room-1"}, map[string]any{})
	require.NoError(t, err)
	provider.state.assertAll(t, reqCtx)
}
