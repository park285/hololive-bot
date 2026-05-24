package holodexprovider

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"sync"
	"testing"

	sharedjson "github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func TestSearchChannels_UsesPaginatedHololiveChannelListCache(t *testing.T) {
	t.Parallel()

	hololive := constants.HolodexAPIParams.OrgHololive
	stars := "HOLOSTARS"
	firstPage := make([]ChannelRaw, constants.HolodexAPIParams.DefaultChannelLimit)
	for i := range firstPage {
		firstPage[i] = ChannelRaw{
			ID:   fmt.Sprintf("channel-%02d", i),
			Name: fmt.Sprintf("Member %02d", i),
			Org:  &hololive,
		}
	}

	aquaEnglish := "Aqua"
	secondPage := []ChannelRaw{
		{
			ID:          "minato-aqua",
			Name:        "湊あくあ",
			EnglishName: &aquaEnglish,
			Org:         &hololive,
		},
		{
			ID:     "holostars-aqua",
			Name:   "Aqua HOLOSTARS",
			Org:    &hololive,
			Suborg: &stars,
		},
	}

	var (
		mu      sync.Mutex
		offsets []string
	)
	requester := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, params url.Values) ([]byte, error) {
			if method != "GET" {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}
			if path != "/channels" {
				return nil, fmt.Errorf("unexpected path: %s", path)
			}
			if got := params.Get("org"); got != hololive {
				return nil, fmt.Errorf("org = %s, want %s", got, hololive)
			}
			if got := params.Get("type"); got != constants.HolodexAPIParams.TypeVtuber {
				return nil, fmt.Errorf("type = %s, want %s", got, constants.HolodexAPIParams.TypeVtuber)
			}
			if got := params.Get("limit"); got != fmt.Sprintf("%d", constants.HolodexAPIParams.DefaultChannelLimit) {
				return nil, fmt.Errorf("limit = %s", got)
			}

			offset := params.Get("offset")
			mu.Lock()
			offsets = append(offsets, offset)
			mu.Unlock()

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

	service := newServiceForFallbackTest(requester)

	firstResult, err := service.SearchChannels(context.Background(), " aqua ")
	if err != nil {
		t.Fatalf("SearchChannels(aqua) error = %v", err)
	}
	if len(firstResult) != 1 || firstResult[0].ID != "minato-aqua" {
		t.Fatalf("SearchChannels(aqua) = %+v, want minato-aqua only", firstResult)
	}

	secondResult, err := service.SearchChannels(context.Background(), "member 01")
	if err != nil {
		t.Fatalf("SearchChannels(member 01) error = %v", err)
	}
	if len(secondResult) != 1 || secondResult[0].ID != "channel-01" {
		t.Fatalf("SearchChannels(member 01) = %+v, want channel-01 only", secondResult)
	}

	mu.Lock()
	gotOffsets := append([]string(nil), offsets...)
	mu.Unlock()
	wantOffsets := []string{"0", fmt.Sprintf("%d", constants.HolodexAPIParams.DefaultChannelLimit)}
	if !reflect.DeepEqual(gotOffsets, wantOffsets) {
		t.Fatalf("channel list offsets = %v, want %v", gotOffsets, wantOffsets)
	}
}

func mustMarshalChannelRawList(t *testing.T, channels []ChannelRaw) []byte {
	t.Helper()

	body, err := sharedjson.Marshal(channels)
	if err != nil {
		t.Fatalf("marshal channels: %v", err)
	}
	return body
}
