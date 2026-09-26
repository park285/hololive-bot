package handlers

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"log/slog"
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

func TestLiveCommand_LogsBlockingAndNonblockingDiagnostics(t *testing.T) {
	for _, status := range []livequery.Status{livequery.Partial, livequery.Complete} {
		t.Run(string(status), func(t *testing.T) {
			deps, _, _ := liveCardTestDeps(t, []*domain.Member{{ChannelID: testYouTubeChannelID, Name: testMemberAqua}})

			var logs bytes.Buffer

			deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

			reason := livequery.Covered

			if status == livequery.Partial {
				reason = livequery.Inconsistent
			}

			deps.LiveQuery = &liveQueryStub{result: livequery.Result{
				Status: status,
				Items:  []livequery.Item{{VideoID: "video-x", ChannelID: "ch-x", ChannelName: "Member X", Title: "방송"}},
				Channels: []livequery.Channel{
					{ChannelID: "ch-x", Reason: livequery.Covered, Diagnostics: livequery.Diagnostics{RetainedOrphanEnds: 1}},
					{ChannelID: "ch-y", Reason: reason, Diagnostics: livequery.Diagnostics{RetainedOrphanEnds: 2, EndedPendingEnds: 4, EndedHeadMismatches: 1}},
				},
			}}
			if err := NewLiveCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, nil); err != nil {
				t.Fatal(err)
			}

			var record struct {
				Status      livequery.Status         `json:"status"`
				Reasons     map[livequery.Reason]int `json:"reasons"`
				Diagnostics livequery.Diagnostics    `json:"nonblocking_diagnostics"`
			}

			if err := jsonv2.Unmarshal(logs.Bytes(), &record); err != nil {
				t.Fatal(err)
			}

			if record.Status != status || record.Diagnostics != (livequery.Diagnostics{RetainedOrphanEnds: 3, EndedPendingEnds: 4, EndedHeadMismatches: 1}) {
				t.Fatalf("diagnostic result = %+v", record)
			}

			if status == livequery.Partial && record.Reasons[livequery.Inconsistent] != 1 || status == livequery.Complete && record.Reasons[livequery.Covered] != 2 {
				t.Fatalf("blocking reasons = %+v", record.Reasons)
			}
		})
	}
}
