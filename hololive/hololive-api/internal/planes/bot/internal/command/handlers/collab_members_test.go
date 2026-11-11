package handlers

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/collabmembers"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type collabMembersStub struct {
	names map[string][]string
	err   error
}

func (s collabMembersStub) DisplayNames(_ context.Context, _ []collabmembers.Video) (map[string][]string, error) {
	if s.err != nil {
		return nil, s.err
	}

	return s.names, nil
}

func TestWithCollabMembersReplacesOfficialNamesOnCopies(t *testing.T) {
	collab := &domain.Stream{ID: "v1", ChannelID: "UC_host"}
	fallback := &domain.Stream{ID: "v2", ChannelID: "UC_host", CollaboTalentNames: []string{"さくらみこ"}}
	solo := &domain.Stream{ID: "v3", ChannelID: "UC_host"}

	got, err := withCollabMembers(t.Context(), collabMembersStub{names: map[string][]string{"v1": {"스이세이", "미코"}}},
		[]*domain.Stream{collab, fallback, solo, nil})
	if err != nil {
		t.Fatalf("withCollabMembers() error = %v", err)
	}

	if !slices.Equal(got[0].CollaboTalentNames, []string{"스이세이", "미코"}) {
		t.Fatalf("collab names = %v", got[0].CollaboTalentNames)
	}

	// 공식 일정 fallback이 담은 공식 표기명은 조회 결과가 없으면 표시하지 않는다.
	if got[1].CollaboTalentNames != nil || got[2] != solo || got[3] != nil {
		t.Fatalf("unexpected streams: fallback=%v solo=%p nil=%v", got[1].CollaboTalentNames, got[2], got[3])
	}

	if collab.CollaboTalentNames != nil || !slices.Equal(fallback.CollaboTalentNames, []string{"さくらみこ"}) {
		t.Fatal("source streams were mutated")
	}
}

type collabFailureCase struct {
	name    string
	want    string
	params  map[string]any
	holodex domain.StreamProvider
	command func(*handlercore.Dependencies) handlercore.Command
}

func collabFailureCases() []collabFailureCase {
	streams := []*domain.Stream{{ID: "v1", Title: "방송", ChannelID: testChannelMiko, ChannelName: "미코"}}

	return []collabFailureCase{
		{
			"live", messaging.ErrLiveStreamQueryFailed,
			map[string]any{},
			nil,
			func(d *handlercore.Dependencies) handlercore.Command { return NewLiveCommand(d) },
		},
		{
			"upcoming", messaging.ErrUpcomingStreamQueryFailed,
			map[string]any{},
			&upcomingStreamProviderStub{upcomingStreams: streams},
			func(d *handlercore.Dependencies) handlercore.Command { return NewUpcomingCommand(d) },
		},
		{
			"schedule", messaging.ErrScheduleQueryFailed,
			map[string]any{paramMember: "미코"},
			&scheduleStreamProviderStub{scheduleStreams: streams},
			func(d *handlercore.Dependencies) handlercore.Command { return NewScheduleCommand(d) },
		},
	}
}

func collabFailureDeps(t *testing.T, tc collabFailureCase, reader collabMembersStub, sentError *string) *handlercore.Dependencies {
	t.Helper()

	return &handlercore.Dependencies{
		Holodex:       tc.holodex,
		LiveQuery:     &liveQueryStub{result: livequery.Result{Status: livequery.Complete, Items: []livequery.Item{{VideoID: "v1", ChannelID: testChannelMiko}}}},
		CollabMembers: reader,
		Matcher:       newUpcomingTestMatcher([]*domain.Member{{ChannelID: testChannelMiko, Name: "미코"}}),
		Formatter:     formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, message string) error {
			t.Fatalf("list must not be sent without collab members: %q", message)

			return nil
		},
		SendError: func(_ context.Context, _, message string) error {
			*sentError = message

			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}
}

func TestStreamListCommandsReportCollabMemberFailure(t *testing.T) {
	for _, tc := range collabFailureCases() {
		t.Run(tc.name, func(t *testing.T) {
			var sentError string

			deps := collabFailureDeps(t, tc, collabMembersStub{err: errors.New("schedule items unavailable")}, &sentError)

			if err := tc.command(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, tc.params); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			if sentError != tc.want {
				t.Fatalf("error reply = %q, want %q", sentError, tc.want)
			}
		})
	}
}

func TestStreamListCommandsReturnCollabMemberCancellation(t *testing.T) {
	for _, tc := range collabFailureCases() {
		t.Run(tc.name, func(t *testing.T) {
			var sentError string

			deps := collabFailureDeps(t, tc, collabMembersStub{err: context.Canceled}, &sentError)
			ctx, cancel := context.WithCancel(t.Context())

			cancel()

			err := tc.command(deps).Execute(ctx, &botcommand.CommandContext{Room: testRoomID}, tc.params)
			if !errors.Is(err, context.Canceled) || sentError != "" {
				t.Fatalf("Execute() error = %v, error reply = %q", err, sentError)
			}
		})
	}
}
