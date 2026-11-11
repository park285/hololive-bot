package handlers

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	alarmcmd "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/alarm"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type failedMemberDataProvider struct {
	domain.MemberDataProvider

	err error
}

func (f *failedMemberDataProvider) LoadAllMembers(context.Context) ([]*domain.Member, error) {
	return nil, f.err
}

func TestMemberLookupPreservesBackendFailureWithoutNotFoundReply(t *testing.T) {
	cause := errors.New("member repository unavailable")
	replies := 0
	deps := &handlercore.Dependencies{
		Matcher:   matcher.NewMatcher(&failedMemberDataProvider{err: cause}, slog.New(slog.DiscardHandler)),
		Formatter: formatter.NewResponseFormatter("!", nil),
		SendMessage: func(context.Context, string, string) error {
			replies++
			return nil
		},
		SendError: func(context.Context, string, string) error { replies++; return nil },
	}

	_, err := handlercore.FindMemberWithCandidatesOrError(t.Context(), deps, testRoomID, "미코", "일정")
	if !errors.Is(err, cause) || errors.Is(err, handlercore.ErrMemberLookupHandled) {
		t.Fatalf("member lookup error = %v, want backend cause", err)
	}

	if replies != 0 {
		t.Fatalf("backend failure produced %d member-not-found replies", replies)
	}
}

func TestAlarmMemberLookupPreservesBackendFailureWithoutNotFoundReply(t *testing.T) {
	cause := errors.New("member repository unavailable")

	for _, action := range []string{testActionAdd, "remove"} {
		t.Run(action, func(t *testing.T) {
			replies := 0
			deps := &handlercore.Dependencies{
				Alarm:     &alarmListViewerStub{},
				Matcher:   matcher.NewMatcher(&failedMemberDataProvider{err: cause}, slog.New(slog.DiscardHandler)),
				Formatter: formatter.NewResponseFormatter("!", nil),
				SendMessage: func(context.Context, string, string) error {
					replies++
					return nil
				},
				SendError: func(context.Context, string, string) error { replies++; return nil },
				Logger:    slog.New(slog.DiscardHandler),
			}

			err := alarmcmd.NewAlarmCommand(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, map[string]any{
				testParamAction: action,
				paramMember:     "미코",
			})
			if !errors.Is(err, cause) || errors.Is(err, handlercore.ErrMemberLookupHandled) {
				t.Fatalf("alarm %s error = %v, want backend cause", action, err)
			}

			if replies != 0 {
				t.Fatalf("backend failure produced %d alarm replies", replies)
			}
		})
	}
}

type channelLookupFailureProvider struct {
	*contextAwareMemberProvider

	err error
}

func (p *channelLookupFailureProvider) FindMemberByChannelID(context.Context, string) (*domain.Member, error) {
	return nil, p.err
}

func TestGraduationCheckPropagatesMemberLookupFailure(t *testing.T) {
	cause := errors.New("member cache unavailable")
	provider := &channelLookupFailureProvider{
		contextAwareMemberProvider: newContextAwareMemberProvider([]*domain.Member{{ChannelID: testChannelAqua, Name: testMemberAqua}}),
		err:                        cause,
	}
	deps := &handlercore.Dependencies{
		Matcher:   matcher.NewMatcher(provider, slog.New(slog.DiscardHandler)),
		Formatter: formatter.NewResponseFormatter("!", nil),
		SendMessage: func(context.Context, string, string) error {
			t.Fatal("lookup failure produced a reply")

			return nil
		},
		SendError: func(context.Context, string, string) error {
			t.Fatal("lookup failure produced an error reply")

			return nil
		},
	}

	channel, err := handlercore.FindActiveMemberWithCandidatesOrError(t.Context(), deps, testRoomID, testMemberAqua, "일정")
	if !errors.Is(err, cause) || channel != nil {
		t.Fatalf("FindActiveMemberWithCandidatesOrError() = %+v, %v; want lookup failure", channel, err)
	}
}
