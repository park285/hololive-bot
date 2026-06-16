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

package polling

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

var numericRe = regexp.MustCompile(`[\d.]+`)

func IsLiveReplayVideo(publishedText string) bool {
	lower := strings.ToLower(publishedText)
	return strings.Contains(lower, "streamed") || strings.Contains(lower, "premiered")
}

// convertThumbnails: scraper.Thumbnail을 domain.ThumbnailsJSON으로 변환
func ConvertThumbnails(thumbnails []scraper.Thumbnail) domain.ThumbnailsJSON {
	if len(thumbnails) == 0 {
		return nil
	}

	result := make(domain.ThumbnailsJSON, len(thumbnails))
	for i, t := range thumbnails {
		result[i] = domain.ThumbnailEntry{
			URL:    t.URL,
			Width:  t.Width,
			Height: t.Height,
		}
	}
	return result
}

// mustMarshalJSON: JSON 마샬링 (에러 시 빈 객체)
func MustMarshalJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

type shortNotificationPayload struct {
	domain.YouTubeVideo
	CanonicalPostID string `json:"canonical_post_id"`
}

type communityNotificationPayload struct {
	domain.YouTubeCommunityPost
	CanonicalPostID string `json:"canonical_post_id"`
}

func normalizeNotificationCanonicalPostID(kind domain.OutboxKind, id string) string {
	canonicalID, err := ytcontentid.ForOutboxKind(kind, id)
	if err != nil {
		return strings.TrimSpace(id)
	}
	return canonicalID
}

func NormalizeContentID(kind domain.OutboxKind, id string) string {
	trimmed := strings.TrimSpace(id)
	switch kind {
	case domain.OutboxKindNewShort, domain.OutboxKindCommunityPost:
		normalized, err := ytcontentid.ForOutboxKind(kind, trimmed)
		if err != nil {
			return trimmed
		}
		return normalized
	default:
		return trimmed
	}
}

func NormalizeShortVideoResourceID(id string) string {
	normalized, err := ytcontentid.NormalizeShortVideoID(id)
	if err != nil {
		return strings.TrimSpace(id)
	}
	return normalized
}

func NormalizeCommunityResourceID(id string) string {
	normalized, err := ytcontentid.NormalizeCommunityPostID(id)
	if err != nil {
		return strings.TrimSpace(id)
	}
	return normalized
}

func NormalizeCollectedShortsByCanonicalPostID(shorts []*scraper.Short) []*scraper.Short {
	if len(shorts) == 0 {
		return nil
	}

	normalized := make([]*scraper.Short, 0, len(shorts))
	indexByPostID := make(map[string]int, len(shorts))
	for _, short := range shorts {
		if short == nil {
			continue
		}

		canonicalPostID := NormalizeContentID(domain.OutboxKindNewShort, short.VideoID)
		if canonicalPostID == "" {
			copyShort := *short
			normalized = append(normalized, &copyShort)
			continue
		}
		if idx, ok := indexByPostID[canonicalPostID]; ok {
			mergeCollectedShort(normalized[idx], short)
			continue
		}

		copyShort := *short
		normalized = append(normalized, &copyShort)
		indexByPostID[canonicalPostID] = len(normalized) - 1
	}

	return normalized
}

func mergeCollectedShort(dst *scraper.Short, src *scraper.Short) {
	if dst == nil || src == nil {
		return
	}
	if dst.Title == "" {
		dst.Title = src.Title
	}
	mergeShortThumbnail(dst, src)
	if dst.ViewCount == 0 && src.ViewCount != 0 {
		dst.ViewCount = src.ViewCount
	}
	mergeShortPublishedAt(dst, src)
}

func mergeShortThumbnail(dst *scraper.Short, src *scraper.Short) {
	if len(dst.Thumbnail) == 0 && len(src.Thumbnail) > 0 {
		dst.Thumbnail = append([]scraper.Thumbnail(nil), src.Thumbnail...)
	}
}

func mergeShortPublishedAt(dst *scraper.Short, src *scraper.Short) {
	if dst.PublishedAt == nil && src.PublishedAt != nil {
		publishedAt := *src.PublishedAt
		dst.PublishedAt = &publishedAt
	}
}

