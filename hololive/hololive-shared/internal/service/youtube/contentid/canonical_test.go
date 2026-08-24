package contentid

import (
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

	shortID, err := ForOutboxKind(domain.OutboxKindNewShort, testShortVideoID)
	require.NoError(t, err)
	require.Equal(t, testShortCanonicalID, shortID)

	communityID, err := ForOutboxKind(domain.OutboxKindCommunityPost, "/post/UgkxPost123?lc=1")
	require.NoError(t, err)
	require.Equal(t, testCommunityCanonicalID, communityID)

	_, err = ForOutboxKind(domain.OutboxKindNewVideo, "video-1")
	require.ErrorContains(t, err, "unsupported outbox kind")
}
