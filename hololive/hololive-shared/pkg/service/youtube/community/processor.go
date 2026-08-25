package community

import (
	"strings"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

type Batch struct {
	Posts         []*domain.YouTubeCommunityPost
	Notifications []*domain.YouTubeNotificationOutbox
	Tracking      []*domain.YouTubeContentAlarmTracking
	Watermark     *domain.YouTubeContentWatermark
}

func NormalizeKeywords(keywords []string) []string {
	if len(keywords) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(keywords))
	seen := make(map[string]struct{}, len(keywords))

	for i := range keywords {
		keyword := strings.ToLower(strings.TrimSpace(keywords[i]))
		if keyword == "" {
			continue
		}

		if _, exists := seen[keyword]; exists {
			continue
		}

		seen[keyword] = struct{}{}
		normalized = append(normalized, keyword)
	}

	return normalized
}

func MatchesKeywords(text string, keywords []string) bool {
	if len(keywords) == 0 {
		return true
	}

	lowerText := strings.ToLower(text)

	for _, keyword := range keywords {
		if strings.Contains(lowerText, keyword) {
			return true
		}
	}

	return false
}

func PayloadFromPosts(
	channelID string,
	posts []*parser.CommunityPost,
	maxResults int,
	exhausted bool,
) contract.CommunityPayloadV1 {
	mapped := make([]contract.CommunityPostV1, 0, len(posts))
	for i := range posts {
		if posts[i] == nil {
			continue
		}

		post := posts[i]

		mapped = append(mapped, contract.CommunityPostV1{
			PostID:         post.PostID,
			UpstreamPostID: post.UpstreamPostID,
			ChannelID:      channelID,
			AuthorID:       post.AuthorID,
			AuthorName:     post.AuthorName,
			AuthorPhoto:    contractThumbnails(post.AuthorPhoto),
			ContentText:    post.ContentText,
			PublishedText:  post.PublishedText,
			PublishedAt:    post.PublishedAt,
			LikeCount:      post.LikeCount,
			CommentCount:   post.CommentCount,
			Images:         contractThumbnails(post.Images),
			VideoID:        post.VideoID,
		})
	}

	return contract.CommunityPayloadV1{
		ChannelID: channelID,
		Posts:     mapped,
		Coverage: contract.CommunityPageCoverageV1{
			ChannelID:  channelID,
			MaxResults: maxResults,
			PageCount:  1,
			Exhausted:  exhausted,
		},
	}
}

func PostsFromPayload(posts []contract.CommunityPostV1) []*parser.CommunityPost {
	if len(posts) == 0 {
		return nil
	}

	mapped := make([]*parser.CommunityPost, 0, len(posts))
	for i := range posts {
		post := posts[i]

		mapped = append(mapped, &parser.CommunityPost{
			PostID:         post.PostID,
			UpstreamPostID: post.UpstreamPostID,
			AuthorID:       post.AuthorID,
			AuthorName:     post.AuthorName,
			AuthorPhoto:    parserThumbnails(post.AuthorPhoto),
			ContentText:    post.ContentText,
			PublishedText:  post.PublishedText,
			PublishedAt:    post.PublishedAt,
			LikeCount:      post.LikeCount,
			CommentCount:   post.CommentCount,
			Images:         parserThumbnails(post.Images),
			VideoID:        post.VideoID,
		})
	}

	return mapped
}

func BuildPostArtifacts(
	channelID string,
	post *parser.CommunityPost,
	initialized bool,
	detectedAt time.Time,
	keywords []string,
) (*domain.YouTubeCommunityPost, *domain.YouTubeContentAlarmTracking, *domain.YouTubeNotificationOutbox) {
	if post == nil {
		return nil, nil, nil
	}

	canonicalPostID := polling.NormalizeContentID(domain.OutboxKindCommunityPost, post.PostID)
	publishedAt := yttimestamp.NormalizePtr(post.PublishedAt)
	dbPost := &domain.YouTubeCommunityPost{
		PostID:        canonicalPostID,
		ChannelID:     channelID,
		AuthorName:    post.AuthorName,
		AuthorPhoto:   polling.ConvertThumbnails(post.AuthorPhoto),
		ContentText:   post.ContentText,
		PublishedText: post.PublishedText,
		PublishedAt:   publishedAt,
		LikeCount:     post.LikeCount,
		CommentCount:  post.CommentCount,
		Images:        polling.ConvertThumbnails(post.Images),
		AttachedVideo: post.VideoID,
	}

	if !initialized || !MatchesKeywords(post.ContentText, keywords) {
		return dbPost, nil, nil
	}

	tracking := &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         canonicalPostID,
		ChannelID:         channelID,
		ActualPublishedAt: dbPost.PublishedAt,
		DetectedAt:        detectedAt,
	}
	notification := &domain.YouTubeNotificationOutbox{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: channelID,
		ContentID: canonicalPostID,
		Payload:   polling.BuildCommunityNotificationPayload(dbPost, canonicalPostID),
		Status:    domain.OutboxStatusPending,
	}

	return dbPost, tracking, notification
}

