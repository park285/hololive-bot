package batchrepo

import (
	"strings"

	"github.com/park285/shared-go/pkg/json"
	"github.com/prometheus/client_golang/prometheus"

	ytcontentid "github.com/kapu/hololive-shared/internal/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var outboxInsertTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "youtube_poller_outbox_insert_total_test",
	Help: "test-only outbox insert counter",
}, []string{"kind", "result"})

type shortNotificationPayload struct {
	domain.YouTubeVideo
	CanonicalPostID string `json:"canonical_post_id"`
}

type communityNotificationPayload struct {
	domain.YouTubeCommunityPost
	CanonicalPostID string `json:"canonical_post_id"`
}

func init() {
	ObserveOutboxInsert = func(kind domain.OutboxKind, result string, count int64) {
		if count > 0 {
			outboxInsertTotal.WithLabelValues(string(kind), result).Add(float64(count))
		}
	}
}

func buildShortNotificationPayload(video *domain.YouTubeVideo, canonicalPostID string) string {
	if video == nil {
		return "{}"
	}
	return mustMarshalJSON(shortNotificationPayload{
		YouTubeVideo:    *video,
		CanonicalPostID: normalizeNotificationCanonicalPostID(domain.OutboxKindNewShort, canonicalPostID),
	})
}

func buildCommunityNotificationPayload(post *domain.YouTubeCommunityPost, canonicalPostID string) string {
	if post == nil {
		return "{}"
	}
	payloadPost := *post
	payloadPost.PostID = normalizeCommunityResourceID(payloadPost.PostID)
	return mustMarshalJSON(communityNotificationPayload{
		YouTubeCommunityPost: payloadPost,
		CanonicalPostID:      normalizeNotificationCanonicalPostID(domain.OutboxKindCommunityPost, canonicalPostID),
	})
}

func normalizeNotificationCanonicalPostID(kind domain.OutboxKind, id string) string {
	canonicalID, err := ytcontentid.ForOutboxKind(kind, id)
	if err != nil {
		return strings.TrimSpace(id)
	}
	return canonicalID
}

func normalizeCommunityResourceID(id string) string {
	normalized, err := ytcontentid.NormalizeCommunityPostID(id)
	if err != nil {
		return strings.TrimSpace(id)
	}
	return normalized
}

func mustMarshalJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
