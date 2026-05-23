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

	result, err := addPreparedEvent(events, first, result)
	require.NoError(t, err)
	require.Equal(t, 1, result.RequestedEvents)

	conflict := eventInsert{EventKey: "key1", PayloadHash: "hash2"}
	result, err = addPreparedEvent(events, conflict, result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "dispatch event hash conflict")
	require.Equal(t, 1, result.HashConflictEvents)

	events2 := make(map[string]eventInsert)
	result2 := PublishBatchResult{}
	orig := eventInsert{EventKey: "key1", PayloadHash: "hash1"}
	result2, err = addPreparedEvent(events2, orig, result2)
	require.NoError(t, err)

	duplicate := eventInsert{EventKey: "key1", PayloadHash: "hash1"}
	result2, err = addPreparedEvent(events2, duplicate, result2)
	require.NoError(t, err)
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
