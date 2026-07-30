package holodexprovider

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestGetChannelsLiveStatus_FillsIndieOrgWhenUsersLiveOmitsIt(t *testing.T) {
	t.Parallel()

	channelID := constants.IndieChannelIDs[0]
	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, params url.Values) ([]byte, error) {
			if method != "GET" {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}
			if path != "/users/live" {
				return nil, fmt.Errorf("unexpected path: %s", path)
			}
			if got := params.Get("channels"); got != channelID {
				return nil, fmt.Errorf("channels = %q, want %q", got, channelID)
			}
			return fmt.Appendf(nil, `[
				{
					"id":"video-1",
					"title":"indie live",
					"channel_id":"%s",
					"status":"live",
					"channel":{"id":"%s","name":"Sakuna Ch. 結城さくな"}
				}
			]`, channelID, channelID), nil
		},
	}

	service := newServiceForFallbackTest(mockReq)

	streams, err := service.GetChannelsLiveStatus(context.Background(), []string{channelID})
	if err != nil {
		t.Fatalf("GetChannelsLiveStatus() error = %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("len(streams) = %d, want 1", len(streams))
	}
	if streams[0].Channel == nil || streams[0].Channel.Org == nil {
		t.Fatalf("stream channel/org not hydrated: %+v", streams[0].Channel)
	}
	if got := *streams[0].Channel.Org; got != constants.HolodexAPIParams.OrgIndie {
		t.Fatalf("channel org = %q, want %q", got, constants.HolodexAPIParams.OrgIndie)
	}
}

func TestGetChannelsLiveStatus_FillsIndieChannelWhenUsersLiveOmitsChannelObject(t *testing.T) {
	t.Parallel()

	channelID := constants.IndieChannelIDs[0]
	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, params url.Values) ([]byte, error) {
			if method != "GET" {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}
			if path != "/users/live" {
				return nil, fmt.Errorf("unexpected path: %s", path)
			}
			if got := params.Get("channels"); got != channelID {
				return nil, fmt.Errorf("channels = %q, want %q", got, channelID)
			}
			return fmt.Appendf(nil, `[
				{
					"id":"video-1",
					"title":"indie live no channel object",
					"channel_id":"%s",
					"status":"live"
				}
			]`, channelID), nil
		},
	}

	service := newServiceForFallbackTest(mockReq)

	streams, err := service.GetChannelsLiveStatus(context.Background(), []string{channelID})
	if err != nil {
		t.Fatalf("GetChannelsLiveStatus() error = %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("len(streams) = %d, want 1", len(streams))
	}
	if streams[0].Channel == nil {
		t.Fatalf("stream channel not hydrated")
	}
	if got := streams[0].Channel.ID; got != channelID {
		t.Fatalf("channel id = %q, want %q", got, channelID)
	}
	if streams[0].Channel.Org == nil {
		t.Fatalf("channel org not hydrated")
	}
	if got := *streams[0].Channel.Org; got != constants.HolodexAPIParams.OrgIndie {
		t.Fatalf("channel org = %q, want %q", got, constants.HolodexAPIParams.OrgIndie)
	}
}

func TestGetChannelsLiveStatus_DoesNotHydrateNonIndieMissingOrg(t *testing.T) {
	t.Parallel()

	channelID := "UC_NON_INDIE"
	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, params url.Values) ([]byte, error) {
			if method != "GET" {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}
			if path != "/users/live" {
				return nil, fmt.Errorf("unexpected path: %s", path)
			}
			if got := params.Get("channels"); got != channelID {
				return nil, fmt.Errorf("channels = %q, want %q", got, channelID)
			}
			return []byte(`[
				{
					"id":"video-2",
					"title":"unknown live",
					"channel_id":"UC_NON_INDIE",
					"status":"live",
					"channel":{"id":"UC_NON_INDIE","name":"Unknown"}
				}
			]`), nil
		},
	}

	service := newServiceForFallbackTest(mockReq)

	streams, err := service.GetChannelsLiveStatus(context.Background(), []string{channelID})
	if err != nil {
		t.Fatalf("GetChannelsLiveStatus() error = %v", err)
	}
	if len(streams) != 0 {
		t.Fatalf("len(streams) = %d, want 0", len(streams))
	}
}

func TestGetChannelsLiveStatus_AppliesIndieOrgOverride(t *testing.T) {
	t.Parallel()

	const channelID = "UCt30jJgChL8qeT9VPadidSw" // しぐれうい (Shigure Ui)
	override, ok := constants.IndieChannelOrgOverrides[channelID]
	if !ok {
		t.Fatalf("channel %q missing from IndieChannelOrgOverrides", channelID)
	}

	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, params url.Values) ([]byte, error) {
			if method != "GET" {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}
			if path != "/users/live" {
				return nil, fmt.Errorf("unexpected path: %s", path)
			}
			if got := params.Get("channels"); got != channelID {
				return nil, fmt.Errorf("channels = %q, want %q", got, channelID)
			}
			return fmt.Appendf(nil, `[
				{
					"id":"video-ui",
					"title":"ui live",
					"channel_id":"%s",
					"status":"live",
					"channel":{"id":"%s","name":"しぐれうい","org":"Independents"}
				}
			]`, channelID, channelID), nil
		},
	}

	service := newServiceForFallbackTest(mockReq)

	streams, err := service.GetChannelsLiveStatus(context.Background(), []string{channelID})
	if err != nil {
		t.Fatalf("GetChannelsLiveStatus() error = %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("len(streams) = %d, want 1", len(streams))
	}
	if streams[0].Channel == nil || streams[0].Channel.Org == nil {
		t.Fatalf("stream channel/org not hydrated: %+v", streams[0].Channel)
	}
	if got := *streams[0].Channel.Org; got != override {
		t.Fatalf("channel org = %q, want %q (override forced over Holodex value)", got, override)
	}
}

func TestCacheManager_GetChannelsLiveStatusStreams_OrderIndependentKeys(t *testing.T) {
	t.Parallel()

	cacheManager := NewCacheManager(
		newInMemoryCacheClient(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	expected := []*domain.Stream{
		{ID: "video-1"},
	}

	cacheManager.SetChannelsLiveStatusStreams(context.Background(), []string{"UC_B", "UC_A"}, expected, time.Minute)

	got, found := cacheManager.GetChannelsLiveStatusStreams(context.Background(), []string{"UC_A", "UC_B"})
	if !found {
		t.Fatal("GetChannelsLiveStatusStreams() found = false, want true")
	}
	if len(got) != len(expected) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(expected))
	}
	if got[0] == nil {
		t.Fatal("got[0] = nil, want stream")
	}
	if got[0].ID != expected[0].ID {
		t.Fatalf("got[0].ID = %q, want %q", got[0].ID, expected[0].ID)
	}
}
