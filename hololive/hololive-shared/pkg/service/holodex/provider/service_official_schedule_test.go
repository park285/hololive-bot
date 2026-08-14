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
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
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

func TestGetChannelScheduleUsesYouTubeBeforeOfficialAPI(t *testing.T) {
	var officialRequests atomic.Int32
	officialServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		officialRequests.Add(1)
		http.Error(writer, "unexpected official schedule request", http.StatusInternalServerError)
	}))
	t.Cleanup(officialServer.Close)

	requester := retryableHolodexFailureRequester("channel_schedule")
	scraperService := htmlscraper.NewTestServiceWithHTTPClient(
		officialServer.Client(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		officialServer.URL,
		func(context.Context, string) ([]*parser.UpcomingEvent, error) {
			return []*parser.UpcomingEvent{{
				VideoID: "youtube-schedule",
				Title:   "From YouTube",
				Status:  "UPCOMING",
			}}, nil
		},
	)
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	streams, err := service.GetChannelSchedule(context.Background(), "channel-1", 24, false)
	if err != nil {
		t.Fatalf("GetChannelSchedule() error = %v", err)
	}
	if len(streams) != 1 || streams[0].ID != "youtube-schedule" || streams[0].Status != domain.StreamStatusUpcoming {
		t.Fatalf("streams = %#v", streams)
	}
	if got := officialRequests.Load(); got != 0 {
		t.Fatalf("official schedule requests = %d, want 0", got)
	}
}

func TestGetChannelScheduleUsesOfficialScheduleAPIAfterYouTubeFailure(t *testing.T) {
	var officialRequests atomic.Int32
	future := time.Now().In(time.FixedZone("Asia/Tokyo", 9*60*60)).Add(time.Hour).Format("2006/01/02 15:04:05")
	officialServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		officialRequests.Add(1)
		if request.Method != http.MethodGet || request.URL.Path != "/api/list/2" {
			http.Error(writer, "unexpected official schedule request", http.StatusNotFound)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{"dateGroupList":[{"videoList":[
			{"datetime":%q,"url":"https://www.youtube.com/watch?v=keep-me","name":"Keep","title":"Keep"},
			{"datetime":%q,"url":"https://www.youtube.com/watch?v=drop-me","name":"Drop","title":"Drop"}
		]}]}`, future, future)
	}))
	t.Cleanup(officialServer.Close)

	requester := retryableHolodexFailureRequester("channel_schedule")
	scraperService := htmlscraper.NewTestServiceWithHTTPClient(
		officialServer.Client(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		officialServer.URL,
		func(context.Context, string) ([]*parser.UpcomingEvent, error) {
			return nil, context.DeadlineExceeded
		},
	)
	scraperService.SetOfficialScheduleIdentityForTest([]*domain.Member{
		{Name: "Keep", ChannelID: "channel-1"},
		{Name: "Drop", ChannelID: "channel-2"},
	})
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	streams, err := service.GetChannelSchedule(context.Background(), "channel-1", 24, false)
	if err != nil {
		t.Fatalf("GetChannelSchedule() error = %v", err)
	}
	if len(streams) != 1 || streams[0].ID != "keep-me" || streams[0].ChannelID != "channel-1" || streams[0].Status != domain.StreamStatusUpcoming {
		t.Fatalf("streams = %#v", streams)
	}
	if streams[0].StartActual != nil {
		t.Fatalf("official schedule row leaked live truth: %#v", streams[0])
	}
	if got := officialRequests.Load(); got != 1 {
		t.Fatalf("official schedule requests = %d, want 1", got)
	}
}

func TestGetChannelScheduleDoesNotFallbackOnHolodexSuccessEmpty(t *testing.T) {
	var officialRequests atomic.Int32
	var youtubeCalls atomic.Int32
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
		func(context.Context, string) ([]*parser.UpcomingEvent, error) {
			youtubeCalls.Add(1)
			return nil, fmt.Errorf("youtube must not run on holodex success-empty")
		},
	)
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	streams, err := service.GetChannelSchedule(context.Background(), "channel-1", 24, false)
	if err != nil {
		t.Fatalf("GetChannelSchedule() error = %v", err)
	}
	if len(streams) != 0 || officialRequests.Load() != 0 || youtubeCalls.Load() != 0 {
		t.Fatalf("streams=%d official=%d youtube=%d", len(streams), officialRequests.Load(), youtubeCalls.Load())
	}
}

func TestGetChannelsLiveStatusDoesNotUseOfficialSchedule(t *testing.T) {
	var officialRequests atomic.Int32
	var youtubeCalls atomic.Int32
	officialServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		officialRequests.Add(1)
		http.Error(writer, "unexpected official schedule request", http.StatusInternalServerError)
	}))
	t.Cleanup(officialServer.Close)

	requester := retryableHolodexFailureRequester("channels_live_status")
	scraperService := htmlscraper.NewTestServiceWithHTTPClient(
		officialServer.Client(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		officialServer.URL,
		func(context.Context, string) ([]*parser.UpcomingEvent, error) {
			youtubeCalls.Add(1)
			return nil, fmt.Errorf("youtube live fallback failed")
		},
	)
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	streams, err := service.GetChannelsLiveStatus(context.Background(), []string{"channel-1"})
	if err == nil {
		t.Fatal("GetChannelsLiveStatus() error = nil, want source failure")
	}
	if len(streams) != 0 {
		t.Fatalf("len(streams) = %d, want 0", len(streams))
	}
	if got := officialRequests.Load(); got != 0 {
		t.Fatalf("official schedule requests = %d, want 0", got)
	}
	if got := youtubeCalls.Load(); got != 1 {
		t.Fatalf("youtube live fallback calls = %d, want 1", got)
	}
}

func retryableHolodexFailureRequester(operation string) *MockRequester {
	return &MockRequester{DoRequestFunc: func(context.Context, string, string, url.Values) ([]byte, error) {
		return nil, &apiclient.APIError{
			Operation:  operation,
			StatusCode: http.StatusServiceUnavailable,
			Err:        fmt.Errorf("upstream unavailable"),
		}
	}}
}
