package alarm

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestLookupChannelSubscribersByTypeUsesTypedKey(t *testing.T) {
	t.Parallel()

	var lookedUpKey string
	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		lookedUpKey = key
		return []string{"room-a", "room-b"}, nil
	}

	got, err := LookupChannelSubscribersByType(t.Context(), cacheSvc, "UC_shorts", domain.AlarmTypeShorts)
	if err != nil {
		t.Fatalf("LookupChannelSubscribersByType() error = %v", err)
	}

	wantKey := sharedalarmkeys.BuildChannelSubscriberKey("UC_shorts", domain.AlarmTypeShorts)
	if lookedUpKey != wantKey {
		t.Fatalf("lookup key = %q, want %q", lookedUpKey, wantKey)
	}
	if len(got) != 2 || got[0] != "room-a" || got[1] != "room-b" {
		t.Fatalf("LookupChannelSubscribersByType() = %#v", got)
	}
}
