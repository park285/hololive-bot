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
	"testing"
	"time"

	streammapping "github.com/kapu/hololive-shared/internal/service/holodex/provider/streammapping"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func verifyLiveEndpointRequest(method, path, org, wantStatus string, params url.Values) error {
	if method != http.MethodGet {
		return fmt.Errorf("unexpected method: %s", method)
	}

	if path != "/live" {
		return fmt.Errorf("unexpected path: %s", path)
	}

	if got := params.Get("org"); got != org {
		return fmt.Errorf("org = %s, want %s", got, org)
	}

	if got := params.Get("status"); got != wantStatus {
		return fmt.Errorf("status = %s, want %s", got, wantStatus)
	}

	if got := params.Get("limit"); got != "50" {
		return fmt.Errorf("limit = %s, want 50", got)
	}

	return nil
}

func assertCachedOrgStreams(t *testing.T, label, wantID string, fetch func() ([]*domain.Stream, error)) {
	t.Helper()

	first, err := fetch()
	if err != nil {
		t.Fatalf("%s() error = %v", label, err)
	}

	if len(first) != 1 || first[0].ID != wantID {
		t.Fatalf("%s() = %+v, want %s only", label, first, wantID)
	}

	second, err := fetch()
	if err != nil {
		t.Fatalf("%s() second call error = %v", label, err)
	}

	if len(second) != 1 || second[0].ID != wantID {
		t.Fatalf("%s() second call = %+v, want %s only", label, second, wantID)
	}
}

func liveByOrgFixture(t *testing.T, org, suborg string) []byte {
	t.Helper()

	return mustMarshalStreamRawList(t, []streammapping.StreamRaw{
		{
			ID:        "live-1",
			Title:     "Live stream",
			Status:    domain.StreamStatusLive,
			ChannelID: new("channel-live"),
			Channel: &streammapping.ChannelRaw{
				ID:   "channel-live",
				Name: "Live Member",
				Org:  &org,
			},
		},
		{
			ID:        "live-stars",
			Title:     "Filtered stars",
			Status:    domain.StreamStatusLive,
			ChannelID: new("channel-stars"),
			Channel: &streammapping.ChannelRaw{
				ID:     "channel-stars",
				Name:   "Stars Member",
				Org:    &org,
				Suborg: &suborg,
			},
		},
		{
			ID:        "upcoming-ignored",
			Title:     "Upcoming",
			Status:    domain.StreamStatusUpcoming,
			ChannelID: new("channel-live"),
			Channel: &streammapping.ChannelRaw{
				ID:   "channel-live",
				Name: "Live Member",
				Org:  &org,
			},
		},
	})
}

func upcomingByOrgFixture(t *testing.T, org string, scheduled *string) []byte {
	t.Helper()

	return mustMarshalStreamRawList(t, []streammapping.StreamRaw{
		{
			ID:             "upcoming-1",
			Title:          "Upcoming stream",
			Status:         domain.StreamStatusUpcoming,
			ChannelID:      new("channel-upcoming"),
			StartScheduled: scheduled,
			Channel: &streammapping.ChannelRaw{
				ID:   "channel-upcoming",
				Name: "Upcoming Member",
				Org:  &org,
			},
		},
		{
			ID:        "live-ignored",
			Title:     "Already live",
			Status:    domain.StreamStatusLive,
			ChannelID: new("channel-upcoming"),
			Channel: &streammapping.ChannelRaw{
				ID:   "channel-upcoming",
				Name: "Upcoming Member",
				Org:  &org,
			},
		},
	})
}

func TestGetLiveStreamsByOrg_CachesFilteredResults(t *testing.T) {
	t.Parallel()

	hololive := constants.HolodexAPIParams.OrgHololive
	requestCount := 0
	requester := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, params url.Values) ([]byte, error) {
			requestCount++

			if err := verifyLiveEndpointRequest(method, path, hololive, constants.HolodexAPIParams.StatusLive, params); err != nil {
				return nil, fmt.Errorf("verify live request: %w", err)
			}

			return liveByOrgFixture(t, hololive, "HOLOSTARS"), nil
		},
	}

	service := newServiceForFallbackTest(requester)

	assertCachedOrgStreams(t, "GetLiveStreamsByOrg", "live-1", func() ([]*domain.Stream, error) {
		return service.GetLiveStreamsByOrg(t.Context(), hololive)
	})

	if requestCount != 1 {
		t.Fatalf("request count = %d, want 1", requestCount)
	}
}

func TestGetLiveStreamsByOrg_CapsProviderResults(t *testing.T) {
	t.Parallel()

	hololive := constants.HolodexAPIParams.OrgHololive
	requester := &MockRequester{
		DoRequestFunc: func(_ context.Context, _, _ string, _ url.Values) ([]byte, error) {
			return mustMarshalStreamRawList(t, streamRawList(55, hololive, domain.StreamStatusLive)), nil
		},
	}

	service := newServiceForFallbackTest(requester)

	streams, err := service.GetLiveStreamsByOrg(t.Context(), hololive)
	if err != nil {
		t.Fatalf("GetLiveStreamsByOrg() error = %v", err)
	}

	if len(streams) != 50 {
		t.Fatalf("len(streams) = %d, want 50", len(streams))
	}
}

