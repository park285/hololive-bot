package dispatchrun

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
	ids := make([]string, 0, len(groups))
	for g := range groups {
		requests, err := buildAlarmDispatchKaringContentListRequests(t.Context(), nil, groups[g])
		require.NoError(t, err)
		require.Len(t, requests, 1,
			"분할 후에는 그룹당 chunk가 정확히 하나여야 부분 성공 상태가 생기지 않는다")
		require.NotNil(t, requests[0].ClientRequestID)
		ids = append(ids, *requests[0].ClientRequestID)
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

func TestAlarmDispatchPersistedSendUnitUsesDistinctStableKaringChunkIDs(t *testing.T) {
	build := func(order []int64) []domain.AlarmQueueEnvelope {
		envelopes := make([]domain.AlarmQueueEnvelope, 0, len(order))
		for _, id := range order {
			envelope := alarmDispatchKaringIdentityTestEnvelope("room-1", id)
			envelope.SendUnitID = 42
			envelope.ClientRequestID = "hololive-alarm:0123456789abcdef0123456789abcdef"
			envelopes = append(envelopes, envelope)
		}
		return envelopes
	}

	first := karingRequestIDs(t, build([]int64{1, 2, 3, 4, 5}))
	second := karingRequestIDs(t, build([]int64{5, 3, 1, 4, 2}))

	require.Len(t, first, 2)
	assert.NotEqual(t, first[0], first[1])
	assert.Equal(t, first, second)
}

func TestAlarmDispatchPersistedOutboxUsesDistinctKaringChunkIDs(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
	envelope.DispatchOutboxID = 42
	envelope.SendUnitID = 7
	envelope.ClientRequestID = "hololive-alarm:fedcba9876543210fedcba9876543210"
	envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
	envelope.Notification.AlarmType = domain.AlarmTypeCommunity
	items := make([]domain.YouTubeOutboxItem, 0, 6)
	for i := range 6 {
		items = append(items, domain.YouTubeOutboxItem{
			OutboxID:  int64(100 + i),
			ContentID: fmt.Sprintf("post-%d", i),
			Payload:   `{"post_id":"p","content_text":"본문"}`,
		})
	}
	envelope.YouTubeOutbox = &domain.YouTubeOutboxDispatchPayload{
		Kind:       domain.OutboxKindCommunityPost,
		AlarmType:  domain.AlarmTypeCommunity,
		ChannelID:  "UCtest",
		MemberName: "Member",
		Items:      items,
	}
	group := newAlarmDispatchGroup(&envelope)

	first, err := buildAlarmDispatchKaringContentListRequests(t.Context(), nil, group)
	require.NoError(t, err)
	second, err := buildAlarmDispatchKaringContentListRequests(t.Context(), nil, group)
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.Len(t, second, 2)
	require.NotNil(t, first[0].ClientRequestID)
	require.NotNil(t, first[1].ClientRequestID)
	assert.NotEqual(t, *first[0].ClientRequestID, *first[1].ClientRequestID)
	assert.Equal(t, *first[0].ClientRequestID, *second[0].ClientRequestID)
	assert.Equal(t, *first[1].ClientRequestID, *second[1].ClientRequestID)
}
