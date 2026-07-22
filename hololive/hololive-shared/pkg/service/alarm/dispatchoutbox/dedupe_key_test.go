package dispatchoutbox

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBuildEventKeyUsesHashFormWhenOverLimit(t *testing.T) {
	input := DedupeInput{
		ChannelID:    strings.Repeat("c", 600),
		AlarmType:    domain.AlarmTypeLive,
		StreamID:     strings.Repeat("s", 600),
		MinutesUntil: 10,
		Category:     "claim:event",
	}

	got := BuildEventKey(&input)
	if len(got) > eventKeyMaxLength {
		t.Fatalf("BuildEventKey length = %d, want <= %d", len(got), eventKeyMaxLength)
	}
	if !strings.HasPrefix(got, "event_sha:") {
		t.Fatalf("BuildEventKey = %q, want event_sha: prefix when over limit", got)
	}

	raw := buildRawEventKey(&input, "")
	sum := sha256.Sum256([]byte(raw))
	want := fmt.Sprintf("event_sha:%s", hex.EncodeToString(sum[:]))
	if got != want {
		t.Fatalf("BuildEventKey = %q, want %q", got, want)
	}
}

func TestBuildEventKeyShortKeyUnchanged(t *testing.T) {
	input := DedupeInput{
		ChannelID:    "channel-1",
		AlarmType:    domain.AlarmTypeLive,
		StreamID:     "stream-1",
		MinutesUntil: 10,
		Category:     "claim:event",
	}
	got := BuildEventKey(&input)
	if got != buildRawEventKey(&input, "") {
		t.Fatalf("short key altered: %q != raw %q", got, buildRawEventKey(&input, ""))
	}
	if strings.HasPrefix(got, "event_sha:") {
		t.Fatalf("short key unexpectedly hashed: %q", got)
	}
}

func TestEventKeyIgnoresRoomAndDeliveryDedupeIncludesRoom(t *testing.T) {
	base := DedupeInput{
		ChannelID:    "channel-1",
		AlarmType:    domain.AlarmTypeLive,
		StreamID:     "stream-1",
		MinutesUntil: 10,
		Category:     "claim:event",
	}
	room1 := base
	room1.RoomID = "room-1"
	room2 := base
	room2.RoomID = "room-2"

	if got, want := BuildEventKey(&room1), BuildEventKey(&room2); got != want {
		t.Fatalf("BuildEventKey differs by room: %q != %q", got, want)
	}
	if got, want := BuildDedupeKey(&room1), BuildDedupeKey(&room2); got == want {
		t.Fatalf("BuildDedupeKey should include room, got same key %q", got)
	}
}

func TestBuildDedupeKeyUsesCanonicalLivePrefix(t *testing.T) {
	input := DedupeInput{
		RoomID:       "room-1",
		ChannelID:    "channel-1",
		AlarmType:    domain.AlarmTypeLive,
		StreamID:     "stream-1",
		MinutesUntil: 10,
		Category:     "claim:event",
	}

	got := BuildDedupeKey(&input)
	if !strings.Contains(got, ":live:") {
		t.Fatalf("BuildDedupeKey = %q, want canonical live event segment", got)
	}
	if strings.Contains(got, "legacy-live") || strings.Contains(got, "legacy-schedule") {
		t.Fatalf("BuildDedupeKey = %q, must not produce legacy event prefix", got)
	}
}

func TestMarshalEventPayloadOmitsRoomSpecificFields(t *testing.T) {
	payload, err := marshalEventPayload(&domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType:    domain.AlarmTypeLive,
			RoomID:       "room-1",
			Users:        []string{"alice"},
			Channel:      &domain.Channel{ID: "channel-1"},
			Stream:       &domain.Stream{ID: "stream-1", ChannelID: "channel-1"},
			MinutesUntil: 10,
		},
		ClaimKeys: []string{"room-specific-claim"},
		Version:   1,
	})
	if err != nil {
		t.Fatalf("marshalEventPayload() error = %v", err)
	}
	raw := string(payload)
	for _, forbidden := range []string{"room_id", "room-1", "users", "alice", "claim_keys", "room-specific-claim", "enqueued_at"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("event payload contains room-specific field/value %q: %s", forbidden, raw)
		}
	}
}

