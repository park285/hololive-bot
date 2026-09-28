package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestEnqueueRejectsMissingCanonicalIdentityFields(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		postID       string
		deliveryPath string
	}{
		{name: "missing post id", deliveryPath: CommunityShortsDeliveryPath},
		{name: "blank post id", postID: " \t\n", deliveryPath: CommunityShortsDeliveryPath},
		{name: "missing delivery path", postID: "community:content-1"},
		{name: "blank delivery path", postID: "community:content-1", deliveryPath: " \t\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo, counting, outboxID := newTelemetryEnqueueTestRepo(t)
			valid := makeEnqueueTestRow(outboxID, 101, 1)
			valid.PostID = "community:content-1"
			invalid := makeEnqueueTestRow(outboxID, 102, 1)
			invalid.PostID = tc.postID
			invalid.DeliveryPath = tc.deliveryPath
			err := repo.Enqueue(t.Context(), []domain.YouTubeNotificationDeliveryTelemetry{valid, invalid})
			require.Error(t, err)

			var count int
			require.NoError(t, counting.inner.QueryRow(t.Context(), `SELECT COUNT(*) FROM youtube_notification_delivery_telemetry`).Scan(&count))
			require.Zero(t, count, "invalid canonical identity must reject the whole batch")
		})
	}
}
