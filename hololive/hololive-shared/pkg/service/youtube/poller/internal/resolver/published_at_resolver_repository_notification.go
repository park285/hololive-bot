package resolver

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func newShortPublishedAtNotification(
	candidate *trackingrepo.PublishedAtResolutionCandidate,
	video *domain.YouTubeVideo,
) *domain.YouTubeNotificationOutbox {
	return &domain.YouTubeNotificationOutbox{
		Kind:      candidate.Kind,
		ChannelID: candidate.ChannelID,
		ContentID: candidate.ContentID,
		Payload:   polling.BuildShortNotificationPayload(video, candidate.PostID),
		Status:    domain.OutboxStatusPending,
	}
}

func newCommunityPublishedAtNotification(
	candidate *trackingrepo.PublishedAtResolutionCandidate,
	post *domain.YouTubeCommunityPost,
) *domain.YouTubeNotificationOutbox {
	return &domain.YouTubeNotificationOutbox{
		Kind:      candidate.Kind,
		ChannelID: candidate.ChannelID,
		ContentID: candidate.ContentID,
		Payload:   polling.BuildCommunityNotificationPayload(post, candidate.PostID),
		Status:    domain.OutboxStatusPending,
	}
}