func TestBuildLedgerRowsEventKeyIgnoresRoomSpecificClaimKeys(t *testing.T) {
	start := time.Now().UTC().Truncate(time.Minute)
	stream := &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start}
	channel := &domain.Channel{ID: "channel-1"}

	firstEnvelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType:    domain.AlarmTypeLive,
			RoomID:       "room-1",
			Channel:      channel,
			Stream:       stream,
			MinutesUntil: 10,
			Users:        []string{"alice"},
		},
		ClaimKeys: []string{"claim:room-1:stream-1"},
		Version:   1,
	}
	event1, delivery1, err := buildLedgerRows(&firstEnvelope, StatusPending)
	if err != nil {
		t.Fatalf("buildLedgerRows room1 error = %v", err)
	}

	secondEnvelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType:    domain.AlarmTypeLive,
			RoomID:       "room-2",
			Channel:      channel,
			Stream:       stream,
			MinutesUntil: 10,
			Users:        []string{"bob"},
		},
		ClaimKeys: []string{"claim:room-2:stream-1"},
		Version:   1,
	}
	event2, delivery2, err := buildLedgerRows(&secondEnvelope, StatusPending)
	if err != nil {
		t.Fatalf("buildLedgerRows room2 error = %v", err)
	}

	if event1.EventKey != event2.EventKey {
		t.Fatalf("event keys differ by room-specific claim key: %q != %q", event1.EventKey, event2.EventKey)
	}
	if delivery1.DedupeKey == delivery2.DedupeKey {
		t.Fatalf("delivery dedupe keys should differ by room, got %q", delivery1.DedupeKey)
	}
}

func TestBuildLedgerRowsEventPayloadHashIgnoresEnqueuedAt(t *testing.T) {
	start := time.Now().UTC().Truncate(time.Minute)
	stream := &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start}
	channel := &domain.Channel{ID: "channel-1"}

	base := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType:    domain.AlarmTypeLive,
			RoomID:       "room-1",
			Channel:      channel,
			Stream:       stream,
			MinutesUntil: 10,
		},
		Version: 1,
	}
	first := base
	first.EnqueuedAt = "2026-05-12T00:00:00Z"
	second := base
	second.EnqueuedAt = "2026-05-12T00:00:05Z"

	event1, _, err := buildLedgerRows(&first, StatusPending)
	if err != nil {
		t.Fatalf("buildLedgerRows first error = %v", err)
	}
	event2, _, err := buildLedgerRows(&second, StatusPending)
	if err != nil {
		t.Fatalf("buildLedgerRows second error = %v", err)
	}

	if event1.EventKey != event2.EventKey {
		t.Fatalf("event keys differ: %q != %q", event1.EventKey, event2.EventKey)
	}
	if event1.PayloadHash != event2.PayloadHash {
		t.Fatalf("payload hashes differ by enqueued_at: %q != %q", event1.PayloadHash, event2.PayloadHash)
	}
}

func TestBuildLedgerRowsYouTubeOutboxUsesSourceIdentity(t *testing.T) {
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeCommunity,
			RoomID:    "room-1",
		},
		SourceKind: domain.AlarmDispatchSourceKindYouTubeOutbox,
		YouTubeOutbox: &domain.YouTubeOutboxDispatchPayload{
			OutboxIDs:         []int64{10, 11},
			Kind:              domain.OutboxKindCommunityPost,
			AlarmType:         domain.AlarmTypeCommunity,
			ChannelID:         "UC_test",
			RenderTemplateKey: domain.TemplateKeyOutboxCommunityGroup,
			Items: []domain.YouTubeOutboxItem{
				{OutboxID: 11, ContentID: "post-b", Payload: `{"post_id":"post-b","content_text":"b"}`},
				{OutboxID: 10, ContentID: "post-a", Payload: `{"post_id":"post-a","content_text":"a"}`},
			},
		},
		ClaimKeys: []string{
			"youtube-notification:COMMUNITY_POST:post-a:room-1",
			"youtube-notification:COMMUNITY_POST:post-b:room-1",
		},
		Version: 1,
	}

	event, delivery, err := buildLedgerRows(&envelope, StatusPending)
	if err != nil {
		t.Fatalf("buildLedgerRows() error = %v", err)
	}

	wantEventKey := "youtube-outbox:COMMUNITY_POST:" + envelope.YouTubeOutbox.Identity()
	if event.EventKey != wantEventKey {
		t.Fatalf("event key = %q, want %q", event.EventKey, wantEventKey)
	}
	if event.AlarmType != domain.AlarmTypeCommunity {
		t.Fatalf("event alarm type = %q, want %q", event.AlarmType, domain.AlarmTypeCommunity)
	}
	if event.ChannelID != "UC_test" {
		t.Fatalf("event channel id = %q, want UC_test", event.ChannelID)
	}
	if !strings.Contains(delivery.DedupeKey, "room-1") {
		t.Fatalf("delivery dedupe key = %q, want room-specific key", delivery.DedupeKey)
	}
	if err := validateEventPayloadRoomAgnostic(event.Payload); err != nil {
		t.Fatalf("validateEventPayloadRoomAgnostic() error = %v", err)
	}
}

