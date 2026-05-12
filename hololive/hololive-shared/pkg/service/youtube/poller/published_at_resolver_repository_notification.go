package poller

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func newShortPublishedAtNotification(
	candidate trackingrepo.PublishedAtResolutionCandidate,
	video *domain.YouTubeVideo,
) *domain.YouTubeNotificationOutbox {
	return &domain.YouTubeNotificationOutbox{
		Kind:      candidate.Kind,
		ChannelID: candidate.ChannelID,
		ContentID: candidate.ContentID,
		Payload:   buildShortNotificationPayload(video, candidate.PostID),
		Status:    domain.OutboxStatusPending,
	}
}

func newCommunityPublishedAtNotification(
	candidate trackingrepo.PublishedAtResolutionCandidate,
	post *domain.YouTubeCommunityPost,
) *domain.YouTubeNotificationOutbox {
	return &domain.YouTubeNotificationOutbox{
		Kind:      candidate.Kind,
		ChannelID: candidate.ChannelID,
		ContentID: candidate.ContentID,
		Payload:   buildCommunityNotificationPayload(post, candidate.PostID),
		Status:    domain.OutboxStatusPending,
	}
}
