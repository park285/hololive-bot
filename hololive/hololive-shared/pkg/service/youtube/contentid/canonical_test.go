package contentid

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestForShort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "raw video id", input: "AbC123xyZ89", want: "short:AbC123xyZ89"},
		{name: "trim spaces", input: "  AbC123xyZ89  ", want: "short:AbC123xyZ89"},
		{name: "already canonical", input: "short:AbC123xyZ89", want: "short:AbC123xyZ89"},
		{name: "canonical suffix trim", input: "short:  AbC123xyZ89  ", want: "short:AbC123xyZ89"},
		{name: "wrong prefix", input: "community:UgkxPost", wantErr: "prefix mismatch"},
		{name: "empty", input: "   ", wantErr: "is empty"},
	}

	for _, tt := range tests {
		tt := tt
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
		{name: "raw video id", input: "AbC123xyZ89", want: "AbC123xyZ89"},
		{name: "trim spaces", input: "  AbC123xyZ89  ", want: "AbC123xyZ89"},
		{name: "already canonical", input: "short:AbC123xyZ89", want: "AbC123xyZ89"},
		{name: "canonical suffix trim", input: "short:  AbC123xyZ89  ", want: "AbC123xyZ89"},
		{name: "wrong prefix", input: "community:UgkxPost", wantErr: "prefix mismatch"},
		{name: "empty", input: "   ", wantErr: "is empty"},
	}

	for _, tt := range tests {
		tt := tt
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
		{name: "raw post id", input: "UgkxPost123", want: "UgkxPost123"},
		{name: "trim spaces", input: "  UgkxPost123  ", want: "UgkxPost123"},
		{name: "already canonical", input: "community:UgkxPost123", want: "UgkxPost123"},
		{name: "canonical suffix trim", input: "community:  UgkxPost123 ", want: "UgkxPost123"},
		{name: "relative post url", input: "/post/UgkxPost123?lc=1", want: "UgkxPost123"},
		{name: "full post url", input: "https://www.youtube.com/post/UgkxPost123?lc=1", want: "UgkxPost123"},
		{name: "escaped post url", input: `https:\/\/www.youtube.com\/post\/UgkxPost123?lc=1`, want: "UgkxPost123"},
		{name: "wrong prefix", input: "short:AbC123xyZ89", wantErr: "prefix mismatch"},
		{name: "empty", input: "", wantErr: "is empty"},
	}

	for _, tt := range tests {
		tt := tt
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
		{name: "raw post id", input: "UgkxPost123", want: "community:UgkxPost123"},
		{name: "trim spaces", input: "  UgkxPost123  ", want: "community:UgkxPost123"},
		{name: "already canonical", input: "community:UgkxPost123", want: "community:UgkxPost123"},
		{name: "canonical suffix trim", input: "community:  UgkxPost123 ", want: "community:UgkxPost123"},
		{name: "post url", input: "/post/UgkxPost123?lc=1", want: "community:UgkxPost123"},
		{name: "wrong prefix", input: "short:AbC123xyZ89", wantErr: "prefix mismatch"},
		{name: "empty", input: "", wantErr: "is empty"},
	}

	for _, tt := range tests {
		tt := tt
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

	shortID, err := ForOutboxKind(domain.OutboxKindNewShort, "AbC123xyZ89")
	require.NoError(t, err)
	require.Equal(t, "short:AbC123xyZ89", shortID)

	communityID, err := ForOutboxKind(domain.OutboxKindCommunityPost, "/post/UgkxPost123?lc=1")
	require.NoError(t, err)
	require.Equal(t, "community:UgkxPost123", communityID)

	_, err = ForOutboxKind(domain.OutboxKindNewVideo, "video-1")
	require.ErrorContains(t, err, "unsupported outbox kind")
}
