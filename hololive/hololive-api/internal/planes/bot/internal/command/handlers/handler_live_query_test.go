package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestLiveCommand_YouTubeQueryFailureIsNotEmpty(t *testing.T) {
	for _, member := range []bool{false, true} {
		for _, sendFails := range []bool{false, true} {
			deps, _, _ := liveCardTestDeps(t, []*domain.Member{{ChannelID: testYouTubeChannelID, Name: testMemberAqua}})

			deps.LiveQuery = &liveQueryStub{err: errors.New("YouTube query unavailable")}

			messages, queryErrors := 0, 0
			sendErr := errors.New("error delivery failed")

			deps.SendMessage = func(context.Context, string, string) error { messages++; return nil }
			deps.SendError = func(_ context.Context, room, message string) error {
				queryErrors++

				if room != testRoomID || message != messaging.ErrLiveStreamQueryFailed {
					t.Errorf("unexpected error response: room=%q message=%q", room, message)
				}

				if sendFails {
					return sendErr
				}

				return nil
			}

			params := map[string]any{}

			if member {
				params[paramMember] = testMemberAqua
			}

			err := NewLiveCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, params)
			if sendFails && !errors.Is(err, sendErr) || !sendFails && err != nil {
				t.Fatalf("Execute(member=%t, sendFails=%t)=%v", member, sendFails, err)
			}

			if messages != 0 || queryErrors != 1 {
				t.Errorf("messages=%d errors=%d, want 0/1", messages, queryErrors)
			}
		}
	}
}

func TestLiveCommand_RetiredProviderMetadataDoesNotRequireRosterLookup(t *testing.T) {
	deps, _, message := liveCardTestDeps(t, []*domain.Member{{ChannelID: testYouTubeChannelID, Name: testMemberAqua, ChzzkChannelID: "retired-channel", TwitchUserID: "retired-login"}})

	deps.LiveQuery = &liveQueryStub{result: livequery.Result{Status: livequery.Complete}}
	// 전체 조회는 플랫폼 매핑을 위한 별도 roster 로더를 필요로 하지 않는다.
	deps.MembersData = nil

	deps.SendError = func(context.Context, string, string) error {
		t.Fatal("unexpected query error")

		return nil
	}

	if err := NewLiveCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, nil); err != nil {
		t.Fatal(err)
	}

	if *message != deps.Formatter.FormatLiveStreams(t.Context(), nil) {
		t.Fatalf("unexpected empty YouTube response: %q", *message)
	}
}
