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

var liveChannelRequest = ChannelRequest{ChannelID: lookupChannelID, Kind: "live"}

const liveChannelBody = `{"protocol_version":1,"live_sessions":[],"profile":{},"photo":[],"page_count":1,"exhausted":true,` +
	`"continuity":"NOT_APPLICABLE","termination_reason":"exhausted","schedule_lookups":%d,` +
	`"live_query":{"channel_id":"UC_TEST","source":"streams","statuses":["ENDED","LIVE","UPCOMING"],"exhausted":true,"access_restricted":false,"page_count":1}}`

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
	if _, err := metadata.FetchChannel(t.Context(), ChannelRequest{ChannelID: lookupChannelID, Kind: "metadata"}); collecterr.CodeOf(err) != contract.ErrorHelperProtocolMismatch {
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

// lease 슬롯 시각은 재사용 일정의 관측 시각에만 쓰이므로 live 요청에만 실리고 metadata 요청에서는 빠진다.
func TestFetchChannelSendsLeaseSlotOnlyForLiveRequests(t *testing.T) {
	t.Parallel()

	slot := time.Date(2026, time.November, 6, 1, 0, 0, 0, time.UTC)

	for _, kind := range []string{"live", "metadata"} {
		var sent ChannelRequest

		client := NewRPC(&http.Client{Transport: metricsTransport(func(req *http.Request) (*http.Response, error) {
			raw, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}

			if err := jsonv2.Unmarshal(raw, &sent, jsonv2.RejectUnknownMembers(true)); err != nil {
				return nil, err
			}

			return jsonResponse(http.StatusInternalServerError, `{"protocol_version":1,"error_code":"helper_internal_invariant","error_class":"INTERNAL","message":"stop"}`), nil
		})}, "http://helper", nil)

		// helper 오류 본문으로 요청만 기록하고 끝냅니다.
		if _, err := client.FetchChannel(t.Context(), ChannelRequest{ChannelID: lookupChannelID, Kind: kind, ScheduledFor: new(slot)}); err == nil {
			t.Fatalf("%s request unexpectedly succeeded", kind)
		}

		if live := kind == "live"; (sent.ScheduledFor != nil) != live || (live && !sent.ScheduledFor.Equal(slot)) {
			t.Fatalf("%s request scheduled_for = %v", kind, sent.ScheduledFor)
		}
	}
}