func TestBuildEventKeyUsesDistinctCanonicalYouTubeIdentities(t *testing.T) {
	identity := func(contentIDs ...string) string {
		payload := &domain.YouTubeOutboxDispatchPayload{
			Kind:      domain.OutboxKindNewVideo,
			AlarmType: domain.AlarmTypeLive,
			ChannelID: "UC_test",
		}
		for _, contentID := range contentIDs {
			payload.Items = append(payload.Items, domain.YouTubeOutboxItem{ContentID: contentID, Payload: `{}`})
		}
		return payload.Identity()
	}
	first := BuildEventKey(&DedupeInput{
		SourceKind:       domain.AlarmDispatchSourceKindYouTubeOutbox,
		SourceOutboxKind: domain.OutboxKindNewVideo,
		SourceIdentity:   identity("a,b", "c"),
	})
	second := BuildEventKey(&DedupeInput{
		SourceKind:       domain.AlarmDispatchSourceKindYouTubeOutbox,
		SourceOutboxKind: domain.OutboxKindNewVideo,
		SourceIdentity:   identity("a", "b,c"),
	})
	if first == second {
		t.Fatalf("BuildEventKey collision = %q", first)
	}
	for _, key := range []string{first, second} {
		if len(key) > eventKeyMaxLength {
			t.Fatalf("BuildEventKey length = %d, want <= %d", len(key), eventKeyMaxLength)
		}
	}
}

func TestBuildEventKeyStaysBoundedForMaximumYouTubeIdentitySet(t *testing.T) {
	payload := &domain.YouTubeOutboxDispatchPayload{
		Kind:      domain.OutboxKindNewVideo,
		AlarmType: domain.AlarmTypeLive,
		ChannelID: "UC_test",
		Items:     make([]domain.YouTubeOutboxItem, 1000),
	}
	for i := range payload.Items {
		payload.Items[i] = domain.YouTubeOutboxItem{
			ContentID: fmt.Sprintf("%0500d-%04d", i, i),
			Payload:   `{}`,
		}
	}
	if err := payload.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	key := BuildEventKey(&DedupeInput{
		SourceKind:       domain.AlarmDispatchSourceKindYouTubeOutbox,
		SourceOutboxKind: payload.Kind,
		SourceIdentity:   payload.Identity(),
	})
	if len(key) > eventKeyMaxLength {
		t.Fatalf("BuildEventKey length = %d, want <= %d", len(key), eventKeyMaxLength)
	}
	if !strings.Contains(key, ":sha256:") {
		t.Fatalf("BuildEventKey = %q, want canonical hashed identity", key)
	}
}

func TestBuildEventKeyBoundsLegacyRawYouTubeSourceIdentity(t *testing.T) {
	key := BuildEventKey(&DedupeInput{
		SourceKind:       domain.AlarmDispatchSourceKindYouTubeOutbox,
		SourceOutboxKind: domain.OutboxKindNewVideo,
		SourceIdentity:   strings.Repeat("legacy,", 1000),
	})
	if len(key) > eventKeyMaxLength {
		t.Fatalf("BuildEventKey length = %d, want <= %d", len(key), eventKeyMaxLength)
	}
	if !strings.Contains(key, ":sha256:") {
		t.Fatalf("BuildEventKey = %q, want bounded source hash", key)
	}
}