func BuildBatch(
	channelID string,
	collected []*parser.CommunityPost,
	notificationPosts []*parser.CommunityPost,
	detectedAt time.Time,
	keywords []string,
) Batch {
	batch := buildPostBatch(channelID, collected, false, detectedAt, keywords)

	for i := range notificationPosts {
		_, tracking, notification := BuildPostArtifacts(channelID, notificationPosts[i], true, detectedAt, keywords)
		if tracking != nil {
			batch.Tracking = append(batch.Tracking, tracking)
		}

		if notification != nil {
			batch.Notifications = append(batch.Notifications, notification)
		}
	}

	batch.Watermark = buildCommunityWatermark(channelID, collected)

	return batch
}

func buildPostBatch(
	channelID string,
	posts []*parser.CommunityPost,
	notificationsEnabled bool,
	detectedAt time.Time,
	keywords []string,
) Batch {
	batch := Batch{
		Posts:         make([]*domain.YouTubeCommunityPost, 0, len(posts)),
		Notifications: make([]*domain.YouTubeNotificationOutbox, 0, len(posts)),
		Tracking:      make([]*domain.YouTubeContentAlarmTracking, 0, len(posts)),
	}
	for i := range posts {
		dbPost, tracking, notification := BuildPostArtifacts(channelID, posts[i], notificationsEnabled, detectedAt, keywords)
		if dbPost != nil {
			batch.Posts = append(batch.Posts, dbPost)
		}

		if tracking != nil {
			batch.Tracking = append(batch.Tracking, tracking)
		}

		if notification != nil {
			batch.Notifications = append(batch.Notifications, notification)
		}
	}

	return batch
}

func buildCommunityWatermark(channelID string, collected []*parser.CommunityPost) *domain.YouTubeContentWatermark {
	if len(collected) == 0 || collected[0] == nil {
		return nil
	}

	return &domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: polling.NormalizeContentID(domain.OutboxKindCommunityPost, collected[0].PostID),
	}
}

func ArtifactsFromPayload(
	payload *contract.CommunityPayloadV1,
	notifyUnseen bool,
	knownPostIDs map[string]struct{},
	detectedAt time.Time,
	keywords []string,
) Batch {
	if payload == nil {
		return Batch{}
	}

	collected := polling.NormalizeCollectedCommunityPostsByCanonicalPostID(PostsFromPayload(payload.Posts))
	notificationPosts := make([]*parser.CommunityPost, 0, len(collected))

	if notifyUnseen {
		for i := range collected {
			canonicalPostID := polling.NormalizeContentID(domain.OutboxKindCommunityPost, collected[i].PostID)
			if _, known := knownPostIDs[canonicalPostID]; !known {
				notificationPosts = append(notificationPosts, collected[i])
			}
		}
	}

	return BuildBatch(
		payload.ChannelID,
		collected,
		notificationPosts,
		detectedAt,
		keywords,
	)
}

func CanonicalPostIDs(posts []contract.CommunityPostV1) []string {
	ids := make([]string, 0, len(posts))
	seen := make(map[string]struct{}, len(posts))

	for i := range posts {
		canonicalPostID := polling.NormalizeContentID(domain.OutboxKindCommunityPost, posts[i].PostID)
		if canonicalPostID == "" {
			continue
		}

		if _, exists := seen[canonicalPostID]; exists {
			continue
		}

		seen[canonicalPostID] = struct{}{}
		ids = append(ids, canonicalPostID)
	}

	return ids
}

func parserThumbnails(thumbnails []contract.Thumbnail) []parser.Thumbnail {
	if len(thumbnails) == 0 {
		return nil
	}

	mapped := make([]parser.Thumbnail, len(thumbnails))
	for i := range thumbnails {
		mapped[i] = parser.Thumbnail{
			URL:    thumbnails[i].URL,
			Width:  thumbnails[i].Width,
			Height: thumbnails[i].Height,
		}
	}

	return mapped
}

func contractThumbnails(thumbnails []parser.Thumbnail) []contract.Thumbnail {
	if len(thumbnails) == 0 {
		return nil
	}

	mapped := make([]contract.Thumbnail, len(thumbnails))
	for i := range thumbnails {
		mapped[i] = contract.Thumbnail{
			URL:    thumbnails[i].URL,
			Width:  thumbnails[i].Width,
			Height: thumbnails[i].Height,
		}
	}

	return mapped
}
