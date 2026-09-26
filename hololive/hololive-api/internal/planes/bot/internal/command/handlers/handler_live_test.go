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

	"github.com/park285/iris-client-go/v2/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type liveQueryStub struct {
	result livequery.Result
	err    error
	state  trackedContextState
}

func (s *liveQueryStub) Query(ctx context.Context, _ livequery.Request) (livequery.Result, error) {
	s.state.record(ctx)

	return s.result, s.err
}

func liveCardTestDeps(t *testing.T, members []*domain.Member) (deps *handlercore.Dependencies, single *[]byte, text *string) {
	t.Helper()

	var (
		singleSent []byte
		textSent   string
	)

	deps = &handlercore.Dependencies{
		LiveQuery: &liveQueryStub{result: livequery.Result{Status: livequery.Complete}},
		Matcher:   matcher.NewMatcher(nilBaseContext(), newContextAwareMemberProvider(members), nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, msg string) error {
			textSent = msg
			return nil
		},
		SendImage: func(_ context.Context, _ string, data []byte, _ ...iris.SendOption) error {
			singleSent = data
			return nil
		},
		SendError: func(context.Context, string, string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	return deps, &singleSent, &textSent
}

func TestLiveCommand_Execute_AllLiveSendsText(t *testing.T) {
	t.Parallel()

	deps, singleSent, textSent := liveCardTestDeps(t, nil)

	deps.LiveQuery = &liveQueryStub{result: livequery.Result{Status: livequery.Complete, Items: []livequery.Item{{VideoID: "video-x", ChannelID: "ch-x", ChannelName: "Member X", Title: "방송"}}}}

	err := NewLiveCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, map[string]any{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if *textSent == "" {
		t.Fatal("expected text message")
	}

	if *singleSent != nil {
		t.Fatal("image path must not be used for all-live")
	}
}

func TestLiveCommand_MemberLookupPropagatesRequestContextToMatcher(t *testing.T) {
	t.Parallel()

	memberProvider := newContextAwareMemberProvider([]*domain.Member{{
		ChannelID: testChannelAqua,
		Name:      testMemberAqua,
	}})
	deps := &handlercore.Dependencies{
		LiveQuery: &liveQueryStub{result: livequery.Result{Status: livequery.Complete}},
		Matcher:   matcher.NewMatcher(nilBaseContext(), memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: formatter.NewResponseFormatter("!", nil),
		SendMessage: func(context.Context, string, string) error {
			return nil
		},
		SendError: func(context.Context, string, string) error {
			t.Fatal("unexpected send error")

			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	ctx := context.WithValue(t.Context(), testContextKey("request-id"), "live-propagation")

	err := NewLiveCommand(deps).Execute(ctx, &domain.CommandContext{Room: testRoomID}, map[string]any{
		paramMember: testMemberAqua,
	})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	if !memberProvider.ctxCapture.saw(ctx) {
		t.Fatal("expected matcher provider to receive request context")
	}
}
