package dispatchrun

import (
	"errors"
	"fmt"
	"testing"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
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

	groups := groupAlarmDispatchEnvelopesForDelivery(t.Context(), &alarmDispatchRunnerTestSender{}, envelopes)
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
		first = append(first, alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, id))
	}

	firstIDs := karingRequestIDs(t, first)
	require.Len(t, firstIDs, 2)

	recomposed := []domain.AlarmQueueEnvelope{
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 1),
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 2),
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 3),
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 4),
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 6),
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
		ordered = append(ordered, alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, id))
	}

	shuffled := []domain.AlarmQueueEnvelope{
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 4),
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 1),
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 5),
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 3),
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 2),
	}

	assert.Equal(t, karingRequestIDs(t, ordered), karingRequestIDs(t, shuffled),
		"chunk 경계는 item identity 정렬로 고정되어 드레인 순서와 무관해야 한다")
}

func TestAlarmDispatchKaringChunkClientRequestIDDiffersPerRoom(t *testing.T) {
	room1 := []domain.AlarmQueueEnvelope{alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 1)}
	room2 := []domain.AlarmQueueEnvelope{alarmDispatchKaringIdentityTestEnvelope("room-2", 1)}

	assert.NotEqual(t, karingRequestIDs(t, room1), karingRequestIDs(t, room2))
}

func TestAlarmDispatchKaringChunkClientRequestIDUsesOutboxItemIdentity(t *testing.T) {
	buildOutboxEnvelope := func(dispatchOutboxID int64) domain.AlarmQueueEnvelope {
		envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

		envelope.DispatchOutboxID = dispatchOutboxID
		envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
		envelope.Notification.AlarmType = domain.AlarmTypeCommunity
		envelope.YouTubeOutbox = &domain.YouTubeOutboxDispatchPayload{
			Kind:       domain.OutboxKindCommunityPost,
			AlarmType:  domain.AlarmTypeCommunity,
			ChannelID:  testAlarmChannelID,
			MemberName: testAlarmMemberName,
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
			envelope := alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, id)

			envelope.SendUnitID = 42
			envelope.ClientRequestID = testRetryClientRequestID
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
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

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
		ChannelID:  testAlarmChannelID,
		MemberName: testAlarmMemberName,
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

func alarmDispatchPersistedSendUnitTestEnvelopes(t *testing.T, count int) []domain.AlarmQueueEnvelope {
	t.Helper()

	envelopes := make([]domain.AlarmQueueEnvelope, 0, count)
	for i := range count {
		envelope := alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, int64(11+i))

		envelope.SendUnitID = 7
		envelope.ClientRequestID = testRetryClientRequestID
		envelopes = append(envelopes, envelope)
	}

	return envelopes
}

func TestHasPersistedClientRequestIDMatchesTextIDActuallySent(t *testing.T) {
	t.Parallel()

	for _, count := range []int{1, alarmDispatchKaringMaxItemsPerRequest, alarmDispatchKaringMaxItemsPerRequest + 1} {
		envelopes := alarmDispatchPersistedSendUnitTestEnvelopes(t, count)
		group := alarmDispatchGroup{roomID: testAlarmRoomID, envelopes: envelopes}
		sentIsPersisted := alarmDispatchClientRequestID(group, 0, len(envelopes)) == envelopes[0].ClientRequestID

		assert.Equal(t, sentIsPersisted, hasPersistedClientRequestID(envelopes),
			"재시도 허가는 실제로 전송된 ClientRequestID가 persisted ID일 때만 참이어야 한다 (envelopes=%d)", count)
	}
}

func TestAlarmDispatchRunnerPersistedSendUnitOverMaxItemsQuarantinesAmbiguousKaringFailure(t *testing.T) {
	transportErr := &iris.TransportError{Op: testIrisPostOp, URL: testIrisReplyPath, Err: errors.New("connection reset")}
	envelopes := alarmDispatchPersistedSendUnitTestEnvelopes(t, alarmDispatchKaringMaxItemsPerRequest+1)
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{envelopes}}
	sender := &alarmDispatchRunnerTestSender{karingErr: transportErr}
	runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, sender.karingRequests, 2)
	require.NotNil(t, sender.karingRequests[0].ClientRequestID)
	require.NotNil(t, sender.karingRequests[1].ClientRequestID)
	assert.NotEqual(t, envelopes[0].ClientRequestID, *sender.karingRequests[0].ClientRequestID)
	assert.NotEqual(t, *sender.karingRequests[0].ClientRequestID, *sender.karingRequests[1].ClientRequestID)
	assert.Empty(t, consumer.scheduledSendingRetry)
	require.Len(t, consumer.quarantined, len(envelopes))
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerPersistedSendUnitAtMaxItemsQuarantinesAmbiguousKaringFailure(t *testing.T) {
	transportErr := &iris.TransportError{Op: testIrisPostOp, URL: testIrisReplyPath, Err: errors.New("connection reset")}
	envelopes := alarmDispatchPersistedSendUnitTestEnvelopes(t, alarmDispatchKaringMaxItemsPerRequest)
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{envelopes}}
	sender := &alarmDispatchRunnerTestSender{karingErr: transportErr}
	runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, sender.karingRequests, 1)
	require.NotNil(t, sender.karingRequests[0].ClientRequestID)
	assert.NotEqual(t, envelopes[0].ClientRequestID, *sender.karingRequests[0].ClientRequestID)
	assert.Equal(t, karingRequestIDs(t, envelopes)[0], *sender.karingRequests[0].ClientRequestID)
	assert.Empty(t, consumer.scheduledSendingRetry)
	require.Len(t, consumer.quarantined, len(envelopes))
}

