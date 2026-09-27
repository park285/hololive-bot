package holodexprovider

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	apiclient "github.com/kapu/hololive-shared/internal/service/holodex/provider/apiclient"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func writeOfficialScheduleResponse(t *testing.T, writer http.ResponseWriter, format string, args ...any) {
	t.Helper()

	if _, err := fmt.Fprintf(writer, format, args...); err != nil {
		t.Errorf("write official schedule response: %v", err)
	}
}

func TestGetLiveStreamsByOrgDoesNotUseOfficialSchedule(t *testing.T) {
	var officialRequests atomic.Int32

	officialServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		officialRequests.Add(1)
		http.Error(writer, "unexpected official schedule request", http.StatusInternalServerError)
	}))
	t.Cleanup(officialServer.Close)

	requester := &MockRequester{DoRequestFunc: func(context.Context, string, string, url.Values) ([]byte, error) {
		return nil, &apiclient.APIError{
			Operation:  "live_streams",
			StatusCode: http.StatusServiceUnavailable,
			Err:        errors.New("upstream unavailable"),
		}
	}}
	scraperService := newScraperServiceForTest(
		officialServer.Client(),
		slog.New(slog.DiscardHandler),
		officialServer.URL,
	)
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	streams, err := service.GetLiveStreamsByOrg(t.Context(), constants.HolodexAPIParams.OrgHololive)
	if err == nil {
		t.Fatal("GetLiveStreamsByOrg() error = nil, want source failure")
	}

	if len(streams) != 0 {
		t.Fatalf("len(streams) = %d, want 0", len(streams))
	}

	if got := officialRequests.Load(); got != 0 {
		t.Fatalf("official schedule requests = %d, want 0", got)
	}
}

func TestGetUpcomingStreamsByOrgUsesOfficialScheduleAPIOnlyOnPrimaryFailure(t *testing.T) {
	var officialRequests atomic.Int32

	future := time.Now().In(time.FixedZone("Asia/Tokyo", 9*60*60)).Add(time.Hour).Format("2006/01/02 15:04:05")
	officialServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		officialRequests.Add(1)

		if request.URL.Path != "/api/list/2" {
			http.NotFound(writer, request)

			return
		}

		writer.Header().Set("Content-Type", "application/json")
		writeOfficialScheduleResponse(t, writer, `{"dateGroupList":[{"videoList":[{
			"datetime":%q,
			"url":"https://www.youtube.com/watch?v=official",
			"name":"Member",
			"title":"Official"
		}]}]}`, future)
	}))
	t.Cleanup(officialServer.Close)

	requester := &MockRequester{DoRequestFunc: func(context.Context, string, string, url.Values) ([]byte, error) {
		return nil, &apiclient.APIError{
			Operation:  "upcoming_streams",
			StatusCode: http.StatusServiceUnavailable,
			Err:        errors.New("upstream unavailable"),
		}
	}}
	scraperService := newScraperServiceForTest(
		officialServer.Client(),
		slog.New(slog.DiscardHandler),
		officialServer.URL,
	)
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	streams, err := service.GetUpcomingStreamsByOrg(t.Context(), 24, constants.HolodexAPIParams.OrgHololive)
	if err != nil {
		t.Fatalf("GetUpcomingStreamsByOrg() error = %v", err)
	}

	if len(streams) != 1 || streams[0].ID != "official" || streams[0].Status != domain.StreamStatusUpcoming {
		t.Fatalf("streams = %#v", streams)
	}

	if got := officialRequests.Load(); got != 1 {
		t.Fatalf("official schedule requests = %d, want 1", got)
	}
}

func TestGetUpcomingStreamsByOrgDoesNotFallbackOnSuccessEmpty(t *testing.T) {
	var officialRequests atomic.Int32

	officialServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		officialRequests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writeOfficialScheduleResponse(t, writer, `{"dateGroupList":[]}`)
	}))
	t.Cleanup(officialServer.Close)

	requester := &MockRequester{DoRequestFunc: func(context.Context, string, string, url.Values) ([]byte, error) {
		return jsonv2.Marshal([]any{})
	}}
	scraperService := newScraperServiceForTest(
		officialServer.Client(),
		slog.New(slog.DiscardHandler),
		officialServer.URL,
	)
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	streams, err := service.GetUpcomingStreamsByOrg(t.Context(), 24, constants.HolodexAPIParams.OrgHololive)
	if err != nil {
		t.Fatalf("GetUpcomingStreamsByOrg() error = %v", err)
	}

	if len(streams) != 0 || officialRequests.Load() != 0 {
		t.Fatalf("streams=%d official requests=%d", len(streams), officialRequests.Load())
	}
}

