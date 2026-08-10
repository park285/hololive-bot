package dispatchoutbox

import (
	"fmt"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestAssignSendUnitsIsDeterministicAndBounded(t *testing.T) {
	first := make([]deliveryInsert, 11)
	second := make([]deliveryInsert, 11)
	for i := range first {
		first[i] = deliveryInsert{RoomID: "room-1", DedupeKey: fmt.Sprintf("delivery-%02d", i), DispatchGroupKey: "group-1"}
		second[10-i] = first[i]
	}

	assignSendUnits(first)
	assignSendUnits(second)

	byDedupe := make(map[string]string, len(first))
	unitCounts := make(map[string]int)
	for i := range first {
		byDedupe[first[i].DedupeKey] = first[i].SendUnitKey
		unitCounts[first[i].SendUnitKey]++
		if first[i].ClientRequestID == "" {
			t.Fatalf("delivery %s has empty client request id", first[i].DedupeKey)
		}
	}
	for i := range second {
		if got, want := second[i].SendUnitKey, byDedupe[second[i].DedupeKey]; got != want {
			t.Fatalf("send unit for %s = %q, want %q", second[i].DedupeKey, got, want)
		}
	}
	if len(unitCounts) != 2 {
		t.Fatalf("send unit count = %d, want 2", len(unitCounts))
	}
	for unitKey, count := range unitCounts {
		if count > maxDeliveriesPerSendUnit {
			t.Fatalf("send unit %s contains %d deliveries, max %d", unitKey, count, maxDeliveriesPerSendUnit)
		}
	}
}

func TestBuildDispatchGroupKeySeparatesRoomsAndKeepsEquivalentEventsTogether(t *testing.T) {
	first := &domain.AlarmQueueEnvelope{Notification: domain.AlarmNotification{RoomID: "room-1", AlarmType: domain.AlarmTypeLive, MinutesUntil: 5}}
	second := &domain.AlarmQueueEnvelope{Notification: domain.AlarmNotification{RoomID: "room-1", AlarmType: domain.AlarmTypeLive, MinutesUntil: 5}}
	otherRoom := &domain.AlarmQueueEnvelope{Notification: domain.AlarmNotification{RoomID: "room-2", AlarmType: domain.AlarmTypeLive, MinutesUntil: 5}}

	if BuildDispatchGroupKeyFromEnvelope(first) != BuildDispatchGroupKeyFromEnvelope(second) {
		t.Fatal("equivalent delivery events must share a dispatch group")
	}
	if BuildDispatchGroupKeyFromEnvelope(first) == BuildDispatchGroupKeyFromEnvelope(otherRoom) {
		t.Fatal("different rooms must not share a dispatch group")
	}
}
