package holodexprovider

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"testing"

	streammapping "github.com/kapu/hololive-shared/internal/service/holodex/provider/streammapping"
	"github.com/kapu/hololive-shared/pkg/constants"
)

func TestSearchChannels_UsesPaginatedHololiveChannelListCache(t *testing.T) {
	t.Parallel()

	hololive := constants.HolodexAPIParams.OrgHololive
	firstPage, secondPage := paginatedChannelListPages(hololive, "HOLOSTARS")

	var recorder channelListOffsetRecorder

	requester := newPaginatedChannelListRequester(t, hololive, firstPage, secondPage, &recorder)
	service := newServiceForFallbackTest(requester)

	firstResult, err := service.SearchChannels(t.Context(), " aqua ")
	if err != nil {
		t.Fatalf("SearchChannels(aqua) error = %v", err)
	}

	if len(firstResult) != 1 || firstResult[0].ID != "minato-aqua" {
		t.Fatalf("SearchChannels(aqua) = %+v, want minato-aqua only", firstResult)
	}

	secondResult, err := service.SearchChannels(t.Context(), "member 01")
	if err != nil {
		t.Fatalf("SearchChannels(member 01) error = %v", err)
	}

	if len(secondResult) != 1 || secondResult[0].ID != "channel-01" {
		t.Fatalf("SearchChannels(member 01) = %+v, want channel-01 only", secondResult)
	}

	gotOffsets := recorder.snapshot()

	wantOffsets := []string{"0", fmt.Sprintf("%d", constants.HolodexAPIParams.DefaultChannelLimit)}
	if !reflect.DeepEqual(gotOffsets, wantOffsets) {
		t.Fatalf("channel list offsets = %v, want %v", gotOffsets, wantOffsets)
	}
}

type channelListOffsetRecorder struct {
	mu      sync.Mutex
	offsets []string
}

func (r *channelListOffsetRecorder) record(offset string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.offsets = append(r.offsets, offset)
}

func (r *channelListOffsetRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.offsets...)
}

func paginatedChannelListPages(org, suborg string) (firstPage, secondPage []streammapping.ChannelRaw) {
	firstPage = make([]streammapping.ChannelRaw, constants.HolodexAPIParams.DefaultChannelLimit)

	for i := range firstPage {
		firstPage[i] = streammapping.ChannelRaw{
			ID:   fmt.Sprintf("channel-%02d", i),
			Name: fmt.Sprintf("Member %02d", i),
			Org:  &org,
		}
	}

	aquaEnglish := "Aqua"

	secondPage = []streammapping.ChannelRaw{
		{
			ID:          "minato-aqua",
			Name:        "湊あくあ",
			EnglishName: &aquaEnglish,
			Org:         &org,
		},
		{
			ID:     "holostars-aqua",
			Name:   "Aqua HOLOSTARS",
			Org:    &org,
			Suborg: &suborg,
		},
	}

	return firstPage, secondPage
}

func verifyChannelListRequest(method, path, org string, params url.Values) error {
	if method != http.MethodGet {
		return fmt.Errorf("unexpected method: %s", method)
	}

	if path != "/channels" {
		return fmt.Errorf("unexpected path: %s", path)
	}

	if got := params.Get("org"); got != org {
		return fmt.Errorf("org = %s, want %s", got, org)
	}

	if got := params.Get("type"); got != constants.HolodexAPIParams.TypeVtuber {
		return fmt.Errorf("type = %s, want %s", got, constants.HolodexAPIParams.TypeVtuber)
	}

	if got := params.Get("limit"); got != fmt.Sprintf("%d", constants.HolodexAPIParams.DefaultChannelLimit) {
		return fmt.Errorf("limit = %s", got)
	}

	return nil
}

func newPaginatedChannelListRequester(
	t *testing.T,
	org string,
	firstPage, secondPage []streammapping.ChannelRaw,
	recorder *channelListOffsetRecorder,
) *MockRequester {
	t.Helper()

	return &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, params url.Values) ([]byte, error) {
			if err := verifyChannelListRequest(method, path, org, params); err != nil {
				return nil, fmt.Errorf("verify channel list request: %w", err)
			}

			offset := params.Get("offset")
			recorder.record(offset)

			switch offset {
			case "", "0":
				return mustMarshalChannelRawList(t, firstPage), nil
			case fmt.Sprintf("%d", constants.HolodexAPIParams.DefaultChannelLimit):
				return mustMarshalChannelRawList(t, secondPage), nil
			default:
				return nil, fmt.Errorf("unexpected offset: %s", offset)
			}
		},
	}
}

func mustMarshalChannelRawList(t *testing.T, channels []streammapping.ChannelRaw) []byte {
	t.Helper()

	body, err := jsonv2.Marshal(channels)
	if err != nil {
		t.Fatalf("marshal channels: %v", err)
	}

	return body
}