func TestGetUpcomingStreamsByOrgReturnsErrorWhenBothSourcesFail(t *testing.T) {
	officialServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		http.Error(writer, `{"error":"unavailable"}`, http.StatusServiceUnavailable)
	}))
	t.Cleanup(officialServer.Close)

	requester := &MockRequester{DoRequestFunc: func(context.Context, string, string, url.Values) ([]byte, error) {
		return nil, &apiclient.APIError{
			Operation:  "upcoming_streams",
			StatusCode: http.StatusServiceUnavailable,
			Err:        errors.New("upstream unavailable"),
		}
	}}
	scraperService := newScraperServiceForTest(
		officialServer.Client(),
		slog.New(slog.DiscardHandler),
		officialServer.URL,
	)
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	streams, err := service.GetUpcomingStreamsByOrg(t.Context(), 24, constants.HolodexAPIParams.OrgHololive)
	if err == nil {
		t.Fatal("GetUpcomingStreamsByOrg() error = nil, want combined source error")
	}

	if len(streams) != 0 {
		t.Fatalf("len(streams) = %d, want 0", len(streams))
	}
}

func TestGetChannelScheduleDoesNotFallbackOnHolodexSuccessEmpty(t *testing.T) {
	var officialRequests atomic.Int32

	officialServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		officialRequests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writeOfficialScheduleResponse(t, writer, `{"dateGroupList":[]}`)
	}))
	t.Cleanup(officialServer.Close)

	requester := &MockRequester{DoRequestFunc: func(context.Context, string, string, url.Values) ([]byte, error) {
		return jsonv2.Marshal([]any{})
	}}
	scraperService := newScraperServiceForTest(
		officialServer.Client(),
		slog.New(slog.DiscardHandler),
		officialServer.URL,
	)
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	streams, err := service.GetChannelSchedule(t.Context(), testChannelID, 24, false)
	if err != nil {
		t.Fatalf("GetChannelSchedule() error = %v", err)
	}

	if len(streams) != 0 || officialRequests.Load() != 0 {
		t.Fatalf("streams=%d official=%d", len(streams), officialRequests.Load())
	}
}

// live-status는 Holodex /users/live 하나만 원천으로 쓴다. YouTube scraper 2차 경로는
// DEC-20260926-hololive-live-status-scraper-fallback-removal로 삭제됐고, 실패는 alarm-worker가
// persisted live session으로 판단하도록 그대로 돌려준다.
func TestGetChannelsLiveStatusDoesNotUseAnySecondarySource(t *testing.T) {
	var officialRequests atomic.Int32

	officialServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		officialRequests.Add(1)
		http.Error(writer, "unexpected official schedule request", http.StatusInternalServerError)
	}))
	t.Cleanup(officialServer.Close)

	requester := retryableHolodexFailureRequester(testOpChannelsLiveStatus)
	scraperService := newScraperServiceForTest(
		officialServer.Client(),
		slog.New(slog.DiscardHandler),
		officialServer.URL,
	)
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	streams, err := service.GetChannelsLiveStatus(t.Context(), []string{testChannelID})
	if err == nil {
		t.Fatal("GetChannelsLiveStatus() error = nil, want source failure")
	}

	if len(streams) != 0 {
		t.Fatalf("len(streams) = %d, want 0", len(streams))
	}

	if got := officialRequests.Load(); got != 0 {
		t.Fatalf("official schedule requests = %d, want 0", got)
	}
}

func retryableHolodexFailureRequester(operation string) *MockRequester {
	return &MockRequester{DoRequestFunc: func(context.Context, string, string, url.Values) ([]byte, error) {
		return nil, &apiclient.APIError{
			Operation:  operation,
			StatusCode: http.StatusServiceUnavailable,
			Err:        errors.New("upstream unavailable"),
		}
	}}
}
