package handlers

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type failedMemberDataProvider struct {
	domain.MemberDataProvider

	err error
}

func (f *failedMemberDataProvider) WithContext(context.Context) domain.MemberDataProvider { return f }

func (f *failedMemberDataProvider) LoadAllMembers() ([]*domain.Member, error) { return nil, f.err }

func TestMemberLookupPreservesBackendFailureWithoutNotFoundReply(t *testing.T) {
	cause := errors.New("member repository unavailable")

	for _, withCandidates := range []bool{false, true} {
		t.Run(map[bool]string{false: "best match", true: "candidates"}[withCandidates], func(t *testing.T) {
			replies := 0
			deps := &handlercore.Dependencies{
				Matcher:   matcher.NewMatcher(&failedMemberDataProvider{err: cause}, nil, slog.New(slog.DiscardHandler)),
				Formatter: formatter.NewResponseFormatter("!", nil),
				SendMessage: func(context.Context, string, string) error {
					replies++
					return nil
				},
				SendError: func(context.Context, string, string) error { replies++; return nil },
			}

			var err error

			if withCandidates {
				_, err = handlercore.FindMemberWithCandidatesOrError(t.Context(), deps, testRoomID, "미코", "일정")
			} else {
				_, err = handlercore.FindMemberOrError(t.Context(), deps, testRoomID, "미코")
			}

			if !errors.Is(err, cause) || errors.Is(err, handlercore.ErrMemberLookupHandled) {
				t.Fatalf("member lookup error = %v, want backend cause", err)
			}

			if replies != 0 {
				t.Fatalf("backend failure produced %d member-not-found replies", replies)
			}
		})
	}
}
