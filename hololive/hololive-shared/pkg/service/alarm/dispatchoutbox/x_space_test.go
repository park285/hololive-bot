package dispatchoutbox

import (
	jsonv2 "encoding/json/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestXSpaceEventAndRoomDeliveryIdentities(t *testing.T) {
	payload := &domain.XSpaceDispatchPayload{SpaceID: "1abc", CreatorID: "123", ChannelID: "UCtest", MemberName: "소라", Title: "테스트", StartedAt: time.Now().UTC()}
	first := domain.AlarmQueueEnvelope{
		SourceKind: domain.AlarmDispatchSourceKindXSpace, XSpace: payload,
		Notification: domain.AlarmNotification{AlarmType: domain.AlarmTypeLive, RoomID: "room-a"}, Version: 1,
	}
	second := first

	second.Notification.RoomID = "room-b"

	eventA, deliveryA, err := buildLedgerRows(&first, StatusPending)
	require.NoError(t, err)

	eventB, deliveryB, err := buildLedgerRows(&second, StatusPending)
	require.NoError(t, err)
	require.Equal(t, "x-space:start:1abc", eventA.EventKey)
	require.Equal(t, eventA.Payload, eventB.Payload)
	require.NotEqual(t, deliveryA.DedupeKey, deliveryB.DedupeKey)

	var restored domain.AlarmQueueEnvelope

	require.NoError(t, jsonv2.Unmarshal(eventA.Payload, &restored))
	require.Equal(t, payload, restored.XSpace)

	changed := *payload

	changed.Title = "수정된 제목"
	second.XSpace = &changed
	require.Equal(t, EnvelopeDedupeInput(&first).SourceIdentity, EnvelopeDedupeInput(&second).SourceIdentity)
	require.NotEqual(t, BuildDispatchGroupKeyFromEnvelope(&first), BuildDispatchGroupKeyFromEnvelope(&domain.AlarmQueueEnvelope{Notification: first.Notification}))
}
