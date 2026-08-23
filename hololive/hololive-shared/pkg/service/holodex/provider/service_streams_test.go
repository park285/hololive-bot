package holodexprovider

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	streammapping "github.com/kapu/hololive-shared/pkg/service/holodex/provider/streammapping"
)

func TestGetLiveStreamsByOrg_CachesFilteredResults(t *testing.T) {
	t.Parallel()

	hololive := constants.HolodexAPIParams.OrgHololive
	stars := "HOLOSTARS"
	requestCount := 0
	requester := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, params url.Values) ([]byte, error) {
			requestCount++
			if method != "GET" {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}
			if path != "/live" {
				return nil, fmt.Errorf("unexpected path: %s", path)
			}
			if got := params.Get("org"); got != hololive {
				return nil, fmt.Errorf("org = %s, want %s", got, hololive)
			}
			if got := params.Get("status"); got != constants.HolodexAPIParams.StatusLive {
				return nil, fmt.Errorf("status = %s, want %s", got, constants.HolodexAPIParams.StatusLive)
			}
			if got := params.Get("limit"); got != "50" {
				return nil, fmt.Errorf("limit = %s, want 50", got)
			}
			body := mustMarshalStreamRawList(t, []streammapping.StreamRaw{
				{
					ID:        "live-1",
					Title:     "Live stream",
					Status:    domain.StreamStatusLive,
					ChannelID: new("channel-live"),
					Channel: &streammapping.ChannelRaw{
						ID:   "channel-live",
						Name: "Live Member",
						Org:  &hololive,
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
						Org:    &hololive,
						Suborg: &stars,
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
						Org:  &hololive,
					},
				},
			})
			return body, nil
		},
	}

	service := newServiceForFallbackTest(requester)

	first, err := service.GetLiveStreamsByOrg(context.Background(), hololive)
	if err != nil {
		t.Fatalf("GetLiveStreamsByOrg() error = %v", err)
	}
	if len(first) != 1 || first[0].ID != "live-1" {
		t.Fatalf("GetLiveStreamsByOrg() = %+v, want live-1 only", first)
	}

	second, err := service.GetLiveStreamsByOrg(context.Background(), hololive)
	if err != nil {
		t.Fatalf("GetLiveStreamsByOrg() second call error = %v", err)
	}
	if len(second) != 1 || second[0].ID != "live-1" {
		t.Fatalf("GetLiveStreamsByOrg() second call = %+v, want live-1 only", second)
	}
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

	streams, err := service.GetLiveStreamsByOrg(context.Background(), hololive)
	if err != nil {
		t.Fatalf("GetLiveStreamsByOrg() error = %v", err)
	}
	if len(streams) != 50 {
		t.Fatalf("len(streams) = %d, want 50", len(streams))
	}
}

func TestGetLiveStreamsByOrg_ReturnsErrorAndSkipsCacheWhenAllSourcesFail(t *testing.T) {
	t.Parallel()

	primaryErr := fmt.Errorf("holodex unavailable")
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

	streams, err := service.GetLiveStreamsByOrg(context.Background(), constants.HolodexAPIParams.OrgHololive)
	if err == nil {
		t.Fatal("GetLiveStreamsByOrg() error = nil, want non-nil when all sources fail")
	}
	if len(streams) != 0 {
		t.Fatalf("GetLiveStreamsByOrg() len = %d, want 0", len(streams))
	}
	if _, found := service.cacheManager.GetLiveStreamsByOrg(context.Background(), constants.HolodexAPIParams.OrgHololive); found {
		t.Fatal("all-source failure must not cache an empty stream result")
	}
}

func TestGetLiveStreamsByOrg_ReturnsErrorWhenPrimaryFailsWithoutScraper(t *testing.T) {
	t.Parallel()

	requester := &MockRequester{
		DoRequestFunc: func(_ context.Context, _, _ string, _ url.Values) ([]byte, error) {
			return nil, fmt.Errorf("holodex unavailable")
		},
	}
	service := newServiceForFallbackTest(requester)

	streams, err := service.GetLiveStreamsByOrg(context.Background(), constants.HolodexAPIParams.OrgHololive)
	if err == nil {
		t.Fatal("GetLiveStreamsByOrg() error = nil, want non-nil when primary fails without scraper")
	}
	if len(streams) != 0 {
		t.Fatalf("GetLiveStreamsByOrg() len = %d, want 0", len(streams))
	}
	if _, found := service.cacheManager.GetLiveStreamsByOrg(context.Background(), constants.HolodexAPIParams.OrgHololive); found {
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
			if method != "GET" {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}
			if path != "/live" {
				return nil, fmt.Errorf("unexpected path: %s", path)
			}
			if got := params.Get("org"); got != hololive {
				return nil, fmt.Errorf("org = %s, want %s", got, hololive)
			}
			if got := params.Get("status"); got != constants.HolodexAPIParams.StatusUpcoming {
				return nil, fmt.Errorf("status = %s, want %s", got, constants.HolodexAPIParams.StatusUpcoming)
			}
			if got := params.Get("max_upcoming_hours"); got != "24" {
				return nil, fmt.Errorf("max_upcoming_hours = %s, want 24", got)
			}
			if got := params.Get("limit"); got != "50" {
				return nil, fmt.Errorf("limit = %s, want 50", got)
			}
			body := mustMarshalStreamRawList(t, []streammapping.StreamRaw{
				{
					ID:             "upcoming-1",
					Title:          "Upcoming stream",
					Status:         domain.StreamStatusUpcoming,
					ChannelID:      new("channel-upcoming"),
					StartScheduled: &future,
					Channel: &streammapping.ChannelRaw{
						ID:   "channel-upcoming",
						Name: "Upcoming Member",
						Org:  &hololive,
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
						Org:  &hololive,
					},
				},
			})
			return body, nil
		},
	}

	service := newServiceForFallbackTest(requester)

	first, err := service.GetUpcomingStreamsByOrg(context.Background(), 24, hololive)
	if err != nil {
		t.Fatalf("GetUpcomingStreamsByOrg() error = %v", err)
	}
	if len(first) != 1 || first[0].ID != "upcoming-1" {
		t.Fatalf("GetUpcomingStreamsByOrg() = %+v, want upcoming-1 only", first)
	}

	second, err := service.GetUpcomingStreamsByOrg(context.Background(), 24, hololive)
	if err != nil {
		t.Fatalf("GetUpcomingStreamsByOrg() second call error = %v", err)
	}
	if len(second) != 1 || second[0].ID != "upcoming-1" {
		t.Fatalf("GetUpcomingStreamsByOrg() second call = %+v, want upcoming-1 only", second)
	}
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

	streams, err := service.GetUpcomingStreamsByOrg(context.Background(), 24, hololive)
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
			if method != "GET" {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}
			if path != "/users/live" {
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

	streams, err := service.GetChannelsLiveStatus(context.Background(), channelIDs)
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
