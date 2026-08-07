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
		envelopes = append(envelopes, alarmDispatchKaringIdentityTestEnvelope("room-1", id))
	}
	return groupAlarmDispatchEnvelopesForKaring(envelopes, true)
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
		ordered = append(ordered, alarmDispatchKaringIdentityTestEnvelope("room-1", id))
	}
	shuffled := []domain.AlarmQueueEnvelope{
		alarmDispatchKaringIdentityTestEnvelope("room-1", 7),
		alarmDispatchKaringIdentityTestEnvelope("room-1", 2),
		alarmDispatchKaringIdentityTestEnvelope("room-1", 9),
		alarmDispatchKaringIdentityTestEnvelope("room-1", 1),
		alarmDispatchKaringIdentityTestEnvelope("room-1", 5),
		alarmDispatchKaringIdentityTestEnvelope("room-1", 3),
		alarmDispatchKaringIdentityTestEnvelope("room-1", 8),
		alarmDispatchKaringIdentityTestEnvelope("room-1", 6),
		alarmDispatchKaringIdentityTestEnvelope("room-1", 4),
	}

	assert.Equal(t, karingRequestIDs(t, ordered), karingRequestIDs(t, shuffled),
		"the split must run on the same identity order as chunking, or drain order leaks into ClientRequestID")
}

func TestKaringSplitLeavesOutboxGroupsIntact(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
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
		ChannelID:  "UCtest",
		MemberName: "Member",
		Items:      items,
	}

	groups := groupAlarmDispatchEnvelopesForKaring([]domain.AlarmQueueEnvelope{envelope}, true)

	require.Len(t, groups, 1, "an outbox envelope carries all its items; splitting by envelope cannot divide them")
	require.Len(t, groups[0].envelopes, 1)
}
