package dispatchoutbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTruncateHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "normal 64-char SHA256",
			in:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			want: "e3b0c442...",
		},
		{
			name: "exactly 9 chars",
			in:   "abcdefghi",
			want: "abcdefgh...",
		},
		{
			name: "exactly 8 chars",
			in:   "abcdefgh",
			want: "abcdefgh",
		},
		{
			name: "shorter than 8 chars",
			in:   "abc",
			want: "abc",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncateHash(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAddPreparedEvent_DetectsSameBatchHashConflict(t *testing.T) {
	t.Parallel()

	events := make(map[string]eventInsert)
	first := eventInsert{EventKey: "key1", PayloadHash: "hash1"}
	result := PublishBatchResult{}

	collision := addPreparedEvent(events, &first, &result)
	require.Nil(t, collision)
	require.Equal(t, 1, result.RequestedEvents)

	conflict := eventInsert{EventKey: "key1", PayloadHash: "hash2"}
	collision = addPreparedEvent(events, &conflict, &result)
	require.NotNil(t, collision)
	require.Equal(t, conflict, collision.Event)
	require.Equal(t, "hash1", collision.ExistingPayloadHash)
	require.Equal(t, 1, result.HashConflictEvents)

	events2 := make(map[string]eventInsert)
	result2 := PublishBatchResult{}
	orig := eventInsert{EventKey: "key1", PayloadHash: "hash1"}
	collision = addPreparedEvent(events2, &orig, &result2)
	require.Nil(t, collision)

	duplicate := eventInsert{EventKey: "key1", PayloadHash: "hash1"}
	collision = addPreparedEvent(events2, &duplicate, &result2)
	require.Nil(t, collision)
	require.Equal(t, 1, result2.RequestedEvents)
	require.Len(t, events2, 1)
}

func TestBuildEventBatchRows_ReturnsExpectedHashes(t *testing.T) {
	t.Parallel()

	events := []eventInsert{
		{EventKey: "key1", PayloadHash: "hash1", AlarmType: "LIVE", Payload: []byte(`{}`)},
		{EventKey: "key2", PayloadHash: "hash2", AlarmType: "LIVE", Payload: []byte(`{}`)},
	}
	rows, hashes := buildEventBatchRows(events)
	require.Len(t, rows, 2)
	require.Equal(t, "hash1", hashes["key1"])
	require.Equal(t, "hash2", hashes["key2"])
}

func TestClassifyEventPreflight_SplitsNewDuplicateAndConflict(t *testing.T) {
	t.Parallel()

	newEvent := eventInsert{EventKey: "new", PayloadHash: "new-hash"}
	duplicateEvent := eventInsert{EventKey: "duplicate", PayloadHash: "same-hash"}
	conflictEvent := eventInsert{
		EventKey:    "conflict",
		PayloadHash: "incoming-hash",
		AlarmType:   "LIVE",
		ChannelID:   "channel-1",
		StreamID:    "stream-1",
		Category:    "10",
		Payload:     []byte(`{"version":1}`),
	}
	existing := map[string]insertedEventRow{
		"duplicate": {
			ID:          11,
			EventKey:    "duplicate",
			PayloadHash: "same-hash",
		},
		"conflict": {
			ID:          12,
			EventKey:    "conflict",
			PayloadHash: "existing-hash",
		},
	}

	classified := classifyEventPreflight([]eventInsert{newEvent, duplicateEvent, conflictEvent}, existing)

	require.Equal(t, []eventInsert{newEvent}, classified.InsertEvents)
	require.Equal(t, map[string]int64{"duplicate": 11}, classified.EventIDs)
	require.Len(t, classified.Collisions, 1)
	require.Equal(t, conflictEvent, classified.Collisions[0].Event)
	require.Equal(t, int64(12), classified.Collisions[0].ExistingEventID)
	require.Equal(t, "existing-hash", classified.Collisions[0].ExistingPayloadHash)
}

func TestProcessedPublishBatchResult_TreatsConflictRecordAsProcessed(t *testing.T) {
	t.Parallel()

	input := PublishBatchResult{
		RequestedDeliveries: 2,
		HashConflictEvents:  1,
	}
	result := processedPublishBatchResult(&input)

	require.Equal(t, 2, result.ProcessedDeliveries)
}
