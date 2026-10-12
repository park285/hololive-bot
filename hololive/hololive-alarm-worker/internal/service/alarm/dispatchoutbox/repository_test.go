package dispatchoutbox

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBuildLedgerRows_DedupeKeyDoesNotDependOnClaimKeys(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.May, 12, 3, 0, 0, 0, time.UTC)
	base := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    testRoomID,
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start},
		},
		Version: 1,
	}
	first := base

	first.ClaimKeys = []string{"claim:old"}

	second := base

	second.ClaimKeys = []string{"claim:new"}

	_, firstDelivery, err := buildLedgerRows(&first)
	if err != nil {
		t.Fatalf("buildLedgerRows(first) error = %v", err)
	}

	_, secondDelivery, err := buildLedgerRows(&second)
	if err != nil {
		t.Fatalf("buildLedgerRows(second) error = %v", err)
	}

	if firstDelivery.DedupeKey != secondDelivery.DedupeKey {
		t.Fatalf("dedupe key depends on claim key: %q != %q", firstDelivery.DedupeKey, secondDelivery.DedupeKey)
	}

	if !strings.HasPrefix(firstDelivery.DedupeKey, "v2:room:room-1:event:") {
		t.Fatalf("dedupe key = %q, want v2 room/event prefix", firstDelivery.DedupeKey)
	}

	if len(firstDelivery.ClaimKeys) != 1 || firstDelivery.ClaimKeys[0] != "claim:old" {
		t.Fatalf("claim keys were not preserved as metadata: %v", firstDelivery.ClaimKeys)
	}
}

// 알림 종류가 비어 있으면 Live로 추정하지 않고 적재를 거절한다. 다른 종류의 알림을 Live 문구로 보내지 않기 위해서다.
func TestBuildLedgerRows_RejectsEmptyAlarmType(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.May, 12, 3, 0, 0, 0, time.UTC)
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			RoomID:  testRoomID,
			Channel: &domain.Channel{ID: testChannelID},
			Stream:  &domain.Stream{ID: testStreamID, ChannelID: testChannelID, StartScheduled: &start},
		},
		Version: 1,
	}

	if _, _, err := buildLedgerRows(&envelope); err == nil {
		t.Fatal("buildLedgerRows() accepted an envelope without alarm type")
	}
}

func TestMarshalEventPayload_RemainsRoomAgnostic(t *testing.T) {
	t.Parallel()

	payload, err := marshalEventPayload(&domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    testRoomID,
			Users:     []string{testUserName},
			Channel:   &domain.Channel{ID: testChannelID},
			Stream:    &domain.Stream{ID: testStreamID, ChannelID: testChannelID},
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
		Notification map[string]jsontext.Value `json:"notification"`
	}

	if err := jsonv2.Unmarshal(payload, &decoded); err != nil {
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
