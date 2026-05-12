package dispatchoutbox

import (
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

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

	if got, want := BuildEventKey(room1), BuildEventKey(room2); got != want {
		t.Fatalf("BuildEventKey differs by room: %q != %q", got, want)
	}
	if got, want := BuildDedupeKey(room1), BuildDedupeKey(room2); got == want {
		t.Fatalf("BuildDedupeKey should include room, got same key %q", got)
	}
}

func TestMarshalEventPayloadOmitsRoomSpecificFields(t *testing.T) {
	payload, err := marshalEventPayload(domain.AlarmQueueEnvelope{
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

	event1, delivery1, err := buildLedgerRows(domain.AlarmQueueEnvelope{
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
	}, StatusPending)
	if err != nil {
		t.Fatalf("buildLedgerRows room1 error = %v", err)
	}

	event2, delivery2, err := buildLedgerRows(domain.AlarmQueueEnvelope{
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
	}, StatusPending)
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

	event1, _, err := buildLedgerRows(first, StatusPending)
	if err != nil {
		t.Fatalf("buildLedgerRows first error = %v", err)
	}
	event2, _, err := buildLedgerRows(second, StatusPending)
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
