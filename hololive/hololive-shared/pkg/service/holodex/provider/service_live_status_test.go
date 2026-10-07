package holodexprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func TestGetChannelsLiveStatus_FillsIndieOrgWhenUsersLiveOmitsIt(t *testing.T) {
	t.Parallel()

	channelID := constants.IndieChannelIDs[0]
	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, params url.Values) ([]byte, error) {
			if method != http.MethodGet {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}

			if path != usersLivePath {
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

	streams, err := service.GetChannelsLiveStatus(t.Context(), []string{channelID})
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
			if method != http.MethodGet {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}

			if path != usersLivePath {
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

	streams, err := service.GetChannelsLiveStatus(t.Context(), []string{channelID})
	if err != nil {
		t.Fatalf("GetChannelsLiveStatus() error = %v", err)
	}

	if len(streams) != 1 {
		t.Fatalf("len(streams) = %d, want 1", len(streams))
	}

	if streams[0].Channel == nil {
		t.Fatal("stream channel not hydrated")
	}

	if got := streams[0].Channel.ID; got != channelID {
		t.Fatalf("channel id = %q, want %q", got, channelID)
	}

	if streams[0].Channel.Org == nil {
		t.Fatal("channel org not hydrated")
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
			if method != http.MethodGet {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}

			if path != usersLivePath {
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

	streams, err := service.GetChannelsLiveStatus(t.Context(), []string{channelID})
	if err != nil {
		t.Fatalf("GetChannelsLiveStatus() error = %v", err)
	}

	if len(streams) != 0 {
		t.Fatalf("len(streams) = %d, want 0", len(streams))
	}
}

func TestGetChannelsLiveStatus_AppliesIndieOrgOverride(t *testing.T) {
	t.Parallel()

	const channelID = "UCt30jJgChL8qeT9VPadidSw" // 시구레 우이(しぐれうい)

	override, ok := constants.IndieChannelOrgOverrides[channelID]

	if !ok {
		t.Fatalf("channel %q missing from IndieChannelOrgOverrides", channelID)
	}

	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, params url.Values) ([]byte, error) {
			if method != http.MethodGet {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}

			if path != usersLivePath {
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

	streams, err := service.GetChannelsLiveStatus(t.Context(), []string{channelID})
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

// live status는 요청 채널 집합별로 캐시하지 않으므로 같은 채널을 연달아 조회해도 매번 upstream 상태를 반영한다.
func TestGetChannelsLiveStatus_ReflectsUpstreamChangeOnConsecutiveCalls(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, _, path string, _ url.Values) ([]byte, error) {
			if path != usersLivePath {
				return nil, fmt.Errorf("unexpected path: %s", path)
			}

			if calls.Add(1) == 1 {
				return []byte(`[]`), nil
			}

			return fmt.Appendf(nil, `[{"id":"%s","title":"live","channel_id":"%s","status":"live",
				"channel":{"id":"%s","name":"member","org":"%s"}}]`,
				testVideoID, testChannelID, testChannelID, constants.HolodexAPIParams.OrgHololive), nil
		},
	}

	service := newServiceForFallbackTest(mockReq)

	first, err := service.GetChannelsLiveStatus(t.Context(), []string{testChannelID})
	if err != nil {
		t.Fatalf("first GetChannelsLiveStatus() error = %v", err)
	}

	if len(first) != 0 {
		t.Fatalf("first GetChannelsLiveStatus() len = %d, want 0", len(first))
	}

	second, err := service.GetChannelsLiveStatus(t.Context(), []string{testChannelID})
	if err != nil {
		t.Fatalf("second GetChannelsLiveStatus() error = %v", err)
	}

	if len(second) != 1 || second[0].ID != testVideoID {
		t.Fatalf("second GetChannelsLiveStatus() = %+v, want live stream %q", second, testVideoID)
	}

	if got := calls.Load(); got != 2 {
		t.Fatalf("upstream calls = %d, want 2", got)
	}
}
