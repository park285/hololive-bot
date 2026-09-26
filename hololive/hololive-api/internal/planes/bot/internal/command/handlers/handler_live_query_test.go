package handlers

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
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

	if *message != deps.Formatter.LiveQuery(t.Context(), livequery.Result{Status: livequery.Complete}, "") {
		t.Fatalf("unexpected empty YouTube response: %q", *message)
	}
}

func TestLiveCommand_IncompleteResultLogsDiagnosticsInsteadOfReplying(t *testing.T) {
	deps, _, message := liveCardTestDeps(t, []*domain.Member{{ChannelID: testYouTubeChannelID, Name: testMemberAqua}})

	var logs bytes.Buffer

	deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	deps.LiveQuery = &liveQueryStub{result: livequery.Result{
		Status: livequery.Partial,
		Items:  []livequery.Item{{VideoID: "video-x", ChannelID: "ch-x", ChannelName: "Member X", Title: "방송"}},
		Channels: []livequery.Channel{
			{ChannelID: "ch-x", Reason: livequery.Covered},
			{ChannelID: "ch-y", Reason: livequery.Inconsistent},
			{ChannelID: "ch-z", Reason: livequery.Inconsistent},
		},
	}}

	if err := NewLiveCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, nil); err != nil {
		t.Fatal(err)
	}

	// 실제 목록 레이아웃은 handler_live_db_test가 검증한다. 여기서는 진단이 응답 대신 로그로 가는지만 본다.
	if *message == "" || strings.Contains(*message, "조회 미완료") {
		t.Fatalf("reply must not carry coverage diagnostics: %q", *message)
	}

	for _, want := range []string{`"msg":"live query incomplete"`, `"status":"partial"`, `"inconsistent":2`, `"covered":1`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("operator log missing %s: %s", want, logs.String())
		}
	}
}
