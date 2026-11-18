package community

import (
	jsonv2 "encoding/json/v2"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/contracts/youtubeoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

type Batch struct {
	Posts         []*domain.YouTubeCommunityPost
	Notifications []*domain.YouTubeNotificationOutbox
	Tracking      []*domain.YouTubeContentAlarmTracking
	Watermark     *domain.YouTubeContentWatermark
}

type Window struct {
	channelID string
	posts     []*domain.YouTubeCommunityPost
}

func NormalizeWindow(payload *contract.CommunityPayloadV1) Window {
	posts := make([]*domain.YouTubeCommunityPost, 0, len(payload.Posts))
	indexByPostID := make(map[string]int, len(payload.Posts))

	for i := range payload.Posts {
		src := &payload.Posts[i]

		postID := canonicalPostID(src.PostID)
		if postID == "" {
			continue
		}

		if idx, ok := indexByPostID[postID]; ok {
			mergePost(posts[idx], src)

			continue
		}

		indexByPostID[postID] = len(posts)
		posts = append(posts, &domain.YouTubeCommunityPost{
			PostID:        postID,
			ChannelID:     payload.ChannelID,
			AuthorName:    src.AuthorName,
			AuthorPhoto:   thumbnails(src.AuthorPhoto),
			ContentText:   src.ContentText,
			PublishedText: src.PublishedText,
			PublishedAt:   yttimestamp.NormalizePtr(src.PublishedAt),
			LikeCount:     src.LikeCount,
			CommentCount:  src.CommentCount,
			Images:        thumbnails(src.Images),
			AttachedVideo: src.VideoID,
		})
	}

	return Window{channelID: payload.ChannelID, posts: posts}
}

func (w Window) CanonicalPostIDs() []string {
	ids := make([]string, len(w.posts))
	for i := range w.posts {
		ids[i] = w.posts[i].PostID
	}

	return ids
}

func (w Window) Artifacts(notifyUnseen bool, knownPostIDs map[string]struct{}, detectedAt time.Time) Batch {
	batch := Batch{Posts: w.posts, Watermark: w.watermark()}

	if !notifyUnseen {
		return batch
	}

	for _, post := range w.posts {
		if _, known := knownPostIDs[post.PostID]; known {
			continue
		}

		batch.Tracking = append(batch.Tracking, &domain.YouTubeContentAlarmTracking{
			Kind:              domain.OutboxKindCommunityPost,
			ContentID:         post.PostID,
			ChannelID:         w.channelID,
			ActualPublishedAt: post.PublishedAt,
			DetectedAt:        detectedAt,
		})
		batch.Notifications = append(batch.Notifications, &domain.YouTubeNotificationOutbox{
			Kind:      domain.OutboxKindCommunityPost,
			ChannelID: w.channelID,
			ContentID: post.PostID,
			Payload:   notificationPayload(post),
			Status:    domain.OutboxStatusPending,
		})
	}

	return batch
}

func (w Window) watermark() *domain.YouTubeContentWatermark {
	if len(w.posts) == 0 {
		return nil
	}

	return &domain.YouTubeContentWatermark{
		ChannelID:     w.channelID,
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: w.posts[0].PostID,
	}
}

func mergePost(dst *domain.YouTubeCommunityPost, src *contract.CommunityPostV1) {
	if dst.AuthorName == "" {
		dst.AuthorName = src.AuthorName
	}

	if len(dst.AuthorPhoto) == 0 && len(src.AuthorPhoto) > 0 {
		dst.AuthorPhoto = thumbnails(src.AuthorPhoto)
	}

	if dst.ContentText == "" {
		dst.ContentText = src.ContentText
	}

	if dst.PublishedText == "" {
		dst.PublishedText = src.PublishedText
	}

	if dst.PublishedAt == nil && src.PublishedAt != nil {
		dst.PublishedAt = yttimestamp.NormalizePtr(src.PublishedAt)
	}

	if dst.LikeCount == 0 && src.LikeCount != 0 {
		dst.LikeCount = src.LikeCount
	}

	if dst.CommentCount == 0 && src.CommentCount != 0 {
		dst.CommentCount = src.CommentCount
	}

	if len(dst.Images) == 0 && len(src.Images) > 0 {
		dst.Images = thumbnails(src.Images)
	}

	if dst.AttachedVideo == "" {
		dst.AttachedVideo = src.VideoID
	}
}

func thumbnails(src []contract.Thumbnail) domain.ThumbnailsJSON {
	if len(src) == 0 {
		return nil
	}

	mapped := make(domain.ThumbnailsJSON, len(src))
	for i := range src {
		mapped[i] = domain.ThumbnailEntry{URL: src[i].URL, Width: src[i].Width, Height: src[i].Height}
	}

	return mapped
}

func notificationPayload(post *domain.YouTubeCommunityPost) string {
	payloadPost := *post

	payloadPost.PostID = communityResourceID(post.PostID)

	data, err := jsonv2.Marshal(youtubeoutbox.NewCommunity(&payloadPost, canonicalPostID(post.PostID)))
	if err != nil {
		panic(err)
	}

	return string(data)
}

func canonicalPostID(raw string) string {
	postID, err := ytcontentid.ForCommunity(raw)
	if err != nil {
		return ""
	}

	return postID
}

func communityResourceID(raw string) string {
	resourceID, err := ytcontentid.NormalizeCommunityPostID(raw)
	if err != nil {
		return ""
	}

	return resourceID
}
