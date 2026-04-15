package holodex

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	sharedjson "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
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
			body := mustMarshalStreamRawList(t, []StreamRaw{
				{
					ID:        "live-1",
					Title:     "Live stream",
					Status:    domain.StreamStatusLive,
					ChannelID: stringPtr("channel-live"),
					Channel: &ChannelRaw{
						ID:   "channel-live",
						Name: "Live Member",
						Org:  &hololive,
					},
				},
				{
					ID:        "live-stars",
					Title:     "Filtered stars",
					Status:    domain.StreamStatusLive,
					ChannelID: stringPtr("channel-stars"),
					Channel: &ChannelRaw{
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
					ChannelID: stringPtr("channel-live"),
					Channel: &ChannelRaw{
						ID:   "channel-live",
						Name: "Live Member",
						Org:  &hololive,
					},
				},
			})
			return body, nil
		},
	}

	svc := newServiceForFallbackTest(requester)

	first, err := svc.GetLiveStreamsByOrg(context.Background(), hololive)
	if err != nil {
		t.Fatalf("GetLiveStreamsByOrg() error = %v", err)
	}
	if len(first) != 1 || first[0].ID != "live-1" {
		t.Fatalf("GetLiveStreamsByOrg() = %+v, want live-1 only", first)
	}

	second, err := svc.GetLiveStreamsByOrg(context.Background(), hololive)
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
			body := mustMarshalStreamRawList(t, []StreamRaw{
				{
					ID:             "upcoming-1",
					Title:          "Upcoming stream",
					Status:         domain.StreamStatusUpcoming,
					ChannelID:      stringPtr("channel-upcoming"),
					StartScheduled: &future,
					Channel: &ChannelRaw{
						ID:   "channel-upcoming",
						Name: "Upcoming Member",
						Org:  &hololive,
					},
				},
				{
					ID:        "live-ignored",
					Title:     "Already live",
					Status:    domain.StreamStatusLive,
					ChannelID: stringPtr("channel-upcoming"),
					Channel: &ChannelRaw{
						ID:   "channel-upcoming",
						Name: "Upcoming Member",
						Org:  &hololive,
					},
				},
			})
			return body, nil
		},
	}

	svc := newServiceForFallbackTest(requester)

	first, err := svc.GetUpcomingStreamsByOrg(context.Background(), 24, hololive)
	if err != nil {
		t.Fatalf("GetUpcomingStreamsByOrg() error = %v", err)
	}
	if len(first) != 1 || first[0].ID != "upcoming-1" {
		t.Fatalf("GetUpcomingStreamsByOrg() = %+v, want upcoming-1 only", first)
	}

	second, err := svc.GetUpcomingStreamsByOrg(context.Background(), 24, hololive)
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

func mustMarshalStreamRawList(t *testing.T, streams []StreamRaw) []byte {
	t.Helper()

	body, err := sharedjson.Marshal(streams)
	if err != nil {
		t.Fatalf("marshal streams: %v", err)
	}
	return body
}

func stringPtr(value string) *string {
	return &value
}
