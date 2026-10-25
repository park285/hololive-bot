package youtubejs

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"testing"
	"testing/synctest"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

const lookupChannelID = "UC_TEST"

var liveChannelRequest = ChannelRequest{ChannelID: lookupChannelID, Kind: "live", MaxPages: 1}

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

		result, err := client.FetchChannel(t.Context(), liveChannelRequest)
		if err != nil {
			t.Fatal(err)
		}

		if requested != ChannelLiveMaxScheduleLookups || result.ScheduleLookups == nil || *result.ScheduleLookups != 3 {
			t.Fatalf("requested lookups = %d, reported = %v", requested, result.ScheduleLookups)
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

		_, err := client.FetchChannel(t.Context(), liveChannelRequest)
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
	if _, err := live.FetchChannel(t.Context(), liveChannelRequest); collecterr.CodeOf(err) != contract.ErrorHelperProtocolMismatch {
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

// traceRPC는 응답 전에 요청 전송 완료를 trace로 알리거나(wrote) 알리지 않는 helper transport입니다.
func traceRPC(limiter *RateLimiter, wrote bool, respond func() (*http.Response, error)) *RPC {
	return NewRPC(&http.Client{Transport: metricsTransport(func(req *http.Request) (*http.Response, error) {
		if trace := httptrace.ContextClientTrace(req.Context()); wrote && trace != nil && trace.WroteRequest != nil {
			trace.WroteRequest(httptrace.WroteRequestInfo{})
		}

		return respond()
	})}, "http://helper", limiter)
}

func nextLimiterWait(t *testing.T, limiter *RateLimiter) time.Duration {
	t.Helper()

	started := time.Now()

	if err := limiter.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}

	return time.Since(started)
}

// 지불을 기다리는 중 수집이 끝나도 이미 보낸 조회는 대기 없이 예약으로 남아 다음 RPC가 그만큼 기다린다.
func TestFetchChannelReservesUnpaidLookupsWhenPaymentIsCanceled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := NewRateLimiter(2 * time.Second)
		client := traceRPC(limiter, true, func() (*http.Response, error) {
			return jsonResponse(http.StatusOK, fmt.Sprintf(liveChannelBody, 3)), nil
		})

		// RPC는 0초, 첫 조회는 2초에 지불하고 4초 예약을 기다리는 3초에 수집이 끝난다.
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()

		if _, err := client.FetchChannel(ctx, liveChannelRequest); err == nil {
			t.Fatal("canceled payment returned success")
		}

		// 남은 두 조회가 4초·6초를 차지하므로 다음 예약은 8초다.
		if got := nextLimiterWait(t, limiter); got != 5*time.Second {
			t.Fatalf("next limiter wait = %s, want 5s", got)
		}
	})
}

// helper에 전송한 요청이 검증된 본문 없이 끝나면 보낸 조회 수를 알 수 없으므로 요청한 상한만큼 예약한다.
// 전송 전에 실패했거나 helper 오류 본문이 조회를 보고하지 않았으면 예약하지 않는다.
func TestFetchChannelReservesLookupBudgetOnlyWhenTheCountIsLost(t *testing.T) {
	cases := []struct {
		name    string
		wrote   bool
		respond func() (*http.Response, error)
		want    time.Duration
	}{
		{"sent then transport failed", true, func() (*http.Response, error) { return nil, errors.New("connection reset") }, time.Duration(ChannelLiveMaxScheduleLookups+1) * 2 * time.Second},
		{"sent then invalid success body", true, func() (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"protocol_version":1}`), nil
		}, time.Duration(ChannelLiveMaxScheduleLookups+1) * 2 * time.Second},
		{"not sent", false, func() (*http.Response, error) { return nil, errors.New("dial unix: no such file") }, 2 * time.Second},
		{"helper error without lookups", true, func() (*http.Response, error) {
			return jsonResponse(http.StatusUnprocessableEntity, `{"protocol_version":1,"error":{"code":"parser_drift","class":"DATA_CONTRACT","retry":{"kind":"default"},"message":"drift"}}`), nil
		}, 2 * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				limiter := NewRateLimiter(2 * time.Second)

				if _, err := traceRPC(limiter, tc.wrote, tc.respond).FetchChannel(t.Context(), liveChannelRequest); err == nil {
					t.Fatal("failed RPC returned success")
				}

				if got := nextLimiterWait(t, limiter); got != tc.want {
					t.Fatalf("next limiter wait = %s, want %s", got, tc.want)
				}
			})
		})
	}
}
