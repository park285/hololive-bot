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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	alarmcmd "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/alarm"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
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

func (s *trackedContextState) snapshot() []context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.seen)
}

func (s *trackedContextState) saw(want context.Context) bool {
	return slices.Contains(s.snapshot(), want)
}

func (s *trackedContextState) sawOnly(want context.Context) bool {
	seen := s.snapshot()

	return len(seen) > 0 && !slices.ContainsFunc(seen, func(got context.Context) bool { return got != want })
}

type trackedMemberProvider struct {
	state     *trackedContextState
	members   []*domain.Member
	byChannel map[string]*domain.Member
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
	return p.members
}

func (p *trackedMemberProvider) WithContext(ctx context.Context) domain.MemberDataProvider {
	p.state.record(ctx)

	return &trackedMemberProvider{
		state:     p.state,
		members:   p.members,
		byChannel: p.byChannel,
	}
}

func (p *trackedMemberProvider) FindMembersByName(string) []*domain.Member {
	return nil
}

func (p *trackedMemberProvider) FindMembersByAlias(string) []*domain.Member {
	return nil
}

type trackedStreamProvider struct {
	state   trackedContextState
	streams []*domain.Stream
}

func (p *trackedStreamProvider) GetLiveStreams(ctx context.Context) ([]*domain.Stream, error) {
	p.state.record(ctx)

	return p.streams, nil
}

func (p *trackedStreamProvider) GetUpcomingStreams(context.Context, int) ([]*domain.Stream, error) {
	return nil, nil
}

func (p *trackedStreamProvider) GetChannelSchedule(context.Context, string, int, bool) ([]*domain.Stream, error) {
	return nil, nil
}

func (p *trackedStreamProvider) GetChannel(context.Context, string) (*domain.Channel, error) {
	return nil, errTestStubNoChannel
}

func TestFindActiveMemberOrError_UsesRequestContextForMatcher(t *testing.T) {
	t.Parallel()

	reqCtx := context.WithValue(t.Context(), commandContextKey{}, "request")
	provider := newTrackedMemberProvider(&domain.Member{
		ChannelID: testChannelAqua,
		Name:      testMemberAqua,
	})

	var baseCtx context.Context

	matcherService := matcher.NewMatcher(baseCtx, provider, nil, nil, nil, newCommandTestLogger())

	deps := &handlercore.Dependencies{
		Matcher:   matcherService,
		Formatter: formatter.NewResponseFormatter("!", nil),
		SendError: func(context.Context, string, string) error {
			t.Fatal("unexpected SendError call")

			return nil
		},
	}

	channel, err := handlercore.FindActiveMemberOrError(reqCtx, deps, testRoomID, testMemberAqua)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, testChannelAqua, channel.ID)
	require.True(t, provider.state.saw(reqCtx), "matcher provider must observe the request context")
}

func TestAlarmCommand_HandleAdd_UsesRequestContextForMatcher(t *testing.T) {
	t.Parallel()

	reqCtx := context.WithValue(t.Context(), commandContextKey{}, "request")
	provider := newTrackedMemberProvider(&domain.Member{
		ChannelID:   testChannelAqua,
		Name:        testMemberAqua,
		IsGraduated: true,
		Org:         "Hololive",
	})
	matcherService := matcher.NewMatcher(t.Context(), provider, nil, nil, nil, newCommandTestLogger())

	var (
		sendErrorState trackedContextState
		sendErrorMsg   string
	)

	cmd := alarmcmd.NewAlarmCommand(&handlercore.Dependencies{
		Alarm:     &alarmListViewerStub{},
		Matcher:   matcherService,
		Formatter: formatter.NewResponseFormatter("!", nil),
		SendMessage: func(context.Context, string, string) error {
			t.Fatal("unexpected SendMessage call")

			return nil
		},
		SendError: func(ctx context.Context, _, message string) error {
			sendErrorState.record(ctx)

			sendErrorMsg = message

			return nil
		},
		Logger: newCommandTestLogger(),
	})

	err := cmd.Execute(reqCtx, &domain.CommandContext{Room: testRoomID}, map[string]any{
		testParamAction: "add",
		paramMember:     testMemberAqua,
	})
	require.NoError(t, err)
	require.True(t, sendErrorState.saw(reqCtx), "SendError must receive the request context")
	assert.Equal(t, messaging.ErrGraduatedMemberBlocked, sendErrorMsg)
	require.True(t, provider.state.saw(reqCtx), "matcher provider must observe the request context")
}

func TestLiveCommand_Execute_UsesRequestContextForMatcher(t *testing.T) {
	t.Parallel()

	reqCtx := context.WithValue(t.Context(), commandContextKey{}, "request")
	provider := newTrackedMemberProvider(&domain.Member{
		ChannelID: testChannelAqua,
		Name:      testMemberAqua,
	})

	var baseCtx context.Context

	matcherService := matcher.NewMatcher(baseCtx, provider, nil, nil, nil, newCommandTestLogger())
	streamProvider := &trackedStreamProvider{}

	var (
		sendMessageState trackedContextState
		sendMessageMsg   string
	)

	cmd := NewLiveCommand(&handlercore.Dependencies{
		Holodex:   streamProvider,
		Matcher:   matcherService,
		Formatter: formatter.NewResponseFormatter("!", nil),
		SendMessage: func(ctx context.Context, _, message string) error {
			sendMessageState.record(ctx)

			sendMessageMsg = message

			return nil
		},
		SendError: func(context.Context, string, string) error {
			t.Fatal("unexpected SendError call")

			return nil
		},
		Logger: newCommandTestLogger(),
	})

	err := cmd.Execute(reqCtx, &domain.CommandContext{Room: testRoomID}, map[string]any{
		paramMember: testMemberAqua,
	})
	require.NoError(t, err)
	require.True(t, streamProvider.state.saw(reqCtx), "stream provider must observe the request context")
	require.True(t, sendMessageState.saw(reqCtx), "SendMessage must receive the request context")
	assert.Equal(t, cmd.Deps().Formatter.FormatMemberNotLive(reqCtx, testMemberAqua), sendMessageMsg)
	require.True(t, provider.state.sawOnly(reqCtx), "matcher provider must observe only the request context")
}

func TestLiveCommand_Execute_UsesRequestContextForMembersData(t *testing.T) {
	t.Parallel()

	reqCtx := context.WithValue(t.Context(), commandContextKey{}, "request")
	provider := newTrackedMemberProvider(&domain.Member{
		ChannelID: testChannelAqua,
		Name:      testMemberAqua,
	})
	streamProvider := &trackedStreamProvider{}

	cmd := NewLiveCommand(&handlercore.Dependencies{
		Holodex: streamProvider,
		Chzzk: chzzk.NewClientWithConfig(&chzzk.ClientConfig{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			Logger:       newCommandTestLogger(),
		}),
		MembersData: provider,
		Matcher:     matcher.NewMatcher(nilBaseContext(), provider, nil, nil, nil, newCommandTestLogger()),
		Formatter:   formatter.NewResponseFormatter("!", setupAlarmCommandTestRenderer(t)),
		SendMessage: func(context.Context, string, string) error {
			return nil
		},
		SendError: func(context.Context, string, string) error {
			t.Fatal("unexpected SendError call")

			return nil
		},
		Logger: newCommandTestLogger(),
	})

	err := cmd.Execute(reqCtx, &domain.CommandContext{Room: testRoomID}, map[string]any{})
	require.NoError(t, err)
	require.True(t, provider.state.sawOnly(reqCtx), "matcher provider must observe only the request context")
}
