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
		first[i] = deliveryInsert{RoomID: testRoomID, DedupeKey: fmt.Sprintf("delivery-%02d", i), DispatchGroupKey: "group-1"}
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
	first := &domain.AlarmQueueEnvelope{Notification: domain.AlarmNotification{RoomID: testRoomID, AlarmType: domain.AlarmTypeLive, MinutesUntil: 5}}
	second := &domain.AlarmQueueEnvelope{Notification: domain.AlarmNotification{RoomID: testRoomID, AlarmType: domain.AlarmTypeLive, MinutesUntil: 5}}
	otherRoom := &domain.AlarmQueueEnvelope{Notification: domain.AlarmNotification{RoomID: testOtherRoomID, AlarmType: domain.AlarmTypeLive, MinutesUntil: 5}}

	if BuildDispatchGroupKeyFromEnvelope(first) != BuildDispatchGroupKeyFromEnvelope(second) {
		t.Fatal("equivalent delivery events must share a dispatch group")
	}

	if BuildDispatchGroupKeyFromEnvelope(first) == BuildDispatchGroupKeyFromEnvelope(otherRoom) {
		t.Fatal("different rooms must not share a dispatch group")
	}
}

func TestBuildDispatchGroupKeyDeliveryDigestUsesContentIdentity(t *testing.T) {
	t.Parallel()

	first := &domain.AlarmQueueEnvelope{
		Notification:   domain.AlarmNotification{RoomID: testRoomID, AlarmType: domain.AlarmTypeCommunity},
		SourceKind:     domain.AlarmDispatchSourceKindDeliveryDigest,
		DeliveryDigest: &domain.DeliveryDigestDispatchPayload{Kind: domain.DeliveryKindMemberNewsMonthly, PeriodKey: testDigestPeriodKey, PreRenderedMessage: "8월 멤버 뉴스 A"},
	}
	equivalent := *first
	otherMessage := equivalent

	otherMessage.DeliveryDigest = &domain.DeliveryDigestDispatchPayload{Kind: domain.DeliveryKindMemberNewsMonthly, PeriodKey: testDigestPeriodKey, PreRenderedMessage: "8월 멤버 뉴스 B"}

	otherRoom := equivalent

	otherRoom.Notification.RoomID = testOtherRoomID

	if BuildDispatchGroupKeyFromEnvelope(first) != BuildDispatchGroupKeyFromEnvelope(&equivalent) {
		t.Fatal("same rendered message and room must share a dispatch group")
	}

	if BuildDispatchGroupKeyFromEnvelope(first) == BuildDispatchGroupKeyFromEnvelope(&otherMessage) {
		t.Fatal("different rendered messages must not share a dispatch group")
	}

	if BuildDispatchGroupKeyFromEnvelope(first) == BuildDispatchGroupKeyFromEnvelope(&otherRoom) {
		t.Fatal("same rendered message in different rooms must not share a dispatch group")
	}
}

func TestBuildDispatchGroupKeyCelebrationUsesMemberIDOnly(t *testing.T) {
	t.Parallel()

	envelope := func(memberID int, channelID string) *domain.AlarmQueueEnvelope {
		return &domain.AlarmQueueEnvelope{
			Notification: domain.AlarmNotification{RoomID: testRoomID, AlarmType: domain.AlarmTypeBirthday},
			SourceKind:   domain.AlarmDispatchSourceKindCelebration,
			Celebration:  &domain.CelebrationDispatchPayload{Kind: domain.CelebrationKindBirthday, MemberID: memberID, ChannelID: channelID, Date: "2026-05-26"},
		}
	}

	// 채널을 공유하거나 채널이 바뀐 멤버도 MemberID로만 묶는다. ChannelID로 대신 묶던 폴백은 지웠다.
	if BuildDispatchGroupKeyFromEnvelope(envelope(101, "UC_old")) != BuildDispatchGroupKeyFromEnvelope(envelope(101, "UC_new")) {
		t.Fatal("channel change must not split one member's celebration group")
	}

	if BuildDispatchGroupKeyFromEnvelope(envelope(101, "UC_shared")) == BuildDispatchGroupKeyFromEnvelope(envelope(202, "UC_shared")) {
		t.Fatal("members sharing a channel must not share a celebration group")
	}
}
