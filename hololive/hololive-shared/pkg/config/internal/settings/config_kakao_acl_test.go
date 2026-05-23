package settings

import (
	"testing"
)

func TestKakaoConfig_SetACLEnabled(t *testing.T) {
	t.Parallel()

	c := &KakaoConfig{
		ACLEnabled: false,
		ACLMode:    "whitelist",
		Rooms:      []string{"room1"},
	}

	if c.ACLEnabled {
		t.Fatal("ACLEnabled should start as false")
	}

	c.SetACLEnabled(true)
	enabled, _, _ := c.SnapshotACL()
	if !enabled {
		t.Fatal("ACLEnabled should be true after SetACLEnabled(true)")
	}

	c.SetACLEnabled(false)
	enabled, _, _ = c.SnapshotACL()
	if enabled {
		t.Fatal("ACLEnabled should be false after SetACLEnabled(false)")
	}
}

func TestKakaoConfig_IsRoomAllowed_EmptyChatIDDenied(t *testing.T) {
	t.Parallel()

	c := &KakaoConfig{
		ACLEnabled: true,
		ACLMode:    "whitelist",
		Rooms:      []string{"room1"},
	}

	if c.IsRoomAllowed("name", "") {
		t.Fatal("empty chatID should be denied when ACL is enabled")
	}
}

func TestKakaoConfig_IsRoomAllowed_BlacklistMode(t *testing.T) {
	t.Parallel()

	c := &KakaoConfig{
		ACLEnabled: true,
		ACLMode:    "blacklist",
		Rooms:      []string{"blocked-room"},
	}

	if c.IsRoomAllowed("name", "blocked-room") {
		t.Fatal("blocked room should be denied in blacklist mode")
	}

	if !c.IsRoomAllowed("name", "other-room") {
		t.Fatal("non-blocked room should be allowed in blacklist mode")
	}
}

func TestKakaoConfig_AddRoom_EmptyRoomRejected(t *testing.T) {
	t.Parallel()

	c := &KakaoConfig{Rooms: []string{}}
	if c.AddRoom("") {
		t.Fatal("AddRoom(\"\") should return false")
	}
	if c.AddRoom("  ") {
		t.Fatal("AddRoom(\"  \") should return false")
	}
}

func TestKakaoConfig_RemoveRoom_EmptyRoomRejected(t *testing.T) {
	t.Parallel()

	c := &KakaoConfig{Rooms: []string{"a"}}
	if c.RemoveRoom("") {
		t.Fatal("RemoveRoom(\"\") should return false")
	}
}

func TestKakaoConfig_RemoveRoom_NotFound(t *testing.T) {
	t.Parallel()

	c := &KakaoConfig{Rooms: []string{"a", "b"}}
	if c.RemoveRoom("c") {
		t.Fatal("RemoveRoom(\"c\") should return false when not in list")
	}
	if len(c.Rooms) != 2 {
		t.Fatalf("Rooms length = %d after RemoveRoom miss, want 2", len(c.Rooms))
	}
}