func TestAlarmDispatchOutboxRetryReproducesEveryKaringChunkClientRequestID(t *testing.T) {
	t.Parallel()

	const itemCount = alarmDispatchKaringMaxItemsPerRequest + 2

	build := func(retry *domain.AlarmQueueRetryMetadata) domain.AlarmQueueEnvelope {
		envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, retry)

		envelope.DispatchOutboxID = 91
		envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
		envelope.Notification.AlarmType = domain.AlarmTypeCommunity

		items := make([]domain.YouTubeOutboxItem, 0, itemCount)

		for i := range itemCount {
			items = append(items, domain.YouTubeOutboxItem{
				OutboxID:  int64(200 + i),
				ContentID: fmt.Sprintf("post-%d", i),
				Payload:   `{"post_id":"p","content_text":"본문"}`,
			})
		}

		envelope.YouTubeOutbox = &domain.YouTubeOutboxDispatchPayload{
			Kind:       domain.OutboxKindCommunityPost,
			AlarmType:  domain.AlarmTypeCommunity,
			ChannelID:  testAlarmChannelID,
			MemberName: testAlarmMemberName,
			Items:      items,
		}

		return envelope
	}

	chunkIDs := func(envelope domain.AlarmQueueEnvelope) []string {
		groups := groupAlarmDispatchEnvelopesForDelivery(t.Context(), &alarmDispatchRunnerTestSender{}, []domain.AlarmQueueEnvelope{envelope})
		ids := make([]string, 0, itemCount)

		for g := range groups {
			requests, err := buildAlarmDispatchKaringContentListRequests(t.Context(), nil, groups[g])
			require.NoError(t, err)

			for i := range requests {
				require.NotNil(t, requests[i].ClientRequestID)

				ids = append(ids, *requests[i].ClientRequestID)
			}
		}

		return ids
	}

	firstSend := chunkIDs(build(nil))
	require.Len(t, firstSend, 2,
		"봉투 1건에 상한 초과 item을 담은 outbox 그룹은 요청 2건으로 나가 부분 성공 상태가 가능하다")

	redrained := chunkIDs(build(&domain.AlarmQueueRetryMetadata{Attempt: 1, LastError: "transport"}))

	assert.Equal(t, firstSend, redrained,
		"부분 성공 후 재드레인은 chunk ID를 그대로 재생산해야 이미 전송된 chunk가 Iris dedup에 접힌다")
}
