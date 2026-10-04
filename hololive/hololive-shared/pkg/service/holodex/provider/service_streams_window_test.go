package holodexprovider

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kapu/hololive-shared/internal/service/holodex/provider/streammapping"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func upcomingWindowPayload(t *testing.T, org string, now time.Time) []byte {
	t.Helper()

	streams := make([]streammapping.StreamRaw, 0, 2)

	for _, hours := range []int{2, 48} {
		streams = append(streams, streammapping.StreamRaw{
			ID: fmt.Sprintf("upcoming-%dh", hours), Status: domain.StreamStatusUpcoming,
			ChannelID: new("window-channel"), StartScheduled: new(now.Add(time.Duration(hours) * time.Hour).Format(time.RFC3339)),
			Channel: &streammapping.ChannelRaw{ID: "window-channel", Name: "Member", Org: &org},
		})
	}

	return mustMarshalStreamRawList(t, streams)
}

func TestGetUpcomingStreamsByOrgEnforcesRequestedWindow(t *testing.T) {
	t.Parallel()

	for _, org := range []string{constants.HolodexAPIParams.OrgIndie, constants.HolodexAPIParams.OrgAll, constants.HolodexAPIParams.OrgHololive} {
		t.Run(org, func(t *testing.T) {
			t.Parallel()

			now := time.Now()

			var requests atomic.Int32

			requester := &MockRequester{DoRequestFunc: func(_ context.Context, method, path string, params url.Values) ([]byte, error) {
				requests.Add(1)

				if path == usersLivePath {
					if method != http.MethodGet || params.Get("channels") == "" {
						return nil, fmt.Errorf("unexpected indie request: %s %s %v", method, path, params)
					}

					return upcomingWindowPayload(t, constants.HolodexAPIParams.OrgIndie, now), nil
				}

				if err := verifyLiveEndpointRequest(method, path, params.Get("org"), constants.HolodexAPIParams.StatusUpcoming, params); err != nil {
					return nil, err
				}

				if params.Get("max_upcoming_hours") != "24" {
					return nil, fmt.Errorf("unexpected max_upcoming_hours: %s", params.Get("max_upcoming_hours"))
				}

				if org == constants.HolodexAPIParams.OrgAll {
					return []byte("[]"), nil
				}

				return upcomingWindowPayload(t, org, now), nil
			}}
			service := newServiceForFallbackTest(requester)

			assertCachedOrgStreams(t, "GetUpcomingStreamsByOrg", "upcoming-2h", func() ([]*domain.Stream, error) {
				return service.GetUpcomingStreamsByOrg(t.Context(), 24, org)
			})

			if got, want := int(requests.Load()), len(streamTargetOrgs(org)); got != want {
				t.Fatalf("source requests = %d, want %d with second result cached", got, want)
			}
		})
	}
}

func TestGetUpcomingStreamsByOrgFiltersExistingCachedWindow(t *testing.T) {
	t.Parallel()

	service := newServiceForFallbackTest(&MockRequester{DoRequestFunc: func(context.Context, string, string, url.Values) ([]byte, error) {
		t.Error("cached stream query must not request the source")

		return []byte("[]"), nil
	}})
	now := time.Now()
	org := constants.HolodexAPIParams.OrgIndie
	service.cacheManager.SetUpcomingStreamsByOrg(t.Context(), org, 24, []*domain.Stream{
		{ID: "within", Status: domain.StreamStatusUpcoming, StartScheduled: new(now.Add(2 * time.Hour))},
		{ID: "beyond", Status: domain.StreamStatusUpcoming, StartScheduled: new(now.Add(48 * time.Hour))},
		{ID: "delayed", Status: domain.StreamStatusUpcoming, StartScheduled: new(now.Add(-time.Hour))},
	})

	assertCachedOrgStreams(t, "cached upcoming window", "within", func() ([]*domain.Stream, error) {
		return service.GetUpcomingStreamsByOrg(t.Context(), 24, org)
	})
}

func TestGetUpcomingStreamsByOrgExcludesStreamsDelayedDuringFetch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		payload := upcomingWindowPayload(t, constants.HolodexAPIParams.OrgIndie, time.Now())
		requester := &MockRequester{DoRequestFunc: func(context.Context, string, string, url.Values) ([]byte, error) {
			time.Sleep(3 * time.Hour)

			return payload, nil
		}}
		service := newServiceForFallbackTest(requester)

		streams, err := service.GetUpcomingStreamsByOrg(t.Context(), 24, constants.HolodexAPIParams.OrgIndie)
		if err != nil {
			t.Fatalf("GetUpcomingStreamsByOrg() error = %v", err)
		}

		if len(streams) != 0 {
			t.Fatalf("streams = %v, want no delayed or out-of-window streams", streams)
		}
	})
}

func TestGetUpcomingStreamsByOrgOfficialFallbackPreservesRequestedHours(t *testing.T) {
	t.Parallel()

	for _, hours := range []int{24, 240, 0, -1} {
		t.Run(fmt.Sprintf("hours=%d", hours), func(t *testing.T) {
			t.Parallel()

			withinHours := 2

			if hours != 24 {
				withinHours = 200
			}

			now := time.Now().In(time.FixedZone("JST", 9*60*60))
			outside := ""

			if hours > 0 {
				outside = fmt.Sprintf(`,{"datetime":%q,"url":"https://www.youtube.com/watch?v=beyond","name":"Member"}`,
					now.Add(time.Duration(hours+24)*time.Hour).Format("2006/01/02 15:04:05"))
			}

			var officialRequests, primaryRequests atomic.Int32

			officialServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				officialRequests.Add(1)
				writer.Header().Set("Content-Type", "application/json")
				writeOfficialScheduleResponse(t, writer, `{"dateGroupList":[{"videoList":[{
					"datetime":%q,"url":"https://www.youtube.com/watch?v=within","name":"Member"
				}%s]}]}`, now.Add(time.Duration(withinHours)*time.Hour).Format("2006/01/02 15:04:05"), outside)
			}))
			t.Cleanup(officialServer.Close)

			requester := &MockRequester{DoRequestFunc: func(_ context.Context, _, _ string, params url.Values) ([]byte, error) {
				primaryRequests.Add(1)

				if got, want := params.Get("max_upcoming_hours"), fmt.Sprint(min(hours, constants.HolodexAPIParams.MaxUpcomingHours)); got != want {
					t.Errorf("primary max_upcoming_hours = %s, want %s", got, want)
				}

				return nil, unavailableHolodexError("upcoming_streams")
			}}
			scraper := newScraperServiceForTest(officialServer.Client(), slog.New(slog.DiscardHandler), officialServer.URL)
			service := newServiceForFallbackTestWithScraper(requester, scraper)

			assertCachedOrgStreams(t, "official upcoming window", "within", func() ([]*domain.Stream, error) {
				return service.GetUpcomingStreamsByOrg(t.Context(), hours, constants.HolodexAPIParams.OrgHololive)
			})

			if primaryRequests.Load() != 1 || officialRequests.Load() != 1 {
				t.Fatalf("primary=%d official=%d, want one source call each", primaryRequests.Load(), officialRequests.Load())
			}
		})
	}
}
