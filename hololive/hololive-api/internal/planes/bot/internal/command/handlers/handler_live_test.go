package handlers

import (
	"context"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
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
		CollabMembers: collabMembersStub{},
		LiveQuery:     &liveQueryStub{result: livequery.Result{Status: livequery.Complete}},
		Matcher:       matcher.NewMatcher(newContextAwareMemberProvider(members), slog.New(slog.DiscardHandler)),
		Formatter:     newSeededTestFormatter(t),
		SendMessage: func(_ context.Context, _, msg string) error {
			textSent = msg
			return nil
		},
		SendImage: func(_ context.Context, _ string, data []byte) error {
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

	err := NewLiveCommand(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, botcommand.LiveParams{})
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
		CollabMembers: collabMembersStub{},
		LiveQuery:     &liveQueryStub{result: livequery.Result{Status: livequery.Complete}},
		Matcher:       matcher.NewMatcher(memberProvider, slog.New(slog.DiscardHandler)),
		Formatter:     formatter.NewResponseFormatter("!", nil),
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

	err := NewLiveCommand(deps).Execute(ctx, &botcommand.CommandContext{Room: testRoomID}, botcommand.LiveParams{Member: testMemberAqua})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	if !memberProvider.ctxCapture.saw(ctx) {
		t.Fatal("expected matcher provider to receive request context")
	}
}