func NormalizeCollectedCommunityPostsByCanonicalPostID(posts []*scraper.CommunityPost) []*scraper.CommunityPost {
	if len(posts) == 0 {
		return nil
	}

	normalized := make([]*scraper.CommunityPost, 0, len(posts))
	indexByPostID := make(map[string]int, len(posts))
	for _, post := range posts {
		if post == nil {
			continue
		}

		canonicalPostID := NormalizeContentID(domain.OutboxKindCommunityPost, post.PostID)
		if canonicalPostID == "" {
			copyPost := *post
			normalized = append(normalized, &copyPost)
			continue
		}
		if idx, ok := indexByPostID[canonicalPostID]; ok {
			mergeCollectedCommunityPost(normalized[idx], post)
			continue
		}

		copyPost := *post
		normalized = append(normalized, &copyPost)
		indexByPostID[canonicalPostID] = len(normalized) - 1
	}

	return normalized
}

func mergeCollectedCommunityPost(dst *scraper.CommunityPost, src *scraper.CommunityPost) {
	if dst == nil || src == nil {
		return
	}
	mergeCommunityPostIdentityFields(dst, src)
	mergeCommunityPostTextFields(dst, src)
	mergeCommunityPostStatsFields(dst, src)
	mergeCommunityPostAttachmentFields(dst, src)
}

func mergeCommunityPostIdentityFields(dst *scraper.CommunityPost, src *scraper.CommunityPost) {
	if dst.UpstreamPostID == "" {
		dst.UpstreamPostID = src.UpstreamPostID
	}
	if dst.AuthorID == "" {
		dst.AuthorID = src.AuthorID
	}
	if dst.AuthorName == "" {
		dst.AuthorName = src.AuthorName
	}
	if len(dst.AuthorPhoto) == 0 && len(src.AuthorPhoto) > 0 {
		dst.AuthorPhoto = append([]scraper.Thumbnail(nil), src.AuthorPhoto...)
	}
}

func mergeCommunityPostTextFields(dst *scraper.CommunityPost, src *scraper.CommunityPost) {
	if dst.ContentText == "" {
		dst.ContentText = src.ContentText
	}
	if dst.PublishedText == "" {
		dst.PublishedText = src.PublishedText
	}
	if dst.PublishedAt == nil && src.PublishedAt != nil {
		publishedAt := *src.PublishedAt
		dst.PublishedAt = &publishedAt
	}
}

func mergeCommunityPostStatsFields(dst *scraper.CommunityPost, src *scraper.CommunityPost) {
	if dst.LikeCount == 0 && src.LikeCount != 0 {
		dst.LikeCount = src.LikeCount
	}
	if dst.CommentCount == 0 && src.CommentCount != 0 {
		dst.CommentCount = src.CommentCount
	}
}

func mergeCommunityPostAttachmentFields(dst *scraper.CommunityPost, src *scraper.CommunityPost) {
	if len(dst.Images) == 0 && len(src.Images) > 0 {
		dst.Images = append([]scraper.Thumbnail(nil), src.Images...)
	}
	if dst.VideoID == "" {
		dst.VideoID = src.VideoID
	}
}

func BuildShortNotificationPayload(video *domain.YouTubeVideo, canonicalPostID string) string {
	if video == nil {
		return "{}"
	}

	return MustMarshalJSON(shortNotificationPayload{
		YouTubeVideo:    *video,
		CanonicalPostID: normalizeNotificationCanonicalPostID(domain.OutboxKindNewShort, canonicalPostID),
	})
}

func BuildCommunityNotificationPayload(post *domain.YouTubeCommunityPost, canonicalPostID string) string {
	if post == nil {
		return "{}"
	}

	payloadPost := *post
	payloadPost.PostID = NormalizeCommunityResourceID(payloadPost.PostID)

	return MustMarshalJSON(communityNotificationPayload{
		YouTubeCommunityPost: payloadPost,
		CanonicalPostID:      normalizeNotificationCanonicalPostID(domain.OutboxKindCommunityPost, canonicalPostID),
	})
}

// parseViewerCount: 시청자 수 텍스트 파싱
// "12,345 watching" -> 12345
// "1.2K watching" -> 1200
func ParseViewerCount(text string) int {
	if text == "" {
		return 0
	}

	// "watching", "viewers" 등 제거
	text = strings.ToLower(text)
	text = strings.ReplaceAll(text, ",", "")
	text = strings.ReplaceAll(text, " watching", "")
	text = strings.ReplaceAll(text, " viewers", "")
	text = strings.ReplaceAll(text, " waiting", "")
	text = strings.TrimSpace(text)

	// K, M 처리
	multiplier := 1.0
	if strings.HasSuffix(text, "k") {
		multiplier = 1000
		text = strings.TrimSuffix(text, "k")
	} else if strings.HasSuffix(text, "m") {
		multiplier = 1000000
		text = strings.TrimSuffix(text, "m")
	}

	// 숫자 추출
	match := numericRe.FindString(text)
	if match == "" {
		return 0
	}

	val, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0
	}

	return int(val * multiplier)
}
