package holodexprovider

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	apiclient "github.com/kapu/hololive-shared/internal/service/holodex/provider/apiclient"
)

func TestGetChannelSchedule_ReturnsRetryableHolodexErrorWithoutScraperFallback(t *testing.T) {
	var officialRequests atomic.Int32

	officialServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		officialRequests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writeOfficialScheduleResponse(t, writer, `{"dateGroupList":[]}`)
	}))
	t.Cleanup(officialServer.Close)

	scraperService := newScraperServiceForTest(
		officialServer.Client(),
		slog.New(slog.DiscardHandler),
		officialServer.URL,
	)
	service := newServiceForFallbackTestWithScraper(retryableHolodexFailureRequester("channel_schedule"), scraperService)

	streams, err := service.GetChannelSchedule(t.Context(), testChannelID, 24, false)
	if err == nil {
		t.Fatalf("GetChannelSchedule() error = nil, streams = %#v; want Holodex error", streams)
	}

	if apiErr, ok := errors.AsType[*apiclient.APIError](err); !ok || apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("GetChannelSchedule() error = %v, want wrapped Holodex 503", err)
	}

	if _, found := service.cacheManager.GetChannelSchedule(t.Context(), testChannelID, 24, false); found {
		t.Fatal("GetChannelSchedule() cached a result after a source error")
	}

	if got := officialRequests.Load(); got != 0 {
		t.Fatalf("official schedule requests = %d, want 0", got)
	}
}
