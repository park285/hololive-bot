// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package batchrepo

import (
	"fmt"
	"time"

	"github.com/park285/shared-go/pkg/json"

	ytcontentid "github.com/kapu/hololive-shared/internal/service/youtube/contentid"
	yttimestamp "github.com/kapu/hololive-shared/internal/service/youtube/timestamp"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type communityNotificationPublishedAtPayload struct {
	CanonicalPostID string     `json:"canonical_post_id"`
	PostID          string     `json:"post_id"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
}

type shortNotificationPublishedAtPayload struct {
	CanonicalPostID string     `json:"canonical_post_id"`
	VideoID         string     `json:"video_id"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
}

func validateCanonicalNotificationIdentity(kind domain.OutboxKind, contentID, payloadID, canonicalPostID string) error {
	wantCanonicalContentID, err := ytcontentid.ForOutboxKind(kind, contentID)
	if err != nil {
		return fmt.Errorf("normalize content id: %w", err)
	}
	gotPayloadCanonicalID, err := ytcontentid.ForOutboxKind(kind, payloadID)
	if err != nil {
		return fmt.Errorf("normalize payload resource id: %w", err)
	}
	gotCanonicalPostID, err := ytcontentid.ForOutboxKind(kind, canonicalPostID)
	if err != nil {
		return fmt.Errorf("normalize canonical_post_id: %w", err)
	}

	if gotPayloadCanonicalID != wantCanonicalContentID {
		return fmt.Errorf("payload resource id mismatch: got %s want %s", gotPayloadCanonicalID, wantCanonicalContentID)
	}
	if gotCanonicalPostID != wantCanonicalContentID {
		return fmt.Errorf("payload canonical_post_id mismatch: got %s want %s", gotCanonicalPostID, wantCanonicalContentID)
	}

	return nil
}

func validateShortNotificationPublishedAt(videos []*domain.YouTubeVideo, notifications []*domain.YouTubeNotificationOutbox) error {
	if len(videos) == 0 || len(notifications) == 0 {
		return nil
	}

	videosByID := shortVideosByNotificationID(videos)
	if len(videosByID) == 0 {
		return nil
	}

	for _, notification := range notifications {
		if err := validateShortPublishedAtNotification(videosByID, notification); err != nil {
			return err
		}
	}

	return nil
}

func shortVideosByNotificationID(videos []*domain.YouTubeVideo) map[string]*domain.YouTubeVideo {
	videosByID := make(map[string]*domain.YouTubeVideo, len(videos)*2)
	for _, video := range videos {
		if video == nil || video.VideoID == "" {
			continue
		}
		videosByID[video.VideoID] = video
		videosByID[normalizeContentID(domain.OutboxKindNewShort, video.VideoID)] = video
	}
	return videosByID
}

func validateShortPublishedAtNotification(videosByID map[string]*domain.YouTubeVideo, notification *domain.YouTubeNotificationOutbox) error {
	if notification == nil || notification.Kind != domain.OutboxKindNewShort {
		return nil
	}
	video, ok := videosByID[notification.ContentID]
	if !ok {
		return nil
	}

	var payload shortNotificationPublishedAtPayload
	if err := json.Unmarshal([]byte(notification.Payload), &payload); err != nil {
		return fmt.Errorf("video %s: unmarshal payload: %w", video.VideoID, err)
	}
	if err := validateCanonicalNotificationIdentity(notification.Kind, notification.ContentID, payload.VideoID, payload.CanonicalPostID); err != nil {
		return fmt.Errorf("video %s: %w", video.VideoID, err)
	}
	return validateShortPublishedAtPayload(video, payload)
}

func validateShortPublishedAtPayload(video *domain.YouTubeVideo, payload shortNotificationPublishedAtPayload) error {
	if video.PublishedAt == nil {
		if payload.PublishedAt != nil {
			return fmt.Errorf("video %s: payload published_at set while video record is empty", video.VideoID)
		}
		return nil
	}
	if payload.PublishedAt == nil {
		return fmt.Errorf("video %s: payload missing published_at", video.VideoID)
	}

	wantPublishedAt := yttimestamp.Format(*video.PublishedAt)
	gotPublishedAt := payload.PublishedAt.Format(yttimestamp.Canonical.Layout)
	if gotPublishedAt != wantPublishedAt {
		return fmt.Errorf("video %s: payload published_at mismatch: got %s want %s", video.VideoID, gotPublishedAt, wantPublishedAt)
	}
	return nil
}

func validateCommunityNotificationPublishedAt(posts []*domain.YouTubeCommunityPost, notifications []*domain.YouTubeNotificationOutbox) error {
	if len(posts) == 0 || len(notifications) == 0 {
		return nil
	}

	postsByID := communityPostsByNotificationID(posts)
	if len(postsByID) == 0 {
		return nil
	}

	for _, notification := range notifications {
		if err := validateCommunityPublishedAtNotification(postsByID, notification); err != nil {
			return err
		}
	}

	return nil
}

func communityPostsByNotificationID(posts []*domain.YouTubeCommunityPost) map[string]*domain.YouTubeCommunityPost {
	postsByID := make(map[string]*domain.YouTubeCommunityPost, len(posts))
	for _, post := range posts {
		if post == nil || post.PostID == "" {
			continue
		}
		postsByID[post.PostID] = post
	}
	return postsByID
}

func validateCommunityPublishedAtNotification(postsByID map[string]*domain.YouTubeCommunityPost, notification *domain.YouTubeNotificationOutbox) error {
	if notification == nil || notification.Kind != domain.OutboxKindCommunityPost {
		return nil
	}
	post, ok := postsByID[notification.ContentID]
	if !ok {
		return nil
	}

	var payload communityNotificationPublishedAtPayload
	if err := json.Unmarshal([]byte(notification.Payload), &payload); err != nil {
		return fmt.Errorf("post %s: unmarshal payload: %w", post.PostID, err)
	}
	if err := validateCanonicalNotificationIdentity(notification.Kind, notification.ContentID, payload.PostID, payload.CanonicalPostID); err != nil {
		return fmt.Errorf("post %s: %w", post.PostID, err)
	}
	return validateCommunityPublishedAtPayload(post, payload)
}

func validateCommunityPublishedAtPayload(post *domain.YouTubeCommunityPost, payload communityNotificationPublishedAtPayload) error {
	if post.PublishedAt == nil {
		if payload.PublishedAt != nil {
			return fmt.Errorf("post %s: payload published_at set while post record is empty", post.PostID)
		}
		return nil
	}
	if payload.PublishedAt == nil {
		return fmt.Errorf("post %s: payload missing published_at", post.PostID)
	}

	wantPublishedAt := yttimestamp.Format(*post.PublishedAt)
	gotPublishedAt := payload.PublishedAt.Format(yttimestamp.Canonical.Layout)
	if gotPublishedAt != wantPublishedAt {
		return fmt.Errorf("post %s: payload published_at mismatch: got %s want %s", post.PostID, gotPublishedAt, wantPublishedAt)
	}
	return nil
}
