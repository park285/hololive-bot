package domain

import (
	"encoding/json"
	"testing"
)

func TestDeliveryDigestDispatchPayloadIdentityAndValidation(t *testing.T) {
	t.Parallel()

	payload := &DeliveryDigestDispatchPayload{
		Kind:               DeliveryKindMajorEventWeekly,
		PeriodKey:          "2026-W32",
		PreRenderedMessage: "이번 주 주요 이벤트",
	}
	if err := payload.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	identity := payload.Identity()
	if identity == "" {
		t.Fatal("Identity() is empty")
	}
	changedMessage := *payload
	changedMessage.PreRenderedMessage = "교정된 주요 이벤트"
	if changedMessage.Identity() != identity {
		t.Fatal("message-only edit changed period identity")
	}
	changedPeriod := *payload
	changedPeriod.PeriodKey = "2026-W33"
	if changedPeriod.Identity() == identity {
		t.Fatal("period change did not change identity")
	}
}

func TestAlarmQueueEnvelopeRoundTripsDeliveryDigest(t *testing.T) {
	t.Parallel()

	envelope := AlarmQueueEnvelope{
		Notification: AlarmNotification{AlarmType: AlarmTypeCommunity, RoomID: "room-1"},
		SourceKind:   AlarmDispatchSourceKindDeliveryDigest,
		DeliveryDigest: &DeliveryDigestDispatchPayload{
			Kind:               DeliveryKindMemberNewsMonthly,
			PeriodKey:          "2026-08",
			PreRenderedMessage: "8월 멤버 뉴스",
		},
		Version: 1,
	}
	if err := envelope.ValidateCanonicalDispatch(); err != nil {
		t.Fatalf("ValidateCanonicalDispatch() error = %v", err)
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var decoded AlarmQueueEnvelope
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded.DeliveryDigest == nil || decoded.DeliveryDigest.PreRenderedMessage != "8월 멤버 뉴스" {
		t.Fatalf("DeliveryDigest = %#v", decoded.DeliveryDigest)
	}
}
