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
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func newCommandTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

type commandContextKey struct{}

type trackedContextState struct {
	mu   sync.Mutex
	seen []context.Context
}

func (s *trackedContextState) record(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seen = append(s.seen, ctx)
}

func (s *trackedContextState) snapshot() []context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.seen)
}

// reset은 생성 단계의 기록을 지워 이후 요청 구간만 보게 한다.
func (s *trackedContextState) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seen = nil
}

func (s *trackedContextState) saw(want context.Context) bool {
	return slices.Contains(s.snapshot(), want)
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

func (p *trackedMemberProvider) FindMemberByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	p.state.record(ctx)

	if member := p.byChannel[channelID]; member != nil {
		return member, nil
	}

	return nil, domain.ErrMemberNotFound
}

func (p *trackedMemberProvider) FindMemberByName(ctx context.Context, _ string) (*domain.Member, error) {
	p.state.record(ctx)

	return nil, domain.ErrMemberNotFound
}

func (p *trackedMemberProvider) FindMemberByAlias(ctx context.Context, _ string) (*domain.Member, error) {
	p.state.record(ctx)

	return nil, domain.ErrMemberNotFound
}

func (p *trackedMemberProvider) GetChannelIDs(ctx context.Context) ([]string, error) {
	p.state.record(ctx)

	ids := make([]string, 0, len(p.byChannel))
	for id := range p.byChannel {
		ids = append(ids, id)
	}

	return ids, nil
}

func (p *trackedMemberProvider) LoadAllMembers(ctx context.Context) ([]*domain.Member, error) {
	p.state.record(ctx)

	return p.members, nil
}

func (p *trackedMemberProvider) FindMembersByName(ctx context.Context, _ string) ([]*domain.Member, error) {
	p.state.record(ctx)

	return []*domain.Member{}, nil
}

func (p *trackedMemberProvider) FindMembersByAlias(ctx context.Context, _ string) ([]*domain.Member, error) {
	p.state.record(ctx)

	return []*domain.Member{}, nil
}

func TestFindActiveMemberOrError_UsesRequestContextForMatcher(t *testing.T) {
	t.Parallel()

	reqCtx := context.WithValue(t.Context(), commandContextKey{}, "request")
	provider := newTrackedMemberProvider(&domain.Member{
		ChannelID: testChannelAqua,
		Name:      testMemberAqua,
	})

	matcherService := matcher.NewMatcher(provider, nil, newCommandTestLogger())

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
	matcherService := matcher.NewMatcher(provider, nil, newCommandTestLogger())

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
		testParamAction: testActionAdd,
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

	matcherService := matcher.NewMatcher(provider, nil, newCommandTestLogger())
	// NewMatcher는 생성 로그의 멤버 수를 base ctx로 읽는다. 명령 실행 구간의 ctx만 검사한다.
	provider.state.reset()

	streamProvider := &liveQueryStub{result: livequery.Result{Status: livequery.Complete}}

	var (
		sendMessageState trackedContextState
		sendMessageMsg   string
	)

	cmd := NewLiveCommand(&handlercore.Dependencies{
		LiveQuery: streamProvider,
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
	require.True(t, streamProvider.state.saw(reqCtx), "live query must observe the request context")
	require.True(t, sendMessageState.saw(reqCtx), "SendMessage must receive the request context")
	assert.Equal(t, cmd.Deps().Formatter.FormatMemberNotLive(reqCtx, testMemberAqua), sendMessageMsg)

	seen := provider.state.snapshot()
	require.NotEmpty(t, seen)

	for _, observed := range seen {
		// 공유 snapshot은 호출자 취소를 격리하되 요청 값과 자체 적재 예산을 보존한다.
		require.Equal(t, reqCtx.Value(commandContextKey{}), observed.Value(commandContextKey{}))

		if observed != reqCtx {
			_, hasDeadline := observed.Deadline()
			require.True(t, hasDeadline, "shared matcher load must have its own deadline")
		}
	}
}
