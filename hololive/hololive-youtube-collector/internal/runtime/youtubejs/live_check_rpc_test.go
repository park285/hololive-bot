package youtubejs

import (
	jsonv2 "encoding/json/v2"
	"net/http"
	"strings"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type sentLiveCheckRequest struct {
	ProtocolVersion         int16 `json:"protocol_version"`
	MaxSuccessResponseBytes int   `json:"max_success_response_bytes"`
}

func TestLiveCheckRPCBoundsBudgetAndKeepsAbsentFactsDistinct(t *testing.T) {
	t.Parallel()

	sent := map[string]sentLiveCheckRequest{}
	client := NewRPC(&http.Client{Transport: metricsTransport(func(req *http.Request) (*http.Response, error) {
		var body sentLiveCheckRequest

		if err := jsonv2.UnmarshalRead(req.Body, &body); err != nil {
			t.Errorf("decode request: %v", err)
		}

		sent[req.URL.Path] = body

		if req.URL.Path == "/v1/channel_live_check" {
			return jsonResponse(http.StatusOK, `{"protocol_version":1,"channel_id":"UC_TEST","outcome":"UPCOMING_VIDEO",`+
				`"selected_video_id":"upcoming-a","channel_identity_confirmed":true}`), nil
		}

		return jsonResponse(http.StatusOK, `{"protocol_version":1,"video_id":"ended-a","channel_id":"UC_TEST",`+
			`"identity_confirmed":true,"is_live_now":false,"is_private":false,"ended_at":"2026-09-01T11:00:00.000Z",`+
			`"availability":"PUBLIC","method":"player_public"}`), nil
	})}, "http://helper", nil)

	channel, err := client.FetchChannelLiveCheck(t.Context(), ChannelLiveCheckRequest{ChannelID: "UC_TEST", MaxSuccessResponseBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	if channel.Outcome != contract.ChannelLiveCheckUpcomingVideo || channel.SelectedVideoID != "upcoming-a" || !channel.ChannelIdentityConfirmed {
		t.Fatalf("channel result = %#v", channel)
	}

	video, err := client.FetchVideoLiveCheck(t.Context(), VideoLiveCheckRequest{VideoID: "ended-a"})
	if err != nil {
		t.Fatal(err)
	}

	if video.IsLive != nil || video.IsLiveNow == nil || *video.IsLiveNow ||
		video.EndedAt == nil || !video.EndedAt.Equal(time.Date(2026, time.September, 1, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("absent and false facts merged: %#v", video)
	}

	for _, path := range []string{"/v1/channel_live_check", "/v1/video_live_check"} {
		if got := sent[path]; got.ProtocolVersion != ProtocolVersion || got.MaxSuccessResponseBytes != MaxLiveCheckResponseBytes {
			t.Fatalf("%s request = %#v, want bounded live check budget", path, got)
		}
	}
}

func TestLiveCheckRPCRejectsResultContractViolations(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		body     string
		response any
	}{
		{"channel legacy pagination", `{"protocol_version":1,"channel_id":"UC_TEST","outcome":"CHANNEL_PAGE",` +
			`"channel_identity_confirmed":true,"page_count":1}`, &ChannelLiveCheckResult{}},
		{"channel outcome vocabulary", `{"protocol_version":1,"channel_id":"UC_TEST","outcome":"OFFLINE",` +
			`"channel_identity_confirmed":true}`, &ChannelLiveCheckResult{}},
		{"channel unknown without reason", `{"protocol_version":1,"channel_id":"UC_TEST","outcome":"UNKNOWN",` +
			`"channel_identity_confirmed":false}`, &ChannelLiveCheckResult{}},
		{"channel known with reason", `{"protocol_version":1,"channel_id":"UC_TEST","outcome":"CHANNEL_PAGE",` +
			`"channel_identity_confirmed":true,"unknown_reason":"not_waiting_state"}`, &ChannelLiveCheckResult{}},
		{"channel video-only reason", `{"protocol_version":1,"channel_id":"UC_TEST","outcome":"UNKNOWN",` +
			`"channel_identity_confirmed":true,"unknown_reason":"availability_unclassified"}`, &ChannelLiveCheckResult{}},
		{"channel protocol version", `{"protocol_version":2,"channel_id":"UC_TEST","outcome":"CHANNEL_PAGE",` +
			`"channel_identity_confirmed":true}`, &ChannelLiveCheckResult{}},
		{"video empty subject", `{"protocol_version":1,"video_id":"","identity_confirmed":false,` +
			`"availability":"UNKNOWN","method":"unknown","unknown_reason":"identity_missing"}`, &VideoLiveCheckResult{}},
		{"video method mismatch", `{"protocol_version":1,"video_id":"v","channel_id":"UC_TEST","identity_confirmed":true,` +
			`"is_private":false,"availability":"PUBLIC","method":"unknown"}`, &VideoLiveCheckResult{}},
		{"video unknown without reason", `{"protocol_version":1,"video_id":"v","identity_confirmed":false,` +
			`"availability":"UNKNOWN","method":"unknown"}`, &VideoLiveCheckResult{}},
		{"video known with reason", `{"protocol_version":1,"video_id":"v","channel_id":"UC_TEST","identity_confirmed":true,` +
			`"availability":"MEMBERS_ONLY","method":"player_members_only","unknown_reason":"error_unclassified"}`, &VideoLiveCheckResult{}},
		{"video channel-only reason", `{"protocol_version":1,"video_id":"v","identity_confirmed":false,` +
			`"availability":"UNKNOWN","method":"unknown","unknown_reason":"not_waiting_state"}`, &VideoLiveCheckResult{}},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertProtocolMismatch(t, decodeTestResponse(t, http.StatusOK, tt.body, MaxLiveCheckResponseBytes, tt.response))
		})
	}
}

func TestLiveCheckRPCContainsOversizedSuccessWithinOwnBudget(t *testing.T) {
	t.Parallel()

	client := NewRPC(&http.Client{Transport: metricsTransport(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"protocol_version":1,"video_id":"`+strings.Repeat("v", MaxLiveCheckResponseBytes)+`"}`), nil
	})}, "http://helper", nil)

	_, err := client.FetchVideoLiveCheck(t.Context(), VideoLiveCheckRequest{VideoID: "v", MaxSuccessResponseBytes: 1 << 20})
	if collecterr.CodeOf(err) != collecterr.ResponseTooLarge || collecterr.ClassOf(err) != collecterr.ClassResourceLimit {
		t.Fatalf("code/class = %q/%q, error = %v", collecterr.CodeOf(err), collecterr.ClassOf(err), err)
	}
}
