package holodexprovider

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	sharedjson "github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	apiclient "github.com/kapu/hololive-shared/pkg/service/holodex/provider/apiclient"
	htmlscraper "github.com/kapu/hololive-shared/pkg/service/holodex/provider/htmlscraper"
)

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
			Err:        fmt.Errorf("upstream unavailable"),
		}
	}}
	scraperService := htmlscraper.NewTestServiceWithHTTPClient(
		officialServer.Client(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		officialServer.URL,
		nil,
	)
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	streams, err := service.GetLiveStreamsByOrg(context.Background(), constants.HolodexAPIParams.OrgHololive)
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
		_, _ = fmt.Fprintf(writer, `{"dateGroupList":[{"videoList":[{
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
			Err:        fmt.Errorf("upstream unavailable"),
		}
	}}
	scraperService := htmlscraper.NewTestServiceWithHTTPClient(
		officialServer.Client(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		officialServer.URL,
		nil,
	)
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	streams, err := service.GetUpcomingStreamsByOrg(context.Background(), 24, constants.HolodexAPIParams.OrgHololive)
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
		_, _ = io.WriteString(writer, `{"dateGroupList":[]}`)
	}))
	t.Cleanup(officialServer.Close)

	requester := &MockRequester{DoRequestFunc: func(context.Context, string, string, url.Values) ([]byte, error) {
		return sharedjson.Marshal([]any{})
	}}
	scraperService := htmlscraper.NewTestServiceWithHTTPClient(
		officialServer.Client(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		officialServer.URL,
		nil,
	)
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	streams, err := service.GetUpcomingStreamsByOrg(context.Background(), 24, constants.HolodexAPIParams.OrgHololive)
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
			Err:        fmt.Errorf("upstream unavailable"),
		}
	}}
	scraperService := htmlscraper.NewTestServiceWithHTTPClient(
		officialServer.Client(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		officialServer.URL,
		nil,
	)
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	streams, err := service.GetUpcomingStreamsByOrg(context.Background(), 24, constants.HolodexAPIParams.OrgHololive)
	if err == nil {
		t.Fatal("GetUpcomingStreamsByOrg() error = nil, want combined source error")
	}
	if len(streams) != 0 {
		t.Fatalf("len(streams) = %d, want 0", len(streams))
	}
}
