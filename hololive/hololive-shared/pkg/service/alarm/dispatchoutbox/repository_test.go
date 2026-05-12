package dispatchoutbox

import (
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

func TestBuildLedgerRows_DedupeKeyDoesNotDependOnClaimKeys(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	base := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-1",
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-1", ChannelID: "channel-1", StartScheduled: &start},
		},
		Version: 1,
	}
	first := base
	first.ClaimKeys = []string{"claim:old"}
	second := base
	second.ClaimKeys = []string{"claim:new"}

	_, firstDelivery, err := buildLedgerRows(first, StatusPending)
	if err != nil {
		t.Fatalf("buildLedgerRows(first) error = %v", err)
	}
	_, secondDelivery, err := buildLedgerRows(second, StatusPending)
	if err != nil {
		t.Fatalf("buildLedgerRows(second) error = %v", err)
	}

	if firstDelivery.DedupeKey != secondDelivery.DedupeKey {
		t.Fatalf("dedupe key depends on claim key: %q != %q", firstDelivery.DedupeKey, secondDelivery.DedupeKey)
	}
	if firstDelivery.LegacyDedupeKey == secondDelivery.LegacyDedupeKey {
		t.Fatalf("legacy dedupe key should preserve claim-key compatibility difference, got %q", firstDelivery.LegacyDedupeKey)
	}
	if !strings.HasPrefix(firstDelivery.DedupeKey, "v2:room:room-1:event:") {
		t.Fatalf("dedupe key = %q, want v2 room/event prefix", firstDelivery.DedupeKey)
	}
	if len(firstDelivery.ClaimKeys) != 1 || firstDelivery.ClaimKeys[0] != "claim:old" {
		t.Fatalf("claim keys were not preserved as metadata: %v", firstDelivery.ClaimKeys)
	}
}

func TestMarshalEventPayload_RemainsRoomAgnostic(t *testing.T) {
	t.Parallel()

	payload, err := marshalEventPayload(domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-1",
			Users:     []string{"alice"},
			Channel:   &domain.Channel{ID: "channel-1"},
			Stream:    &domain.Stream{ID: "stream-1", ChannelID: "channel-1"},
		},
		Version: 1,
	})
	if err != nil {
		t.Fatalf("marshalEventPayload() error = %v", err)
	}
	if err := validateEventPayloadRoomAgnostic(payload); err != nil {
		t.Fatalf("validateEventPayloadRoomAgnostic() error = %v", err)
	}

	var decoded struct {
		Notification map[string]json.RawMessage `json:"notification"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	for _, key := range []string{"room_id", "roomId", "room", "users"} {
		if _, ok := decoded.Notification[key]; ok {
			t.Fatalf("payload notification contains delivery-specific key %q: %s", key, string(payload))
		}
	}
}

func TestValidateEventPayloadRoomAgnosticRejectsNestedDeliveryFields(t *testing.T) {
	t.Parallel()

	err := validateEventPayloadRoomAgnostic([]byte(`{"notification":{"room_id":"room-1","users":["alice"]},"version":1}`))
	if err == nil {
		t.Fatal("validateEventPayloadRoomAgnostic() error = nil, want nested delivery field error")
	}
}
