package privacylog

import (
	"strings"
	"testing"
)

const (
	plainRoom      = "상대방닉네임 님과의 대화"
	colonRoom      = "공지: 상대방닉네임 님과의 대화"
	canonicalRoom  = "18446744073709551615"
	streamID       = "dQw4w9WgXcQ"
	channelID      = "UC1DCedRgGHBdm81E1llLhOQ"
	scheduleUnix   = "1785499200"
	titleFinger    = "aaf2320646108059"
	notifyCategory = "10m"
)

type redactCacheKeyCase struct {
	name    string
	key     string
	prefix  string
	tail    string
	hasRoom string
}

func redactCacheKeyCases() []redactCacheKeyCase {
	return []redactCacheKeyCase{
		{
			name:    "notify claim key",
			key:     strings.Join([]string{"notified:claim:" + plainRoom, streamID, scheduleUnix, notifyCategory}, ":"),
			prefix:  "notified:claim:",
			tail:    streamID + ":" + scheduleUnix + ":" + notifyCategory,
			hasRoom: plainRoom,
		},
		{
			name:    "logical event claim key",
			key:     strings.Join([]string{"notified:claim:event:" + plainRoom, channelID, scheduleUnix, titleFinger, notifyCategory}, ":"),
			prefix:  "notified:claim:event:",
			tail:    channelID + ":" + scheduleUnix + ":" + titleFinger + ":" + notifyCategory,
			hasRoom: plainRoom,
		},
		{
			name:    "upcoming event key",
			key:     strings.Join([]string{"notified:upcoming:event:" + plainRoom, channelID, scheduleUnix, titleFinger}, ":"),
			prefix:  "notified:upcoming:event:",
			tail:    channelID + ":" + scheduleUnix + ":" + titleFinger,
			hasRoom: plainRoom,
		},
		{
			name:    "room schedule transition key",
			key:     strings.Join([]string{"notified:schedule:transition:room:" + plainRoom, streamID, scheduleUnix, "1785502800"}, ":"),
			prefix:  "notified:schedule:transition:room:",
			tail:    streamID + ":" + scheduleUnix + ":1785502800",
			hasRoom: plainRoom,
		},
		{
			name:    "logical schedule index key",
			key:     strings.Join([]string{"notified:schedule:index:" + plainRoom, channelID, titleFinger}, ":"),
			prefix:  "notified:schedule:index:",
			tail:    channelID + ":" + titleFinger,
			hasRoom: plainRoom,
		},
		{
			name:    "logical schedule transition key",
			key:     strings.Join([]string{"notified:schedule:transition:event:" + plainRoom, channelID, titleFinger, scheduleUnix, "1785502800"}, ":"),
			prefix:  "notified:schedule:transition:event:",
			tail:    channelID + ":" + titleFinger + ":" + scheduleUnix + ":1785502800",
			hasRoom: plainRoom,
		},
		{
			name:    "room title containing the segment separator",
			key:     strings.Join([]string{"notified:claim:" + colonRoom, streamID, scheduleUnix, notifyCategory}, ":"),
			prefix:  "notified:claim:",
			tail:    streamID + ":" + scheduleUnix + ":" + notifyCategory,
			hasRoom: colonRoom,
		},
	}
}

func TestRedactCacheKeyRemovesRoomAndKeepsDiagnosticTail(t *testing.T) {
	t.Parallel()

	cases := redactCacheKeyCases()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := RedactCacheKey(tc.key)
			if strings.Contains(got, tc.hasRoom) {
				t.Fatalf("RedactCacheKey(%q) = %q, room plaintext survived", tc.key, got)
			}

			if !strings.HasPrefix(got, tc.prefix+PseudonymPrefix) {
				t.Errorf("RedactCacheKey(%q) = %q, want prefix %q followed by a pseudonym", tc.key, got, tc.prefix)
			}

			if tc.tail != "" && !strings.HasSuffix(got, ":"+tc.tail) {
				t.Errorf("RedactCacheKey(%q) = %q, want diagnostic tail %q preserved", tc.key, got, tc.tail)
			}
		})
	}
}

func TestRedactCacheKeyLeavesIdentifierFreeKeysIntact(t *testing.T) {
	t.Parallel()

	keys := []string{
		"alarm:channel_registry",
		"alarm:subscriber_cache_empty",
		"alarm:member_names",
		"alarm:dispatch:wakeup",
		"alarm:dispatch:wakeup:guard",
		"alarm:channel_subscribers:" + channelID,
		"alarm:channel_subscribers:COMMUNITY:" + channelID,
		"alarm:channel_subscribers_empty:SHORTS:" + channelID,
		"notified:" + streamID,
		"notified:chzzk:live:" + channelID,
		"notified:integrated:" + streamID,
		"notified:schedule:transition:" + streamID + ":" + scheduleUnix + ":1785502800",
		"hololive:members",
		"holodex:api",
		"member:name:" + channelID,
		"lock:ingestion:runtime",
		"{alarm:twitch_logins}:tmp:1",
		"",
	}

	for _, key := range keys {
		if got := RedactCacheKey(key); got != key {
			t.Errorf("RedactCacheKey(%q) = %q, want the key unchanged", key, got)
		}
	}
}

func TestRedactCacheKeyKeepsCanonicalRoomIDReadable(t *testing.T) {
	t.Parallel()

	claim := strings.Join([]string{"notified:claim:" + canonicalRoom, streamID, scheduleUnix, notifyCategory}, ":")
	if got := RedactCacheKey(claim); got != claim {
		t.Errorf("RedactCacheKey(%q) = %q, want the canonical key unchanged", claim, got)
	}
}

func TestRedactCacheKeyFailsClosedOnTruncatedKeys(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		"notified:claim:" + plainRoom,
		"notified:claim:" + plainRoom + ":" + streamID,
		"notified:upcoming:event:" + plainRoom + ":" + channelID,
	} {
		got := RedactCacheKey(key)
		if strings.Contains(got, plainRoom) {
			t.Errorf("RedactCacheKey(%q) = %q, room plaintext survived a truncated key", key, got)
		}
	}
}

func TestCacheAttrsKeepTheirWireNames(t *testing.T) {
	t.Parallel()

	claim := strings.Join([]string{"notified:claim:" + plainRoom, streamID, scheduleUnix, notifyCategory}, ":")

	keyAttr := CacheKeyAttr(claim)
	if keyAttr.Key != KeyCacheKey || keyAttr.Value.String() != RedactCacheKey(claim) {
		t.Errorf("CacheKeyAttr = %v, want key %q carrying the redacted value", keyAttr, KeyCacheKey)
	}

	fieldAttr := CacheFieldAttr(channelID)
	if fieldAttr.Key != KeyCacheField || fieldAttr.Value.String() != channelID {
		t.Errorf("CacheFieldAttr = %v, want key %q carrying the field", fieldAttr, KeyCacheField)
	}
}

func TestRedactedKeysCorrelateWithRoomIDAttr(t *testing.T) {
	t.Parallel()

	want := RoomIDAttr(plainRoom).Value.String()
	tail := ":" + strings.Join([]string{streamID, scheduleUnix, notifyCategory}, ":")
	claim := "notified:claim:" + plainRoom + tail
	got := strings.TrimSuffix(strings.TrimPrefix(RedactCacheKey(claim), "notified:claim:"), tail)

	if got != want {
		t.Errorf("cache key token = %q, room_id token = %q; the two must correlate within a process", got, want)
	}
}
