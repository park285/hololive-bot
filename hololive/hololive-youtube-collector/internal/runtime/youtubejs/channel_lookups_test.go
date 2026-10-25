package youtubejs

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

const lookupChannelID = "UC_TEST"

const liveChannelBody = `{"protocol_version":1,"live_sessions":[],"profile":{},"photo":[],"page_count":1,"exhausted":true,` +
	`"continuity":"NOT_APPLICABLE","termination_reason":"exhausted","schedule_lookups":%d,` +
	`"live_query":{"channel_id":"UC_TEST","source":"streams","statuses":["ENDED","LIVE","UPCOMING"],"exhausted":true,"access_restricted":false,"page_count":1}}`

// channelLookupRPC는 helper 응답 하나를 돌려주고 받은 요청의 예정 영상 조회 상한을 기록합니다.
func channelLookupRPC(t *testing.T, limiter *RateLimiter, status int, body string, requested *int) *RPC {
	t.Helper()

	return NewRPC(&http.Client{Transport: metricsTransport(func(req *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}

		var request ChannelRequest

		if err := jsonv2.Unmarshal(raw, &request, jsonv2.RejectUnknownMembers(true)); err != nil {
			return nil, err
		}

		*requested = request.MaxScheduleLookups

		return jsonResponse(status, body), nil
	})}, "http://helper", limiter)
}

// 한 RPC 안의 예정 영상 player 조회도 limiter 예약 하나씩으로 지불해 다른 RPC와 같은 간격을 따른다.
func TestFetchChannelPaysScheduleLookupsThroughLimiter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := NewRateLimiter(2 * time.Second)

		var requested int

		client := channelLookupRPC(t, limiter, http.StatusOK, fmt.Sprintf(liveChannelBody, 3), &requested)
		started := time.Now()

		result, err := client.FetchChannel(t.Context(), ChannelRequest{ChannelID: lookupChannelID, Kind: "live", MaxPages: 1})
		if err != nil {
			t.Fatal(err)
		}

		if requested != ChannelLiveMaxScheduleLookups || result.ScheduleLookups != 3 {
			t.Fatalf("requested lookups = %d, reported = %d", requested, result.ScheduleLookups)
		}

		// RPC 예약은 즉시이고 조회 세 건이 2초 간격 예약을 차례로 기다린다.
		if elapsed := time.Since(started); elapsed != 6*time.Second {
			t.Fatalf("elapsed = %s, want 6s", elapsed)
		}

		if got := limiterReservations(limiter); got != 4 {
			t.Fatalf("limiter reservations = %d, want 4", got)
		}
	})
}

func TestFetchChannelPaysScheduleLookupsBeforeFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := NewRateLimiter(2 * time.Second)

		var requested int

		body := `{"protocol_version":1,"error":{"code":"parser_drift","class":"DATA_CONTRACT","retry":{"kind":"default"},"message":"drift"},"schedule_lookups":2}`
		client := channelLookupRPC(t, limiter, http.StatusUnprocessableEntity, body, &requested)

		_, err := client.FetchChannel(t.Context(), ChannelRequest{ChannelID: lookupChannelID, Kind: "live", MaxPages: 1})
		if collecterr.CodeOf(err) != contract.ErrorParserDrift {
			t.Fatalf("error = %v, want parser drift", err)
		}

		if got := limiterReservations(limiter); got != 3 {
			t.Fatalf("limiter reservations = %d, want RPC plus two lookups", got)
		}
	})
}

func TestFetchChannelRejectsLookupsOutsideTheRequestedBudget(t *testing.T) {
	t.Parallel()

	var requested int

	live := channelLookupRPC(t, nil, http.StatusOK, fmt.Sprintf(liveChannelBody, ChannelLiveMaxScheduleLookups+1), &requested)
	if _, err := live.FetchChannel(t.Context(), ChannelRequest{ChannelID: lookupChannelID, Kind: "live", MaxPages: 1}); collecterr.CodeOf(err) != contract.ErrorHelperProtocolMismatch {
		t.Fatalf("live error = %v, want protocol mismatch", err)
	}

	metadata := channelLookupRPC(t, nil, http.StatusOK, fmt.Sprintf(liveChannelBody, 1), &requested)
	if _, err := metadata.FetchChannel(t.Context(), ChannelRequest{ChannelID: lookupChannelID, Kind: "metadata", MaxPages: 1}); collecterr.CodeOf(err) != contract.ErrorHelperProtocolMismatch {
		t.Fatalf("metadata error = %v, want protocol mismatch", err)
	}

	if requested != 0 {
		t.Fatalf("metadata request lookups = %d, want omitted", requested)
	}
}

// 예정 영상 조회 수는 /v1/channel 오류 본문에만 있을 수 있으며, 다른 endpoint에 오면 exact-key 위반이다.
func TestScheduleLookupsOutsideChannelEndpointAreProtocolMismatch(t *testing.T) {
	t.Parallel()

	body := `{"protocol_version":1,"error":{"code":"parser_drift","class":"DATA_CONTRACT","retry":{"kind":"default"},"message":"drift"},"schedule_lookups":1}`
	client := NewRPC(&http.Client{Transport: metricsTransport(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusUnprocessableEntity, body), nil
	})}, "http://helper", nil)

	_, err := client.FetchCommunity(t.Context(), CommunityRequest{ChannelID: lookupChannelID, MaxResults: 1, MaxPages: 1})
	if collecterr.CodeOf(err) != contract.ErrorHelperProtocolMismatch {
		t.Fatalf("error = %v, want protocol mismatch", err)
	}
}
