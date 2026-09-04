package dispatchrun

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func karingGroupsFor(t *testing.T, count int) []alarmDispatchGroup {
	t.Helper()

	envelopes := make([]domain.AlarmQueueEnvelope, 0, count)
	for id := int64(1); id <= int64(count); id++ {
		envelopes = append(envelopes, alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, id))
	}

	return groupAlarmDispatchEnvelopesForDelivery(t.Context(), &alarmDispatchRunnerTestSender{}, envelopes)
}

func TestKaringGroupNeverExceedsOneChunk(t *testing.T) {
	for _, count := range []int{1, 4, 5, 8, 9, 17} {
		groups := karingGroupsFor(t, count)

		total := 0

		for g := range groups {
			require.LessOrEqual(t, len(groups[g].envelopes), alarmDispatchKaringMaxItemsPerRequest,
				"a group larger than one chunk can be half-sent, and a 502 on the later chunk retries the whole group under new ClientRequestIDs")

			requests, err := buildAlarmDispatchKaringContentListRequests(t.Context(), nil, groups[g])
			require.NoError(t, err)
			require.Len(t, requests, 1)

			total += len(groups[g].envelopes)
		}

		require.Equal(t, count, total, "splitting must not drop or duplicate envelopes")
	}
}

func TestKaringSplitKeepsEveryEnvelopeExactlyOnce(t *testing.T) {
	groups := karingGroupsFor(t, 9)

	seen := map[int64]int{}

	for g := range groups {
		for i := range groups[g].envelopes {
			seen[groups[g].envelopes[i].DispatchOutboxID]++
		}

		require.Len(t, groups[g].notifications, len(groups[g].envelopes),
			"notifications must stay index-aligned with envelopes; karing item identity pairs them by index")
	}

	require.Len(t, seen, 9)

	for id, count := range seen {
		assert.Equal(t, 1, count, "envelope %d appeared %d times across groups", id, count)
	}
}

func TestKaringSplitIsIndependentOfDrainOrder(t *testing.T) {
	ordered := make([]domain.AlarmQueueEnvelope, 0, 9)

	for id := int64(1); id <= 9; id++ {
		ordered = append(ordered, alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, id))
	}

	shuffled := []domain.AlarmQueueEnvelope{
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 7),
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 2),
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 9),
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 1),
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 5),
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 3),
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 8),
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 6),
		alarmDispatchKaringIdentityTestEnvelope(testAlarmRoomID, 4),
	}

	assert.Equal(t, karingRequestIDs(t, ordered), karingRequestIDs(t, shuffled),
		"the split must run on the same identity order as chunking, or drain order leaks into ClientRequestID")
}

func TestKaringSplitLeavesOutboxGroupsIntact(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.DispatchOutboxID = 42
	envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
	envelope.Notification.AlarmType = domain.AlarmTypeCommunity

	items := make([]domain.YouTubeOutboxItem, 0, 6)

	for i := range 6 {
		items = append(items, domain.YouTubeOutboxItem{
			OutboxID:  int64(100 + i),
			ContentID: string(rune('a'+i)) + "-post",
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

	groups := groupAlarmDispatchEnvelopesForDelivery(t.Context(), &alarmDispatchRunnerTestSender{}, []domain.AlarmQueueEnvelope{envelope})

	require.Len(t, groups, 1, "an outbox envelope carries all its items; splitting by envelope cannot divide them")
	require.Len(t, groups[0].envelopes, 1)
}
