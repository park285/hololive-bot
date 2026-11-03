package sharedcache_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/sharedcache"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

func TestValkeyStoreRoundTripsBinaryValueWithTTL(t *testing.T) {
	t.Parallel()

	client, mini := testutil.NewTestCacheServiceWithMini(t.Context(), t)

	store := sharedcache.NewValkeyStore(client)
	if store == nil {
		t.Fatal("NewValkeyStore() returned nil")
	}

	value := []byte{0x89, 'P', 'N', 'G', 0x00, 0xff}

	if _, ok, err := store.Get(t.Context(), "bot:test:missing"); err != nil || ok {
		t.Fatalf("Get(missing) = %v, %v; want miss without error", ok, err)
	}

	if err := store.Set(t.Context(), "bot:test:value", value, time.Hour); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	got, ok, err := store.Get(t.Context(), "bot:test:value")
	if err != nil || !ok || !bytes.Equal(got, value) {
		t.Fatalf("Get() = %q, %v, %v; want stored binary value", got, ok, err)
	}

	if ttl := mini.TTL("bot:test:value"); ttl != time.Hour {
		t.Fatalf("TTL = %s, want 1h", ttl)
	}

	mini.SetError("valkey unavailable")

	if _, _, err := store.Get(t.Context(), "bot:test:value"); err == nil {
		t.Fatal("Get() during Valkey error = nil error, want error")
	}
}

func TestNewValkeyStoreWithoutClientIsNil(t *testing.T) {
	t.Parallel()

	if store := sharedcache.NewValkeyStore(nil); store != nil {
		t.Fatalf("NewValkeyStore(nil) = %#v, want nil", store)
	}
}
