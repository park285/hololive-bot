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
			name:    "room alarm key",
			key:     "alarm:" + plainRoom,
			prefix:  "alarm:",
			hasRoom: plainRoom,
		},
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
		"alarm:registry",
		"alarm:channel_registry",
		"alarm:channel_registry:version",
		"alarm:subscriber_cache_empty",
		"alarm:member_names",
		"alarm:room_names",
		"alarm:user_names",
		"alarm:chzzk_channels",
		"alarm:chzzk_channels_empty",
		"alarm:twitch_logins",
		"alarm:twitch_channel_logins_empty",
		"alarm:dispatch:queue",
		"alarm:dispatch:wakeup:guard",
		"alarm:next_stream:" + channelID,
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
		"majorevent:lock:weekly:2026-W31",
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

	if got, want := RedactCacheKey("alarm:"+canonicalRoom), "alarm:"+canonicalRoom; got != want {
		t.Errorf("RedactCacheKey(%q) = %q, want %q", "alarm:"+canonicalRoom, got, want)
	}

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
		"alarm:",
	} {
		got := RedactCacheKey(key)
		if strings.Contains(got, plainRoom) {
			t.Errorf("RedactCacheKey(%q) = %q, room plaintext survived a truncated key", key, got)
		}
	}
}

func TestRedactCacheFieldOnlyTouchesIdentifierKeyedHashes(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"alarm:room_names", "alarm:user_names", "membernews:room_names"} {
		got := RedactCacheField(key, plainRoom)
		if strings.Contains(got, plainRoom) {
			t.Errorf("RedactCacheField(%q, room) = %q, plaintext survived", key, got)
		}

		if !strings.HasPrefix(got, PseudonymPrefix) {
			t.Errorf("RedactCacheField(%q, room) = %q, want a pseudonym", key, got)
		}

		if got := RedactCacheField(key, canonicalRoom); got != canonicalRoom {
			t.Errorf("RedactCacheField(%q, canonical) = %q, want %q", key, got, canonicalRoom)
		}
	}

	for _, key := range []string{"hololive:members", "alarm:member_names", "alarm:twitch_logins"} {
		if got := RedactCacheField(key, "Gawr Gura:Hololive EN"); got != "Gawr Gura:Hololive EN" {
			t.Errorf("RedactCacheField(%q, member) = %q, want the field unchanged", key, got)
		}
	}
}

func TestCacheAttrsKeepTheirWireNames(t *testing.T) {
	t.Parallel()

	keyAttr := CacheKeyAttr("alarm:" + plainRoom)
	if keyAttr.Key != KeyCacheKey || keyAttr.Value.String() != RedactCacheKey("alarm:"+plainRoom) {
		t.Errorf("CacheKeyAttr = %v, want key %q carrying the redacted value", keyAttr, KeyCacheKey)
	}

	fieldAttr := CacheFieldAttr("alarm:room_names", plainRoom)
	if fieldAttr.Key != KeyCacheField || strings.Contains(fieldAttr.Value.String(), plainRoom) {
		t.Errorf("CacheFieldAttr = %v, want key %q carrying the redacted value", fieldAttr, KeyCacheField)
	}
}

func TestRedactedKeysCorrelateWithRoomIDAttr(t *testing.T) {
	t.Parallel()

	want := RoomIDAttr(plainRoom).Value.String()
	got := strings.TrimPrefix(RedactCacheKey("alarm:"+plainRoom), "alarm:")

	if got != want {
		t.Errorf("cache key token = %q, room_id token = %q; the two must correlate within a process", got, want)
	}
}
