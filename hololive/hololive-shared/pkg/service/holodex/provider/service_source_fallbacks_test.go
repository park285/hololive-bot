package holodexprovider

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	apiclient "github.com/kapu/hololive-shared/internal/service/holodex/provider/apiclient"
)

// DEC-20260926-hololive-source-fallbacks-retirement: Holodex 채널·일정 조회는 원천 오류를 그대로 돌려준다.
// YouTube scraper·공식 일정·개별 조회로 보충하지 않고, 보충 결과(부분 Channel 포함)를 캐시하지도 않는다.

func TestGetChannel_ReturnsRetryableHolodexErrorWithoutScraperFallback(t *testing.T) {
	scraperService := newScraperServiceForTest(nil, slog.New(slog.DiscardHandler), "")
	service := newServiceForFallbackTestWithScraper(retryableHolodexFailureRequester("get_channel"), scraperService)

	channel, err := service.GetChannel(t.Context(), testChannelID)
	if err == nil {
		t.Fatalf("GetChannel() error = nil, channel = %#v; want Holodex error", channel)
	}

	if apiErr, ok := errors.AsType[*apiclient.APIError](err); !ok || apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("GetChannel() error = %v, want wrapped Holodex 503", err)
	}

	if channel != nil {
		t.Fatalf("GetChannel() channel = %#v, want nil", channel)
	}

	if cached, found := service.cacheManager.GetChannel(t.Context(), testChannelID); found {
		t.Fatalf("GetChannel() cached %#v after a source error, want no cache entry", cached)
	}
}

func TestGetChannels_ReturnsRetryableListErrorWithoutIndividualFallback(t *testing.T) {
	var individualRequests atomic.Int32

	requester := &MockRequester{DoRequestFunc: func(_ context.Context, _, path string, _ url.Values) ([]byte, error) {
		if strings.HasPrefix(path, "/channels/") {
			individualRequests.Add(1)

			return []byte(`{"id":"c1","name":"c1"}`), nil
		}

		return nil, &apiclient.APIError{
			Operation:  "list_channels",
			StatusCode: http.StatusServiceUnavailable,
			Err:        errors.New("upstream unavailable"),
		}
	}}
	service := newServiceForFallbackTest(requester)

	got, err := service.GetChannels(t.Context(), []string{"c1", "c2"})
	if err == nil {
		t.Fatalf("GetChannels() error = nil, got = %#v; want list error", got)
	}

	if !strings.Contains(err.Error(), "get channels batch list") {
		t.Fatalf("GetChannels() error = %v, want list error context", err)
	}

	if len(got) != 0 {
		t.Fatalf("GetChannels() len = %d, want 0", len(got))
	}

	if requests := individualRequests.Load(); requests != 0 {
		t.Fatalf("individual channel requests = %d, want 0", requests)
	}
}

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