func TestGetLiveStreamsByOrg_ReturnsErrorAndSkipsCacheWhenAllSourcesFail(t *testing.T) {
	t.Parallel()

	primaryErr := errors.New("holodex unavailable")
	requester := &MockRequester{
		DoRequestFunc: func(_ context.Context, _, _ string, _ url.Values) ([]byte, error) {
			return nil, primaryErr
		},
	}
	scraperServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "scraper unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(scraperServer.Close)

	scraperService := newScraperServiceForTest(scraperServer.Client(), slog.Default(), scraperServer.URL, nil)
	service := newServiceForFallbackTestWithScraper(requester, scraperService)

	streams, err := service.GetLiveStreamsByOrg(t.Context(), constants.HolodexAPIParams.OrgHololive)
	if err == nil {
		t.Fatal("GetLiveStreamsByOrg() error = nil, want non-nil when all sources fail")
	}

	if len(streams) != 0 {
		t.Fatalf("GetLiveStreamsByOrg() len = %d, want 0", len(streams))
	}

	if _, found := service.cacheManager.GetLiveStreamsByOrg(t.Context(), constants.HolodexAPIParams.OrgHololive); found {
		t.Fatal("all-source failure must not cache an empty stream result")
	}
}

func TestGetLiveStreamsByOrg_ReturnsErrorWhenPrimaryFailsWithoutScraper(t *testing.T) {
	t.Parallel()

	requester := &MockRequester{
		DoRequestFunc: func(_ context.Context, _, _ string, _ url.Values) ([]byte, error) {
			return nil, errors.New("holodex unavailable")
		},
	}
	service := newServiceForFallbackTest(requester)

	streams, err := service.GetLiveStreamsByOrg(t.Context(), constants.HolodexAPIParams.OrgHololive)
	if err == nil {
		t.Fatal("GetLiveStreamsByOrg() error = nil, want non-nil when primary fails without scraper")
	}

	if len(streams) != 0 {
		t.Fatalf("GetLiveStreamsByOrg() len = %d, want 0", len(streams))
	}

	if _, found := service.cacheManager.GetLiveStreamsByOrg(t.Context(), constants.HolodexAPIParams.OrgHololive); found {
		t.Fatal("primary failure without scraper must not cache an empty stream result")
	}
}

func TestGetUpcomingStreamsByOrg_CachesFilteredResults(t *testing.T) {
	t.Parallel()

	hololive := constants.HolodexAPIParams.OrgHololive
	future := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
	requestCount := 0
	requester := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, params url.Values) ([]byte, error) {
			requestCount++

			if err := verifyLiveEndpointRequest(method, path, hololive, constants.HolodexAPIParams.StatusUpcoming, params); err != nil {
				return nil, fmt.Errorf("verify upcoming request: %w", err)
			}

			if got := params.Get("max_upcoming_hours"); got != "24" {
				return nil, fmt.Errorf("max_upcoming_hours = %s, want 24", got)
			}

			return upcomingByOrgFixture(t, hololive, &future), nil
		},
	}

	service := newServiceForFallbackTest(requester)

	assertCachedOrgStreams(t, "GetUpcomingStreamsByOrg", "upcoming-1", func() ([]*domain.Stream, error) {
		return service.GetUpcomingStreamsByOrg(t.Context(), 24, hololive)
	})

	if requestCount != 1 {
		t.Fatalf("request count = %d, want 1", requestCount)
	}
}

func TestGetUpcomingStreamsByOrg_CapsProviderResults(t *testing.T) {
	t.Parallel()

	hololive := constants.HolodexAPIParams.OrgHololive
	requester := &MockRequester{
		DoRequestFunc: func(_ context.Context, _, _ string, _ url.Values) ([]byte, error) {
			return mustMarshalStreamRawList(t, streamRawList(55, hololive, domain.StreamStatusUpcoming)), nil
		},
	}

	service := newServiceForFallbackTest(requester)

	streams, err := service.GetUpcomingStreamsByOrg(t.Context(), 24, hololive)
	if err != nil {
		t.Fatalf("GetUpcomingStreamsByOrg() error = %v", err)
	}

	if len(streams) != 50 {
		t.Fatalf("len(streams) = %d, want 50", len(streams))
	}
}

func TestGetChannelsLiveStatus_DoesNotCapBatch(t *testing.T) {
	t.Parallel()

	hololive := constants.HolodexAPIParams.OrgHololive

	const batch = 60

	requester := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, _ url.Values) ([]byte, error) {
			if method != http.MethodGet {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}

			if path != usersLivePath {
				return nil, fmt.Errorf("unexpected path: %s", path)
			}

			return mustMarshalStreamRawList(t, streamRawList(batch, hololive, domain.StreamStatusLive)), nil
		},
	}

	service := newServiceForFallbackTest(requester)

	channelIDs := make([]string, batch)
	for i := range channelIDs {
		channelIDs[i] = fmt.Sprintf("channel-%d", i)
	}

	streams, err := service.GetChannelsLiveStatus(t.Context(), channelIDs)
	if err != nil {
		t.Fatalf("GetChannelsLiveStatus() error = %v", err)
	}

	if len(streams) != batch {
		t.Fatalf("len(streams) = %d, want %d (internal detection path must not be capped)", len(streams), batch)
	}
}

func streamRawList(count int, org string, status domain.StreamStatus) []streammapping.StreamRaw {
	streams := make([]streammapping.StreamRaw, count)
	for i := range streams {
		id := fmt.Sprintf("stream-%d", i)
		channelID := fmt.Sprintf("channel-%d", i)

		streams[i] = streammapping.StreamRaw{
			ID:        id,
			Title:     id,
			Status:    status,
			ChannelID: &channelID,
			Channel: &streammapping.ChannelRaw{
				ID:   channelID,
				Name: channelID,
				Org:  &org,
			},
		}
	}

	return streams
}

func mustMarshalStreamRawList(t *testing.T, streams []streammapping.StreamRaw) []byte {
	t.Helper()

	body, err := jsonv2.Marshal(streams)
	if err != nil {
		t.Fatalf("marshal streams: %v", err)
	}

	return body
}
