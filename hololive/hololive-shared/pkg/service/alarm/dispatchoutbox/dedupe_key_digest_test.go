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
		PeriodKey:          "2026-08",
		PreRenderedMessage: "8월 주요 이벤트",
	}
	first := domain.AlarmQueueEnvelope{
		Notification:   domain.AlarmNotification{AlarmType: domain.AlarmTypeCommunity, RoomID: "room-1"},
		SourceKind:     domain.AlarmDispatchSourceKindDeliveryDigest,
		DeliveryDigest: payload,
		Version:        1,
	}
	second := first
	second.Notification.RoomID = "room-2"

	firstEvent, firstDelivery, err := buildLedgerRows(&first, StatusShadowed)
	if err != nil {
		t.Fatalf("buildLedgerRows(first) error = %v", err)
	}
	secondEvent, secondDelivery, err := buildLedgerRows(&second, StatusShadowed)
	if err != nil {
		t.Fatalf("buildLedgerRows(second) error = %v", err)
	}
	if firstEvent.EventKey != secondEvent.EventKey || !bytes.Equal(firstEvent.Payload, secondEvent.Payload) {
		t.Fatalf("room-agnostic event mismatch: first=%q second=%q", firstEvent.EventKey, secondEvent.EventKey)
	}
	if firstDelivery.DedupeKey == secondDelivery.DedupeKey {
		t.Fatal("per-room delivery dedupe keys are equal")
	}
	if firstDelivery.Status != StatusShadowed || secondDelivery.Status != StatusShadowed {
		t.Fatalf("statuses = %q/%q", firstDelivery.Status, secondDelivery.Status)
	}
	if firstDelivery.DispatchGroupKey != "" || firstDelivery.SendUnitKey != "" {
		t.Fatalf("shadow delivery allocated send identity: %#v", firstDelivery)
	}
}
