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

package poller

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

	for i := range trackingRows {
		if trackingRows[i] == nil || trackingRows[i].Kind != domain.OutboxKindNewShort {
			continue
		}
		postID := normalizeSourcePostID(domain.OutboxKindNewShort, trackingRows[i].ContentID)
		if postID == "" {
			continue
		}
		rowsByKey[sourcePostKey{kind: domain.OutboxKindNewShort, postID: postID}] = &domain.YouTubeCommunityShortsSourcePost{
			Kind:              domain.OutboxKindNewShort,
			PostID:            postID,
			ChannelID:         strings.TrimSpace(trackingRows[i].ChannelID),
			ActualPublishedAt: yttimestamp.NormalizePtr(trackingRows[i].ActualPublishedAt),
			DetectedAt:        yttimestamp.Normalize(trackingRows[i].DetectedAt),
		}
	}

	for i := range videos {
		if videos[i] == nil || !videos[i].IsShort {
			continue
		}
		postID := normalizeSourcePostID(domain.OutboxKindNewShort, videos[i].VideoID)
		if postID == "" {
			continue
		}
		key := sourcePostKey{kind: domain.OutboxKindNewShort, postID: postID}
		if row, ok := rowsByKey[key]; ok {
			if row.ActualPublishedAt == nil {
				row.ActualPublishedAt = yttimestamp.NormalizePtr(videos[i].PublishedAt)
			}
			if row.ChannelID == "" {
				row.ChannelID = strings.TrimSpace(videos[i].ChannelID)
			}
			continue
		}
		rowsByKey[key] = &domain.YouTubeCommunityShortsSourcePost{
			Kind:              domain.OutboxKindNewShort,
			PostID:            postID,
			ChannelID:         strings.TrimSpace(videos[i].ChannelID),
			ActualPublishedAt: yttimestamp.NormalizePtr(videos[i].PublishedAt),
			DetectedAt:        fallbackDetectedAt,
		}
	}

	return flattenSourcePosts(rowsByKey)
}

func buildCommunitySourcePosts(posts []*domain.YouTubeCommunityPost, trackingRows []*domain.YouTubeContentAlarmTracking) []*domain.YouTubeCommunityShortsSourcePost {
	rowsByKey := make(map[sourcePostKey]*domain.YouTubeCommunityShortsSourcePost, len(posts)+len(trackingRows))
	fallbackDetectedAt := yttimestamp.Normalize(time.Now())

	for i := range trackingRows {
		if trackingRows[i] == nil || trackingRows[i].Kind != domain.OutboxKindCommunityPost {
			continue
		}
		postID := normalizeSourcePostID(domain.OutboxKindCommunityPost, trackingRows[i].ContentID)
		if postID == "" {
			continue
		}
		rowsByKey[sourcePostKey{kind: domain.OutboxKindCommunityPost, postID: postID}] = &domain.YouTubeCommunityShortsSourcePost{
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            postID,
			ChannelID:         strings.TrimSpace(trackingRows[i].ChannelID),
			ActualPublishedAt: yttimestamp.NormalizePtr(trackingRows[i].ActualPublishedAt),
			DetectedAt:        yttimestamp.Normalize(trackingRows[i].DetectedAt),
		}
	}

	for i := range posts {
		if posts[i] == nil {
			continue
		}
		postID := normalizeSourcePostID(domain.OutboxKindCommunityPost, posts[i].PostID)
		if postID == "" {
			continue
		}
		key := sourcePostKey{kind: domain.OutboxKindCommunityPost, postID: postID}
		if row, ok := rowsByKey[key]; ok {
			if row.ActualPublishedAt == nil {
				row.ActualPublishedAt = yttimestamp.NormalizePtr(posts[i].PublishedAt)
			}
			if row.ChannelID == "" {
				row.ChannelID = strings.TrimSpace(posts[i].ChannelID)
			}
			continue
		}
		rowsByKey[key] = &domain.YouTubeCommunityShortsSourcePost{
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            postID,
			ChannelID:         strings.TrimSpace(posts[i].ChannelID),
			ActualPublishedAt: yttimestamp.NormalizePtr(posts[i].PublishedAt),
			DetectedAt:        fallbackDetectedAt,
		}
	}

	return flattenSourcePosts(rowsByKey)
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
