// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package cache

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/park285/hololive-bot/shared-go/pkg/json"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/internal/testredis"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type testPayload struct {
	Name string `json:"name"`
}

func newTestCacheService(t *testing.T) (*Service, *miniredis.Miniredis) {
	t.Helper()

	host, _, mini := testredis.StartMiniRedis(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	addr := net.JoinHostPort(host, mini.Port())

	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:  []string{addr},
		DisableCache: true,
		// 테스트에서 cluster slot 체크로 인한 multi-key 제약을 피하기 위해 단일 클라이언트를 강제합니다.
		ForceSingleClient: true,
	})
	if err != nil {
		mini.Close()
		t.Fatalf("failed to create valkey client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Do(ctx, client.B().Ping().Build()).Error(); err != nil {
		client.Close()
		mini.Close()
		t.Fatalf("failed to ping miniredis: %v", err)
	}
	service := &Service{client: client, logger: logger}

	t.Cleanup(func() {
		_ = service.Close()
		mini.Close()
	})

	return service, mini
}

func TestCacheServiceSetGetAndExists(t *testing.T) {
	service, mini := newTestCacheService(t)
	ctx := context.Background()

	value := testPayload{Name: "value"}
	if err := service.Set(ctx, "key", value, 0); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	var got testPayload
	if err := service.Get(ctx, "key", &got); err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.Name != "value" {
		t.Fatalf("unexpected value: %+v", got)
	}

	exists, err := service.Exists(ctx, "key")
	if err != nil {
		t.Fatalf("exists failed: %v", err)
	}
	if !exists {
		t.Fatalf("expected key to exist")
	}

	if err := service.Expire(ctx, "key", time.Second); err != nil {
		t.Fatalf("expire failed: %v", err)
	}
	mini.FastForward(2 * time.Second)

	exists, err = service.Exists(ctx, "key")
	if err != nil {
		t.Fatalf("exists after expire failed: %v", err)
	}
	if exists {
		t.Fatalf("expected key to expire")
	}
}

func TestCacheServiceMSetMGetDel(t *testing.T) {
	service, _ := newTestCacheService(t)
	ctx := context.Background()

	pairs := map[string]any{
		"a": testPayload{Name: "A"},
		"b": testPayload{Name: "B"},
	}
	if err := service.MSet(ctx, pairs, 0); err != nil {
		t.Fatalf("mset failed: %v", err)
	}

	values, err := service.MGet(ctx, []string{"a", "b"})
	if err != nil {
		t.Fatalf("mget failed: %v", err)
	}
	var decoded testPayload
	if err := json.Unmarshal([]byte(values["a"]), &decoded); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Name != "A" {
		t.Fatalf("unexpected decoded value: %+v", decoded)
	}

	count, err := service.DelMany(ctx, []string{"a", "b"})
	if err != nil {
		t.Fatalf("delmany failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 deletions, got %d", count)
	}
}

func TestCacheServiceDelManyChunksLargeRequests(t *testing.T) {
	service, mini := newTestCacheService(t)
	ctx := context.Background()

	keys := make([]string, 0, 1200)
	for i := range 1200 {
		key := fmt.Sprintf("bulk:%d", i)
		keys = append(keys, key)
		requireNoError(t, service.Set(ctx, key, testPayload{Name: key}, 0))
	}

	before := mini.CommandCount()
	count, err := service.DelMany(ctx, keys)
	requireNoError(t, err)
	if count != int64(len(keys)) {
		t.Fatalf("expected %d deletions, got %d", len(keys), count)
	}
	if got := mini.CommandCount() - before; got != 3 {
		t.Fatalf("expected 3 delete commands, got %d", got)
	}
}

func TestCacheServiceBatchHGetReturnsExistingFields(t *testing.T) {
	service, _ := newTestCacheService(t)
	ctx := context.Background()

	requireNoError(t, service.HMSet(ctx, "members", map[string]any{
		"UC_A": "Member A",
		"UC_B": "Member B",
	}))

	values, err := service.BatchHGet(ctx, "members", []string{"UC_A", "UC_MISSING", "UC_B"})
	requireNoError(t, err)

	if got := values["UC_A"]; got != "Member A" {
		t.Fatalf("values[UC_A] = %q, want Member A", got)
	}
	if _, ok := values["UC_MISSING"]; ok {
		t.Fatalf("values contains missing field: %#v", values)
	}
	if got := values["UC_B"]; got != "Member B" {
		t.Fatalf("values[UC_B] = %q, want Member B", got)
	}
}

