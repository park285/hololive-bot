package holodex

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	"github.com/kapu/hololive-shared/pkg/constants"
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
			return []byte(fmt.Sprintf(`[
				{
					"id":"video-1",
					"title":"indie live",
					"channel_id":"%s",
					"status":"live",
					"channel":{"id":"%s","name":"Sakuna Ch. 結城さくな"}
				}
			]`, channelID, channelID)), nil
		},
	}

	svc := newServiceForFallbackTest(mockReq)

	streams, err := svc.GetChannelsLiveStatus(context.Background(), []string{channelID})
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
			return []byte(fmt.Sprintf(`[
				{
					"id":"video-1",
					"title":"indie live no channel object",
					"channel_id":"%s",
					"status":"live"
				}
			]`, channelID)), nil
		},
	}

	svc := newServiceForFallbackTest(mockReq)

	streams, err := svc.GetChannelsLiveStatus(context.Background(), []string{channelID})
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

	svc := newServiceForFallbackTest(mockReq)

	streams, err := svc.GetChannelsLiveStatus(context.Background(), []string{channelID})
	if err != nil {
		t.Fatalf("GetChannelsLiveStatus() error = %v", err)
	}
	if len(streams) != 0 {
		t.Fatalf("len(streams) = %d, want 0", len(streams))
	}
}
