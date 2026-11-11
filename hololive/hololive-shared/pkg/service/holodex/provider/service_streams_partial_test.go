package holodexprovider

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync/atomic"
	"testing"

	apiclient "github.com/kapu/hololive-shared/internal/service/holodex/provider/apiclient"
	"github.com/kapu/hololive-shared/pkg/constants"
)

func unavailableHolodexError(operation string) error {
	return &apiclient.APIError{
		Operation:  operation,
		StatusCode: http.StatusServiceUnavailable,
		Err:        errors.New("upstream unavailable"),
	}
}

func TestGetLiveStreamsByOrgAllReportsPartialFailureWithoutCaching(t *testing.T) {
	t.Parallel()

	hololive := constants.HolodexAPIParams.OrgHololive
	requester := &MockRequester{DoRequestFunc: func(_ context.Context, _, _ string, params url.Values) ([]byte, error) {
		if params.Get("org") == hololive {
			return liveByOrgFixture(t, hololive, "HOLOSTARS"), nil
		}

		return nil, unavailableHolodexError("live_streams")
	}}
	service := newServiceForFallbackTest(requester)

	streams, err := service.GetLiveStreamsByOrg(t.Context(), constants.HolodexAPIParams.OrgAll)

	partialErr, ok := errors.AsType[*PartialStreamsError](err)
	if !ok {
		t.Fatalf("GetLiveStreamsByOrg(all) error = %v, want *PartialStreamsError", err)
	}

	if slices.Contains(partialErr.FailedOrgs, hololive) || len(partialErr.FailedOrgs) == 0 {
		t.Fatalf("FailedOrgs = %v, want failed orgs excluding %s", partialErr.FailedOrgs, hololive)
	}

	if len(streams) != 1 || streams[0].ID != "live-1" {
		t.Fatalf("streams = %+v, want successful org streams", streams)
	}

	if _, found := service.cacheManager.GetLiveStreamsByOrg(t.Context(), constants.HolodexAPIParams.OrgAll); found {
		t.Fatal("partial primary result must not be cached")
	}
}

func TestGetUpcomingStreamsByOrgEmptyOfficialFallbackReturnsErrorWithoutCaching(t *testing.T) {
	t.Parallel()

	officialServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writeOfficialScheduleResponse(t, writer, `{"dateGroupList":[]}`)
	}))
	t.Cleanup(officialServer.Close)

	requester := &MockRequester{DoRequestFunc: func(context.Context, string, string, url.Values) ([]byte, error) {
		return nil, unavailableHolodexError("upcoming_streams")
	}}
	scraperService := newScraperServiceForTest(officialServer.Client(), slog.New(slog.DiscardHandler), officialServer.URL)
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	streams, err := service.GetUpcomingStreamsByOrg(t.Context(), 24, constants.HolodexAPIParams.OrgHololive)
	if err == nil {
		t.Fatal("GetUpcomingStreamsByOrg() error = nil, want primary error after empty fallback")
	}

	if len(streams) != 0 {
		t.Fatalf("len(streams) = %d, want 0", len(streams))
	}

	if _, found := service.cacheManager.GetUpcomingStreamsByOrg(t.Context(), constants.HolodexAPIParams.OrgHololive, 24); found {
		t.Fatal("empty fallback result must not be cached")
	}
}

func TestGetUpcomingStreamsByOrgCanceledPrimarySkipsOfficialFallback(t *testing.T) {
	t.Parallel()

	var officialRequests atomic.Int32

	officialServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		officialRequests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writeOfficialScheduleResponse(t, writer, `{"dateGroupList":[]}`)
	}))
	t.Cleanup(officialServer.Close)

	ctx, cancel := context.WithCancel(t.Context())
	requester := &MockRequester{DoRequestFunc: func(context.Context, string, string, url.Values) ([]byte, error) {
		cancel()

		return nil, context.Canceled
	}}
	scraperService := newScraperServiceForTest(officialServer.Client(), slog.New(slog.DiscardHandler), officialServer.URL)
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	_, err := service.GetUpcomingStreamsByOrg(ctx, 24, constants.HolodexAPIParams.OrgHololive)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GetUpcomingStreamsByOrg() error = %v, want context.Canceled", err)
	}

	if got := officialRequests.Load(); got != 0 {
		t.Fatalf("official schedule requests = %d, want 0 after caller cancellation", got)
	}
}