func TestCacheServiceMSetFailsWithoutWritingOnMarshalError(t *testing.T) {
	service, _ := newTestCacheService(t)
	ctx := context.Background()

	err := service.MSet(ctx, map[string]any{
		"good": testPayload{Name: "A"},
		"bad":  make(chan int),
	}, time.Minute)
	if err == nil {
		t.Fatalf("expected marshal error from mset")
	}

	exists, existsErr := service.Exists(ctx, "good")
	if existsErr != nil {
		t.Fatalf("exists failed: %v", existsErr)
	}
	if exists {
		t.Fatalf("expected mset to avoid partial writes on marshal failure")
	}
}

func TestCacheServiceSetCeilsSubSecondTTL(t *testing.T) {
	service, mini := newTestCacheService(t)
	ctx := context.Background()

	requireNoError(t, service.Set(ctx, "ttl:set", testPayload{Name: "value"}, 500*time.Millisecond))

	exists, err := service.Exists(ctx, "ttl:set")
	requireNoError(t, err)
	if !exists {
		t.Fatalf("expected key to exist immediately after set")
	}

	mini.FastForward(900 * time.Millisecond)
	exists, err = service.Exists(ctx, "ttl:set")
	requireNoError(t, err)
	if !exists {
		t.Fatalf("key expired too early; expected ceil-rounded ttl")
	}

	mini.FastForward(200 * time.Millisecond)
	exists, err = service.Exists(ctx, "ttl:set")
	requireNoError(t, err)
	if exists {
		t.Fatalf("expected key to expire after rounded ttl elapsed")
	}
}

func TestCacheServiceMSetCeilsSubSecondTTL(t *testing.T) {
	service, mini := newTestCacheService(t)
	ctx := context.Background()

	requireNoError(t, service.MSet(ctx, map[string]any{
		"ttl:mset:a": testPayload{Name: "A"},
		"ttl:mset:b": testPayload{Name: "B"},
	}, 500*time.Millisecond))

	mini.FastForward(900 * time.Millisecond)
	for _, key := range []string{"ttl:mset:a", "ttl:mset:b"} {
		exists, err := service.Exists(ctx, key)
		requireNoError(t, err)
		if !exists {
			t.Fatalf("%s expired too early; expected ceil-rounded ttl", key)
		}
	}

	mini.FastForward(200 * time.Millisecond)
	for _, key := range []string{"ttl:mset:a", "ttl:mset:b"} {
		exists, err := service.Exists(ctx, key)
		requireNoError(t, err)
		if exists {
			t.Fatalf("expected %s to expire after rounded ttl elapsed", key)
		}
	}
}

func TestCacheServiceExpireCeilsSubSecondTTL(t *testing.T) {
	service, mini := newTestCacheService(t)
	ctx := context.Background()

	requireNoError(t, service.Set(ctx, "ttl:expire", testPayload{Name: "value"}, 0))
	requireNoError(t, service.Expire(ctx, "ttl:expire", 500*time.Millisecond))

	mini.FastForward(900 * time.Millisecond)
	exists, err := service.Exists(ctx, "ttl:expire")
	requireNoError(t, err)
	if !exists {
		t.Fatalf("key expired too early; expected ceil-rounded ttl")
	}

	mini.FastForward(200 * time.Millisecond)
	exists, err = service.Exists(ctx, "ttl:expire")
	requireNoError(t, err)
	if exists {
		t.Fatalf("expected key to expire after rounded ttl elapsed")
	}
}

func TestMemberCacheOperations(t *testing.T) {
	service, _ := newTestCacheService(t)
	ctx := context.Background()

	members := map[string]string{"member": "channel"}
	if err := service.InitializeMemberDatabase(ctx, members); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	channelID, err := service.GetMemberChannelID(ctx, "member")
	if err != nil {
		t.Fatalf("get member failed: %v", err)
	}
	if channelID != "channel" {
		t.Fatalf("unexpected channel id: %s", channelID)
	}

	all, err := service.GetAllMembers(ctx)
	if err != nil {
		t.Fatalf("get all failed: %v", err)
	}
	if all["member"] != "channel" {
		t.Fatalf("unexpected members: %+v", all)
	}

	if err := service.AddMember(ctx, "member2", "channel2"); err != nil {
		t.Fatalf("add member failed: %v", err)
	}
	channelID, err = service.GetMemberChannelID(ctx, "member2")
	if err != nil {
		t.Fatalf("get member2 failed: %v", err)
	}
	if channelID != "channel2" {
		t.Fatalf("unexpected channel id: %s", channelID)
	}
}

