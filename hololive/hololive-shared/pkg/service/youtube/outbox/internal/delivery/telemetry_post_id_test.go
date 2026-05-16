package delivery

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestResolveTelemetryPostID_PrefersPayloadIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		kind      domain.OutboxKind
		contentID string
		payload   string
		want      string
	}{
		{
			name:    "short canonical post id",
			kind:    domain.OutboxKindNewShort,
			payload: `{"canonical_post_id":"short-canonical","video_id":"short-resource"}`,
			want:    "short-canonical",
		},
		{
			name:    "community post id fallback",
			kind:    domain.OutboxKindCommunityPost,
			payload: `{"post_id":"community-post"}`,
			want:    "community-post",
		},
		{
			name:      "content id beats non canonical payload id",
			kind:      domain.OutboxKindNewShort,
			contentID: "tracked-short",
			payload:   `{"video_id":"payload-short"}`,
			want:      "tracked-short",
		},
		{
			name:      "content id fallback",
			kind:      domain.OutboxKindCommunityPost,
			contentID: "tracked-post",
			payload:   `{}`,
			want:      "tracked-post",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, resolveTelemetryPostID(tt.kind, tt.contentID, tt.payload))
		})
	}
}
