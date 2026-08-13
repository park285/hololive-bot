package acl

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func newReloadTestService(store *fakeACLStore, cacheClient *cachemocks.Client) *Service {
	return &Service{
		store:          store,
		cache:          cacheClient,
		logger:         slog.New(slog.DiscardHandler),
		enabled:        true,
		mode:           ACLModeWhitelist,
		whitelistRooms: make(map[string]struct{}),
		blacklistRooms: make(map[string]struct{}),
	}
}

func newReloadTestCache() *cachemocks.Client {
	return &cachemocks.Client{
		SetFunc:  func(context.Context, string, any, time.Duration) error { return nil },
		DelFunc:  func(context.Context, string) error { return nil },
		SAddFunc: func(_ context.Context, _ string, members []string) (int64, error) { return int64(len(members)), nil },
		SRemFunc: func(_ context.Context, _ string, members []string) (int64, error) { return int64(len(members)), nil },
	}
}

func TestReloadPropagatesAnotherInstanceRoomAddition(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()
	store.settings[dbKeyEnabled] = "true"
	store.settings[dbKeyMode] = "whitelist"

	adminSide := newReloadTestService(store, newReloadTestCache())
	botSide := newReloadTestService(store, newReloadTestCache())

	if botSide.IsRoomAllowed("", "room-new") {
		t.Fatal("room must start disallowed on the bot-side instance")
	}

	added, err := adminSide.AddRoom(t.Context(), "room-new")
	if err != nil {
		t.Fatalf("AddRoom error: %v", err)
	}
	if !added {
		t.Fatal("AddRoom should report the room as added")
	}

	if botSide.IsRoomAllowed("", "room-new") {
		t.Fatal("bot-side instance must not observe the change before reload")
	}

	if err := botSide.Reload(t.Context()); err != nil {
		t.Fatalf("Reload error: %v", err)
	}

	if !botSide.IsRoomAllowed("", "room-new") {
		t.Fatal("bot-side instance must observe the room after reload")
	}
}

func TestReloadPropagatesRoomRemovalAndSettings(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()
	store.settings[dbKeyEnabled] = "true"
	store.settings[dbKeyMode] = "whitelist"
	store.rooms[roomKey{roomID: "room-old", listType: listTypeWhitelist}] = struct{}{}

	botSide := newReloadTestService(store, newReloadTestCache())
	if err := botSide.Reload(t.Context()); err != nil {
		t.Fatalf("initial Reload error: %v", err)
	}
	if !botSide.IsRoomAllowed("", "room-old") {
		t.Fatal("seeded room must be allowed after the first reload")
	}

	delete(store.rooms, roomKey{roomID: "room-old", listType: listTypeWhitelist})
	store.settings[dbKeyMode] = "blacklist"
	store.settings[dbKeyEnabled] = "false"

	if err := botSide.Reload(t.Context()); err != nil {
		t.Fatalf("Reload error: %v", err)
	}

	enabled, mode, rooms := botSide.GetACLStatus()
	if enabled {
		t.Fatal("reload must pick up enabled=false")
	}
	if mode != ACLModeBlacklist {
		t.Fatalf("reload must pick up mode=blacklist, got %s", mode)
	}
	if len(rooms) != 0 {
		t.Fatalf("reload must drop the removed room, got %v", rooms)
	}
}

// Reload는 통지를 받은 복제본이 호출한다. 여기서 Valkey에 되쓰면 관리 plane이 방금 쓴
// 상태를 자기 스냅샷으로 덮어써 다른 복제본에 낡은 목록을 퍼뜨린다.
func TestReloadDoesNotWriteToCache(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()
	store.settings[dbKeyEnabled] = "true"
	store.settings[dbKeyMode] = "whitelist"
	store.rooms[roomKey{roomID: "room-a", listType: listTypeWhitelist}] = struct{}{}

	var writes atomic.Int32
	cacheClient := &cachemocks.Client{
		SetFunc: func(context.Context, string, any, time.Duration) error { writes.Add(1); return nil },
		DelFunc: func(context.Context, string) error { writes.Add(1); return nil },
		SAddFunc: func(_ context.Context, _ string, members []string) (int64, error) {
			writes.Add(1)
			return int64(len(members)), nil
		},
		SRemFunc: func(_ context.Context, _ string, members []string) (int64, error) {
			writes.Add(1)
			return int64(len(members)), nil
		},
	}

	service := newReloadTestService(store, cacheClient)
	if err := service.Reload(t.Context()); err != nil {
		t.Fatalf("Reload error: %v", err)
	}

	if got := writes.Load(); got != 0 {
		t.Fatalf("Reload must not write to cache, got %d writes", got)
	}
}

func TestReloadKeepsCurrentSettingsWhenRowsMissing(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()
	store.rooms[roomKey{roomID: "room-a", listType: listTypeWhitelist}] = struct{}{}

	service := newReloadTestService(store, newReloadTestCache())
	service.enabled = false
	service.mode = ACLModeBlacklist

	if err := service.Reload(t.Context()); err != nil {
		t.Fatalf("Reload error: %v", err)
	}

	enabled, mode, _ := service.GetACLStatus()
	if enabled {
		t.Fatal("missing enabled row must keep the current value")
	}
	if mode != ACLModeBlacklist {
		t.Fatalf("missing mode row must keep the current value, got %s", mode)
	}
}

func TestReloadRejectsUnparsableMode(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()
	store.settings[dbKeyEnabled] = "true"
	store.settings[dbKeyMode] = "not-a-mode"

	service := newReloadTestService(store, newReloadTestCache())
	service.whitelistRooms["room-keep"] = struct{}{}

	if err := service.Reload(t.Context()); err == nil {
		t.Fatal("Reload must fail on an unparsable mode")
	}

	if !service.IsRoomAllowed("", "room-keep") {
		t.Fatal("failed reload must leave the previous room set intact")
	}
}

func TestReloadRejectsUnparsableEnabledSetting(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()
	store.settings[dbKeyEnabled] = "not-a-bool"
	store.settings[dbKeyMode] = "whitelist"

	service := newReloadTestService(store, newReloadTestCache())
	service.enabled = false
	service.whitelistRooms["room-keep"] = struct{}{}

	if err := service.Reload(t.Context()); err == nil {
		t.Fatal("Reload must fail on an unparsable enabled setting")
	}

	enabled, _, _ := service.GetACLStatus()
	if enabled {
		t.Fatal("failed reload must leave ACL disabled state intact")
	}
}
