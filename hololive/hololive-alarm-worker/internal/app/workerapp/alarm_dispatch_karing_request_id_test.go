package workerapp

import (
	"fmt"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func alarmDispatchKaringIdentityTestEnvelope(roomID string, dispatchOutboxID int64) domain.AlarmQueueEnvelope {
	envelope := alarmDispatchRunnerTestEnvelope(roomID, nil)
	envelope.DispatchOutboxID = dispatchOutboxID
	envelope.Notification.Stream.ID = fmt.Sprintf("stream-%d", dispatchOutboxID)
	envelope.Notification.Stream.Title = fmt.Sprintf("Stream %d", dispatchOutboxID)
	return envelope
}

func karingRequestIDs(t *testing.T, envelopes []domain.AlarmQueueEnvelope) []string {
	t.Helper()
	groups := groupAlarmDispatchEnvelopesForKaring(envelopes, true)
	require.Len(t, groups, 1)
	requests, err := buildAlarmDispatchKaringContentListRequests(t.Context(), nil, groups[0])
	require.NoError(t, err)
	ids := make([]string, 0, len(requests))
	for i := range requests {
		require.NotNil(t, requests[i].ClientRequestID)
		ids = append(ids, *requests[i].ClientRequestID)
	}
	return ids
}

func TestAlarmDispatchKaringChunkClientRequestIDStableAcrossGroupRecomposition(t *testing.T) {
	first := make([]domain.AlarmQueueEnvelope, 0, 5)
	for id := int64(1); id <= 5; id++ {
		first = append(first, alarmDispatchKaringIdentityTestEnvelope("room-1", id))
	}
	firstIDs := karingRequestIDs(t, first)
	require.Len(t, firstIDs, 2)

	recomposed := []domain.AlarmQueueEnvelope{
		alarmDispatchKaringIdentityTestEnvelope("room-1", 1),
		alarmDispatchKaringIdentityTestEnvelope("room-1", 2),
		alarmDispatchKaringIdentityTestEnvelope("room-1", 3),
		alarmDispatchKaringIdentityTestEnvelope("room-1", 4),
		alarmDispatchKaringIdentityTestEnvelope("room-1", 6),
	}
	recomposedIDs := karingRequestIDs(t, recomposed)
	require.Len(t, recomposedIDs, 2)

	assert.Equal(t, firstIDs[0], recomposedIDs[0],
		"동일 item 조합(1..4) chunk는 그룹 구성이 바뀌어도 ClientRequestID가 같아야 admission dedup이 기전송 chunk를 걸러낸다")
	assert.NotEqual(t, firstIDs[1], recomposedIDs[1],
		"item 조합이 다른 chunk(5 vs 6)는 서로 다른 ClientRequestID를 가져야 한다")
}

func TestAlarmDispatchKaringChunkClientRequestIDIndependentOfEnvelopeOrder(t *testing.T) {
	ordered := make([]domain.AlarmQueueEnvelope, 0, 5)
	for id := int64(1); id <= 5; id++ {
		ordered = append(ordered, alarmDispatchKaringIdentityTestEnvelope("room-1", id))
	}
	shuffled := []domain.AlarmQueueEnvelope{
		alarmDispatchKaringIdentityTestEnvelope("room-1", 4),
		alarmDispatchKaringIdentityTestEnvelope("room-1", 1),
		alarmDispatchKaringIdentityTestEnvelope("room-1", 5),
		alarmDispatchKaringIdentityTestEnvelope("room-1", 3),
		alarmDispatchKaringIdentityTestEnvelope("room-1", 2),
	}

	assert.Equal(t, karingRequestIDs(t, ordered), karingRequestIDs(t, shuffled),
		"chunk 경계는 item identity 정렬로 고정되어 드레인 순서와 무관해야 한다")
}

func TestAlarmDispatchKaringChunkClientRequestIDDiffersPerRoom(t *testing.T) {
	room1 := []domain.AlarmQueueEnvelope{alarmDispatchKaringIdentityTestEnvelope("room-1", 1)}
	room2 := []domain.AlarmQueueEnvelope{alarmDispatchKaringIdentityTestEnvelope("room-2", 1)}

	assert.NotEqual(t, karingRequestIDs(t, room1), karingRequestIDs(t, room2))
}

func TestAlarmDispatchKaringChunkClientRequestIDUsesOutboxItemIdentity(t *testing.T) {
	buildOutboxEnvelope := func(dispatchOutboxID int64) domain.AlarmQueueEnvelope {
		envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
		envelope.DispatchOutboxID = dispatchOutboxID
		envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
		envelope.Notification.AlarmType = domain.AlarmTypeCommunity
		envelope.YouTubeOutbox = &domain.YouTubeOutboxDispatchPayload{
			Kind:       domain.OutboxKindCommunityPost,
			AlarmType:  domain.AlarmTypeCommunity,
			ChannelID:  "UCtest",
			MemberName: "Member",
			Items: []domain.YouTubeOutboxItem{{
				OutboxID:  77,
				ContentID: "UgkxPost",
				Payload:   `{"post_id":"UgkxPost","content_text":"본문"}`,
			}},
		}
		return envelope
	}

	assert.Equal(t,
		karingRequestIDs(t, []domain.AlarmQueueEnvelope{buildOutboxEnvelope(10)}),
		karingRequestIDs(t, []domain.AlarmQueueEnvelope{buildOutboxEnvelope(11)}),
	)
}
