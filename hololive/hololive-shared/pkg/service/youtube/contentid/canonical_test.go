package contentid

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	testShortVideoID         = "AbC123xyZ89"
	testShortCanonicalID     = "short:AbC123xyZ89"
	testCommunityPostID      = "UgkxPost123"
	testCommunityCanonicalID = "community:UgkxPost123"
)

func TestForShort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "raw video id", input: testShortVideoID, want: testShortCanonicalID},
		{name: "trim spaces", input: "  AbC123xyZ89  ", want: testShortCanonicalID},
		{name: "already canonical", input: testShortCanonicalID, want: testShortCanonicalID},
		{name: "canonical suffix trim", input: "short:  AbC123xyZ89  ", want: testShortCanonicalID},
		{name: "wrong prefix", input: "community:UgkxPost", wantErr: "prefix mismatch"},
		{name: "empty", input: "   ", wantErr: "is empty"},
		{name: "too long", input: strings.Repeat("s", MaxLogicalIDLength), wantErr: "too long"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ForShort(tt.input)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeShortVideoID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "raw video id", input: testShortVideoID, want: testShortVideoID},
		{name: "trim spaces", input: "  AbC123xyZ89  ", want: testShortVideoID},
		{name: "already canonical", input: testShortCanonicalID, want: testShortVideoID},
		{name: "canonical suffix trim", input: "short:  AbC123xyZ89  ", want: testShortVideoID},
		{name: "wrong prefix", input: "community:UgkxPost", wantErr: "prefix mismatch"},
		{name: "empty", input: "   ", wantErr: "is empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeShortVideoID(tt.input)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeCommunityPostID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "raw post id", input: testCommunityPostID, want: testCommunityPostID},
		{name: "trim spaces", input: "  UgkxPost123  ", want: testCommunityPostID},
		{name: "already canonical", input: testCommunityCanonicalID, want: testCommunityPostID},
		{name: "canonical suffix trim", input: "community:  UgkxPost123 ", want: testCommunityPostID},
		{name: "relative post url", input: "/post/UgkxPost123?lc=1", want: testCommunityPostID},
		{name: "full post url", input: "https://www.youtube.com/post/UgkxPost123?lc=1", want: testCommunityPostID},
		{name: "escaped post url", input: `https:\/\/www.youtube.com\/post\/UgkxPost123?lc=1`, want: testCommunityPostID},
		{name: "wrong prefix", input: testShortCanonicalID, wantErr: "prefix mismatch"},
		{name: "empty", input: "", wantErr: "is empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeCommunityPostID(tt.input)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestForCommunity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "raw post id", input: testCommunityPostID, want: testCommunityCanonicalID},
		{name: "trim spaces", input: "  UgkxPost123  ", want: testCommunityCanonicalID},
		{name: "already canonical", input: testCommunityCanonicalID, want: testCommunityCanonicalID},
		{name: "canonical suffix trim", input: "community:  UgkxPost123 ", want: testCommunityCanonicalID},
		{name: "post url", input: "/post/UgkxPost123?lc=1", want: testCommunityCanonicalID},
		{name: "wrong prefix", input: testShortCanonicalID, wantErr: "prefix mismatch"},
		{name: "empty", input: "", wantErr: "is empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ForCommunity(tt.input)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestForOutboxKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		kind       domain.OutboxKind
		resourceID string
		want       string
	}{
		{name: "short", kind: domain.OutboxKindNewShort, resourceID: testShortVideoID, want: testShortCanonicalID},
		{name: "community", kind: domain.OutboxKindCommunityPost, resourceID: "/post/UgkxPost123?lc=1", want: testCommunityCanonicalID},
		{name: "video", kind: domain.OutboxKindNewVideo, resourceID: " video-1 ", want: "video-1"},
		{name: "live", kind: domain.OutboxKindLiveStream, resourceID: " live-1 ", want: "live-1"},
		{name: "milestone", kind: domain.OutboxKindMilestone, resourceID: " milestone-1 ", want: "milestone-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ForOutboxKind(tt.kind, tt.resourceID)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResolveLogicalKeyValidatesSchemaBounds(t *testing.T) {
	t.Parallel()

	key, err := ResolveLogicalKey(domain.OutboxKindCommunityPost, "/post/UgkxPost123?lc=1", " room-1 ")
	require.NoError(t, err)
	require.Equal(t, LogicalKey{
		Kind:      domain.OutboxKindCommunityPost,
		LogicalID: testCommunityCanonicalID,
		RoomID:    "room-1",
	}, key)
	require.Len(t, key.Hash(), 32)

	_, err = ResolveLogicalKey(domain.OutboxKindNewVideo, "video-1", strings.Repeat("r", MaxRoomIDLength+1))
	require.Error(t, err)

	identityErr, ok := errors.AsType[*Error](err)
	require.True(t, ok)
	require.Equal(t, ErrorReasonTooLong, identityErr.Reason)
	require.Equal(t, "room id", identityErr.Field)
}

func TestResolveDeliveryKeyUsesValidatedCanonicalPayload(t *testing.T) {
	t.Parallel()

	key, err := ResolveDeliveryKey(
		domain.OutboxKindCommunityPost,
		testCommunityCanonicalID,
		`{"post_id":"UgkxPost123","canonical_post_id":"community:UgkxPost123"}`,
		"room-1",
	)
	require.NoError(t, err)
	require.Equal(t, testCommunityCanonicalID, key.LogicalID)

	_, err = ResolveDeliveryKey(
		domain.OutboxKindNewShort,
		testShortCanonicalID,
		`{"video_id":"different","canonical_post_id":"short:different"}`,
		"room-1",
	)
	require.Error(t, err)

	identityErr, ok := errors.AsType[*Error](err)
	require.True(t, ok)
	require.Equal(t, ErrorReasonMismatch, identityErr.Reason)

	_, err = ResolveDeliveryKey(
		domain.OutboxKindNewShort,
		testShortCanonicalID,
		`{"video_id":"AbC123xyZ89"}`,
		"room-1",
	)
	identityErr, ok = errors.AsType[*Error](err)
	require.True(t, ok)
	require.Equal(t, ErrorReasonEmpty, identityErr.Reason)

	_, err = ResolveDeliveryKey(domain.OutboxKindNewShort, testShortCanonicalID, `{`, "room-1")
	identityErr, ok = errors.AsType[*Error](err)
	require.True(t, ok)
	require.Equal(t, ErrorReasonInvalidPayload, identityErr.Reason)

	key, err = ResolveDeliveryKey(domain.OutboxKindNewVideo, " video-1 ", "not-json", "room-1")
	require.NoError(t, err)
	require.Equal(t, "video-1", key.LogicalID)
}

func TestInvalidIdentityReturnsTypedRedactedError(t *testing.T) {
	t.Parallel()

	secretValue := strings.Repeat("x", MaxLogicalIDLength+1)
	_, err := ForOutboxKind(domain.OutboxKindNewVideo, secretValue)
	require.Error(t, err)
	require.NotContains(t, err.Error(), secretValue)

	identityErr, ok := errors.AsType[*Error](err)
	require.True(t, ok)
	require.Equal(t, domain.OutboxKindNewVideo, identityErr.Kind)
	require.Equal(t, ErrorReasonTooLong, identityErr.Reason)

	_, err = ForOutboxKind(domain.OutboxKind("UNKNOWN"), "id")
	identityErr, ok = errors.AsType[*Error](err)
	require.True(t, ok)
	require.Equal(t, ErrorReasonUnsupportedKind, identityErr.Reason)
}
