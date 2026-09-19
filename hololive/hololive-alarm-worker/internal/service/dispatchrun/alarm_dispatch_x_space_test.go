package dispatchrun

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestXSpaceRenderingAndIsolation(t *testing.T) {
	envelope := domain.AlarmQueueEnvelope{
		SourceKind:   domain.AlarmDispatchSourceKindXSpace,
		Notification: domain.AlarmNotification{RoomID: "room-a", AlarmType: domain.AlarmTypeLive},
		XSpace:       &domain.XSpaceDispatchPayload{SpaceID: "1abc", CreatorID: "123", ChannelID: "UCtest", MemberName: "소라", Title: "이야기", StartedAt: time.Now().UTC()},
	}
	message, handled, err := renderAlarmDispatchGroupSource(t.Context(), nil, nil, alarmDispatchGroup{envelopes: []domain.AlarmQueueEnvelope{envelope}})
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, "소라 스페이스 시작\n이야기\nhttps://x.com/i/spaces/1abc", message)

	path, _, err := alarmDispatchEnvelopeEgressPath(t.Context(), nil, &envelope)
	require.NoError(t, err)
	require.Equal(t, alarmDispatchEgressText, path)

	other := envelope

	other.XSpace = new(*envelope.XSpace)
	other.XSpace.SpaceID = "1def"
	require.NotEqual(t, alarmDispatchGroupKey(&envelope), alarmDispatchGroupKey(&other))
	require.NotEqual(t, alarmDispatchGroupKey(&envelope), alarmDispatchGroupKey(&domain.AlarmQueueEnvelope{Notification: envelope.Notification}))

	envelope.XSpace.SpaceID = "bad/path"
	_, _, err = renderAlarmDispatchGroupSource(t.Context(), nil, nil, alarmDispatchGroup{envelopes: []domain.AlarmQueueEnvelope{envelope}})
	require.Error(t, err)
}

func TestXSpaceDispatchUsesTextAndRecordsCompletion(t *testing.T) {
	envelope := domain.AlarmQueueEnvelope{
		SourceKind:   domain.AlarmDispatchSourceKindXSpace,
		Notification: domain.AlarmNotification{RoomID: testAlarmRoomID, AlarmType: domain.AlarmTypeLive},
		XSpace:       &domain.XSpaceDispatchPayload{SpaceID: "1abc", CreatorID: "123", ChannelID: "UCtest", MemberName: "소라", Title: "이야기", StartedAt: time.Now().UTC()},
	}
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{consumer: consumer, sender: sender, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	require.Equal(t, testAlarmRoomID, sender.roomID)
	require.Equal(t, []string{"소라 스페이스 시작\n이야기\nhttps://x.com/i/spaces/1abc"}, sender.messages)
	require.Empty(t, sender.karingRequests)
	require.Len(t, consumer.markSending, 1)
	require.Len(t, consumer.markDispatched, 1)
	require.Empty(t, consumer.scheduledRetry)
	require.Empty(t, consumer.movedDLQ)
}
