package canonicalwrite

import (
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

type sourcePostKey struct {
	kind   domain.OutboxKind
	postID string
}

func buildShortSourcePosts(videos []*domain.YouTubeVideo, trackingRows []*domain.YouTubeContentAlarmTracking) []*domain.YouTubeCommunityShortsSourcePost {
	rowsByKey := make(map[sourcePostKey]*domain.YouTubeCommunityShortsSourcePost, len(videos)+len(trackingRows))
	fallbackDetectedAt := yttimestamp.Normalize(time.Now())

	addTrackingSourcePosts(rowsByKey, trackingRows, domain.OutboxKindNewShort)

	for i := range videos {
		if videos[i] == nil || !videos[i].IsShort {
			continue
		}

		addContentSourcePost(
			rowsByKey,
			domain.OutboxKindNewShort,
			videos[i].VideoID,
			videos[i].ChannelID,
			videos[i].PublishedAt,
			fallbackDetectedAt,
		)
	}

	return flattenSourcePosts(rowsByKey)
}

func buildCommunitySourcePosts(posts []*domain.YouTubeCommunityPost, trackingRows []*domain.YouTubeContentAlarmTracking) []*domain.YouTubeCommunityShortsSourcePost {
	rowsByKey := make(map[sourcePostKey]*domain.YouTubeCommunityShortsSourcePost, len(posts)+len(trackingRows))
	fallbackDetectedAt := yttimestamp.Normalize(time.Now())

	addTrackingSourcePosts(rowsByKey, trackingRows, domain.OutboxKindCommunityPost)

	for i := range posts {
		if posts[i] == nil {
			continue
		}

		addContentSourcePost(
			rowsByKey,
			domain.OutboxKindCommunityPost,
			posts[i].PostID,
			posts[i].ChannelID,
			posts[i].PublishedAt,
			fallbackDetectedAt,
		)
	}

	return flattenSourcePosts(rowsByKey)
}

func addTrackingSourcePosts(
	rowsByKey map[sourcePostKey]*domain.YouTubeCommunityShortsSourcePost,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	kind domain.OutboxKind,
) {
	for i := range trackingRows {
		if trackingRows[i] == nil || trackingRows[i].Kind != kind {
			continue
		}

		postID := normalizeSourcePostID(kind, trackingRows[i].ContentID)
		if postID == "" {
			continue
		}

		rowsByKey[sourcePostKey{kind: kind, postID: postID}] = &domain.YouTubeCommunityShortsSourcePost{
			Kind:              kind,
			PostID:            postID,
			ChannelID:         strings.TrimSpace(trackingRows[i].ChannelID),
			ActualPublishedAt: yttimestamp.NormalizePtr(trackingRows[i].ActualPublishedAt),
			DetectedAt:        yttimestamp.Normalize(trackingRows[i].DetectedAt),
		}
	}
}

func addContentSourcePost(
	rowsByKey map[sourcePostKey]*domain.YouTubeCommunityShortsSourcePost,
	kind domain.OutboxKind,
	rawPostID string,
	channelID string,
	publishedAt *time.Time,
	fallbackDetectedAt time.Time,
) {
	postID := normalizeSourcePostID(kind, rawPostID)
	if postID == "" {
		return
	}

	key := sourcePostKey{kind: kind, postID: postID}
	if row, ok := rowsByKey[key]; ok {
		if row.ActualPublishedAt == nil {
			row.ActualPublishedAt = yttimestamp.NormalizePtr(publishedAt)
		}

		if row.ChannelID == "" {
			row.ChannelID = strings.TrimSpace(channelID)
		}

		return
	}

	rowsByKey[key] = &domain.YouTubeCommunityShortsSourcePost{
		Kind:              kind,
		PostID:            postID,
		ChannelID:         strings.TrimSpace(channelID),
		ActualPublishedAt: yttimestamp.NormalizePtr(publishedAt),
		DetectedAt:        fallbackDetectedAt,
	}
}

func flattenSourcePosts(rowsByKey map[sourcePostKey]*domain.YouTubeCommunityShortsSourcePost) []*domain.YouTubeCommunityShortsSourcePost {
	rows := make([]*domain.YouTubeCommunityShortsSourcePost, 0, len(rowsByKey))
	for _, row := range rowsByKey {
		if row == nil {
			continue
		}

		rows = append(rows, row)
	}

	return rows
}

func normalizeSourcePostID(kind domain.OutboxKind, postID string) string {
	normalizedPostID := strings.TrimSpace(postID)
	if normalizedPostID == "" {
		return ""
	}

	canonicalPostID, err := ytcontentid.ForOutboxKind(kind, normalizedPostID)
	if err == nil && strings.TrimSpace(canonicalPostID) != "" {
		return canonicalPostID
	}

	return normalizedPostID
}
