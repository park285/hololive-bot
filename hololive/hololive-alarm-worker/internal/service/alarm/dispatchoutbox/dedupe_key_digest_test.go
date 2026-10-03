package dispatchoutbox

import (
	"bytes"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBuildLedgerRowsDeliveryDigestSharesRoomAgnosticEvent(t *testing.T) {
	t.Parallel()

	payload := &domain.DeliveryDigestDispatchPayload{
		Kind:               domain.DeliveryKindMajorEventMonthly,
		PeriodKey:          testDigestPeriodKey,
		PreRenderedMessage: "8월 주요 이벤트",
	}
	first := domain.AlarmQueueEnvelope{
		Notification:   domain.AlarmNotification{AlarmType: domain.AlarmTypeCommunity, RoomID: testRoomID},
		SourceKind:     domain.AlarmDispatchSourceKindDeliveryDigest,
		DeliveryDigest: payload,
		Version:        1,
	}
	second := first

	second.Notification.RoomID = testOtherRoomID

	firstEvent, firstDelivery, err := buildLedgerRows(&first)
	if err != nil {
		t.Fatalf("buildLedgerRows(first) error = %v", err)
	}

	secondEvent, secondDelivery, err := buildLedgerRows(&second)
	if err != nil {
		t.Fatalf("buildLedgerRows(second) error = %v", err)
	}

	if firstEvent.EventKey != secondEvent.EventKey || !bytes.Equal(firstEvent.Payload, secondEvent.Payload) {
		t.Fatalf("room-agnostic event mismatch: first=%q second=%q", firstEvent.EventKey, secondEvent.EventKey)
	}

	if firstDelivery.DedupeKey == secondDelivery.DedupeKey {
		t.Fatal("per-room delivery dedupe keys are equal")
	}

	// shadowed 행을 없앤 뒤 모든 delivery는 pending이라 dispatch group key를 가진다. send unit은 batch 단위로 나중에 배정한다.
	if firstDelivery.DispatchGroupKey == "" || firstDelivery.SendUnitKey != "" {
		t.Fatalf("pending delivery send identity: %#v", firstDelivery)
	}
}

func TestBuildLedgerRowsDeliveryDigestSeparatesRenderedMessages(t *testing.T) {
	t.Parallel()

	first := domain.AlarmQueueEnvelope{
		Notification:   domain.AlarmNotification{AlarmType: domain.AlarmTypeCommunity, RoomID: testRoomID},
		SourceKind:     domain.AlarmDispatchSourceKindDeliveryDigest,
		DeliveryDigest: &domain.DeliveryDigestDispatchPayload{Kind: domain.DeliveryKindMemberNewsMonthly, PeriodKey: testDigestPeriodKey, PreRenderedMessage: "8월 멤버 뉴스 A"},
		Version:        1,
	}
	second := first

	second.Notification.RoomID = testOtherRoomID
	second.DeliveryDigest = &domain.DeliveryDigestDispatchPayload{Kind: domain.DeliveryKindMemberNewsMonthly, PeriodKey: testDigestPeriodKey, PreRenderedMessage: "8월 멤버 뉴스 B"}

	firstEvent, _, err := buildLedgerRows(&first)
	if err != nil {
		t.Fatalf("buildLedgerRows(first) error = %v", err)
	}

	secondEvent, _, err := buildLedgerRows(&second)
	if err != nil {
		t.Fatalf("buildLedgerRows(second) error = %v", err)
	}

	if firstEvent.EventKey == secondEvent.EventKey {
		t.Fatalf("different rendered messages share event key: %q", firstEvent.EventKey)
	}

	if bytes.Equal(firstEvent.Payload, secondEvent.Payload) {
		t.Fatal("different rendered messages produced identical event payloads")
	}
}

func TestDeliveryDigestContentIdentityPropagatesThroughPendingSendIdentity(t *testing.T) {
	t.Parallel()

	first := domain.AlarmQueueEnvelope{
		Notification:   domain.AlarmNotification{AlarmType: domain.AlarmTypeCommunity, RoomID: testRoomID},
		SourceKind:     domain.AlarmDispatchSourceKindDeliveryDigest,
		DeliveryDigest: &domain.DeliveryDigestDispatchPayload{Kind: domain.DeliveryKindMemberNewsMonthly, PeriodKey: testDigestPeriodKey, PreRenderedMessage: "8월 멤버 뉴스 A"},
		Version:        1,
	}
	second := first

	second.DeliveryDigest = &domain.DeliveryDigestDispatchPayload{Kind: domain.DeliveryKindMemberNewsMonthly, PeriodKey: testDigestPeriodKey, PreRenderedMessage: "8월 멤버 뉴스 B"}

	firstEvent, firstDelivery, err := buildLedgerRows(&first)
	if err != nil {
		t.Fatalf("buildLedgerRows(first) error = %v", err)
	}

	secondEvent, secondDelivery, err := buildLedgerRows(&second)
	if err != nil {
		t.Fatalf("buildLedgerRows(second) error = %v", err)
	}

	if firstEvent.EventKey == secondEvent.EventKey {
		t.Fatalf("different rendered messages share event key: %q", firstEvent.EventKey)
	}

	if firstDelivery.DedupeKey == secondDelivery.DedupeKey {
		t.Fatalf("different rendered messages share delivery dedupe key: %q", firstDelivery.DedupeKey)
	}

	if firstDelivery.DispatchGroupKey == secondDelivery.DispatchGroupKey {
		t.Fatalf("different rendered messages share dispatch group key: %q", firstDelivery.DispatchGroupKey)
	}

	deliveries := []deliveryInsert{firstDelivery, secondDelivery}
	assignSendUnits(deliveries)

	if deliveries[0].SendUnitKey == deliveries[1].SendUnitKey {
		t.Fatalf("different rendered messages share send unit key: %q", deliveries[0].SendUnitKey)
	}

	if deliveries[0].ClientRequestID == deliveries[1].ClientRequestID {
		t.Fatalf("different rendered messages share client request id: %q", deliveries[0].ClientRequestID)
	}
}
