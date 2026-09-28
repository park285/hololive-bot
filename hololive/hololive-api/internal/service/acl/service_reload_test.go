package acl

import (
	"log/slog"
	"testing"
)

func newReloadTestService(store *fakeACLStore) *Service {
	return &Service{
		store:          store,
		logger:         slog.New(slog.DiscardHandler),
		enabled:        true,
		mode:           ACLModeWhitelist,
		whitelistRooms: make(map[string]struct{}),
		blacklistRooms: make(map[string]struct{}),
	}
}

func TestReloadPropagatesAnotherInstanceRoomAddition(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()

	store.settings[dbKeyEnabled] = testDBEnabledTrue
	store.settings[dbKeyMode] = string(ACLModeWhitelist)

	adminSide := newReloadTestService(store)
	botSide := newReloadTestService(store)

	if botSide.IsRoomAllowed("3001") {
		t.Fatal("room must start disallowed on the bot-side instance")
	}

	added, err := adminSide.AddRoom(t.Context(), "3001")
	if err != nil {
		t.Fatalf("AddRoom error: %v", err)
	}

	if !added {
		t.Fatal("AddRoom should report the room as added")
	}

	if botSide.IsRoomAllowed("3001") {
		t.Fatal("bot-side instance must not observe the change before reload")
	}

	if err := botSide.Reload(t.Context()); err != nil {
		t.Fatalf("Reload error: %v", err)
	}

	if !botSide.IsRoomAllowed("3001") {
		t.Fatal("bot-side instance must observe the room after reload")
	}
}

func TestReloadPropagatesRoomRemovalAndSettings(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()

	store.settings[dbKeyEnabled] = testDBEnabledTrue
	store.settings[dbKeyMode] = string(ACLModeWhitelist)
	store.rooms[roomKey{roomID: "room-old", listType: listTypeWhitelist}] = struct{}{}

	botSide := newReloadTestService(store)
	if err := botSide.Reload(t.Context()); err != nil {
		t.Fatalf("initial Reload error: %v", err)
	}

	if !botSide.IsRoomAllowed("room-old") {
		t.Fatal("seeded room must be allowed after the first reload")
	}

	delete(store.rooms, roomKey{roomID: "room-old", listType: listTypeWhitelist})

	store.settings[dbKeyMode] = "blacklist"
	store.settings[dbKeyEnabled] = testDBEnabledFalse

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

func TestReloadKeepsCurrentSettingsWhenRowsMissing(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()

	store.rooms[roomKey{roomID: testRoomA, listType: listTypeWhitelist}] = struct{}{}

	service := newReloadTestService(store)

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

	store.settings[dbKeyEnabled] = testDBEnabledTrue
	store.settings[dbKeyMode] = "not-a-mode"

	service := newReloadTestService(store)

	service.whitelistRooms["room-keep"] = struct{}{}

	if err := service.Reload(t.Context()); err == nil {
		t.Fatal("Reload must fail on an unparsable mode")
	}

	if !service.IsRoomAllowed("room-keep") {
		t.Fatal("failed reload must leave the previous room set intact")
	}
}

func TestReloadRejectsUnparsableEnabledSetting(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()

	store.settings[dbKeyEnabled] = "not-a-bool"
	store.settings[dbKeyMode] = string(ACLModeWhitelist)

	service := newReloadTestService(store)

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