func TestStreamCacheOperations(t *testing.T) {
	service, _ := newTestCacheService(t)
	ctx := context.Background()

	streams := []*domain.Stream{{ID: "stream-1"}}
	service.SetStreams(ctx, "streams:key", streams, time.Minute)

	got, found := service.GetStreams(ctx, "streams:key")
	if !found || len(got) != 1 || got[0].ID != "stream-1" {
		t.Fatalf("unexpected streams: %+v, found=%v", got, found)
	}

	_, found = service.GetStreams(ctx, "streams:missing")
	if found {
		t.Fatalf("expected missing streams to return false")
	}
}

func TestSetNX(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*Service, context.Context)
		key        string
		value      string
		ttl        time.Duration
		wantResult bool
		wantErr    bool
	}{
		{
			name:       "key does not exist - should acquire lock",
			setup:      nil,
			key:        "lock:test",
			value:      "1",
			ttl:        time.Minute,
			wantResult: true,
			wantErr:    false,
		},
		{
			name: "key already exists - should fail",
			setup: func(service *Service, ctx context.Context) {
				_ = service.Set(ctx, "lock:existing", "already_set", time.Minute)
			},
			key:        "lock:existing",
			value:      "2",
			ttl:        time.Minute,
			wantResult: false,
			wantErr:    false,
		},
		{
			name:       "no TTL - should work",
			setup:      nil,
			key:        "lock:no_ttl",
			value:      "1",
			ttl:        0,
			wantResult: true,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, _ := newTestCacheService(t)
			ctx := context.Background()

			if tt.setup != nil {
				tt.setup(service, ctx)
			}

			got, err := service.SetNX(ctx, tt.key, tt.value, tt.ttl)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetNX() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantResult {
				t.Errorf("SetNX() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

func TestSetNXWithTTLExpiry(t *testing.T) {
	service, mini := newTestCacheService(t)
	ctx := context.Background()

	acquired, err := service.SetNX(ctx, "lock:expiry", "1", time.Second)
	if err != nil {
		t.Fatalf("SetNX failed: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock")
	}

	acquired, err = service.SetNX(ctx, "lock:expiry", "2", time.Second)
	if err != nil {
		t.Fatalf("second SetNX failed: %v", err)
	}
	if acquired {
		t.Fatal("expected second SetNX to fail while lock is held")
	}

	mini.FastForward(2 * time.Second)

	acquired, err = service.SetNX(ctx, "lock:expiry", "3", time.Second)
	if err != nil {
		t.Fatalf("third SetNX after expiry failed: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock after expiry")
	}
}

func TestSetNXCeilsSubSecondTTL(t *testing.T) {
	service, mini := newTestCacheService(t)
	ctx := context.Background()

	acquired, err := service.SetNX(ctx, "lock:ceil", "1", 500*time.Millisecond)
	requireNoError(t, err)
	if !acquired {
		t.Fatal("expected initial lock acquisition to succeed")
	}

	mini.FastForward(900 * time.Millisecond)
	acquired, err = service.SetNX(ctx, "lock:ceil", "2", 500*time.Millisecond)
	requireNoError(t, err)
	if acquired {
		t.Fatal("lock should still be held before rounded ttl elapses")
	}

	mini.FastForward(200 * time.Millisecond)
	acquired, err = service.SetNX(ctx, "lock:ceil", "3", 500*time.Millisecond)
	requireNoError(t, err)
	if !acquired {
		t.Fatal("expected lock acquisition after rounded ttl elapsed")
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestService_DoMulti(t *testing.T) {
	service, _ := newTestCacheService(t)
	ctx := context.Background()

	result := service.DoMulti(ctx)
	if result != nil {
		t.Errorf("DoMulti() with empty commands should return nil, got %v", result)
	}

	cmds := []valkey.Completed{
		service.Builder().Set().Key("multi1").Value("val1").Build(),
		service.Builder().Set().Key("multi2").Value("val2").Build(),
	}
	results := service.DoMulti(ctx, cmds...)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, res := range results {
		if err := res.Error(); err != nil {
			t.Errorf("command failed: %v", err)
		}
	}

	val1, _ := service.GetClient().Do(ctx, service.Builder().Get().Key("multi1").Build()).ToString()
	if val1 != "val1" {
		t.Errorf("expected val1, got %s", val1)
	}
}

func TestService_Builder(t *testing.T) {
	service, _ := newTestCacheService(t)
	builder := service.Builder()

	cmd := builder.Ping().Build()
	if cmd.Commands()[0] != "PING" {
		t.Errorf("expected PING command, got %v", cmd.Commands())
	}
}
