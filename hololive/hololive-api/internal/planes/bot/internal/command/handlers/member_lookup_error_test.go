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

func (f *failedMemberDataProvider) LoadAllMembers(context.Context) ([]*domain.Member, error) {
	return nil, f.err
}

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

// channelLookupFailureProvider는 매칭 snapshot은 적재하지만 채널 대표 조회가 실패하는 원천이다.
type channelLookupFailureProvider struct {
	*contextAwareMemberProvider

	err error
}

func (p *channelLookupFailureProvider) FindMemberByChannelID(context.Context, string) (*domain.Member, error) {
	return nil, p.err
}

// 졸업 여부 확인의 조회 실패는 "졸업 아님"으로 추측해 진행하지 않고 오류로 돌려준다.
func TestGraduationCheckPropagatesMemberLookupFailure(t *testing.T) {
	cause := errors.New("member cache unavailable")
	provider := &channelLookupFailureProvider{
		contextAwareMemberProvider: newContextAwareMemberProvider([]*domain.Member{{ChannelID: testChannelAqua, Name: testMemberAqua}}),
		err:                        cause,
	}
	deps := &handlercore.Dependencies{
		Matcher:   matcher.NewMatcher(provider, nil, slog.New(slog.DiscardHandler)),
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

	channel, err := handlercore.FindActiveMemberOrError(t.Context(), deps, testRoomID, testMemberAqua)
	if !errors.Is(err, cause) || channel != nil {
		t.Fatalf("FindActiveMemberOrError() = %+v, %v; want lookup failure", channel, err)
	}
}
