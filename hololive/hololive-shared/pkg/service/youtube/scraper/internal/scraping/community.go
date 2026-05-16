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

package scraping

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
)

var (
	communityPostIDJSONPattern = regexp.MustCompile(`"postId"\s*:\s*"([^"]+)"`)
	communityPostURLPattern    = regexp.MustCompile(`/post/([^"?#&/]+)`)
)

// 2025년 8월 YouTube URL 변경: /community → /posts
func (c *Client) GetCommunityPosts(ctx context.Context, channelID string, maxResults int) ([]*CommunityPost, error) {
	if c.isCommunityMissing(ctx, channelID) {
		return []*CommunityPost{}, nil
	}

	html, missing, err := c.fetchCommunityPostsPage(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if missing {
		return []*CommunityPost{}, nil
	}
	c.clearCommunityMissing(ctx, channelID)

	jsonStr, err := extractYtInitialData(html)
	if err != nil {
		logStructureWarning("community_posts", channelID, "ytInitialData extraction failed", "error", err)
		url := fmt.Sprintf("https://www.youtube.com/channel/%s/posts", channelID)
		return nil, c.recordParserDrift(ctx, "community_posts", "extract_yt_initial_data", channelID, url, FailureSourceHTML, html, err)
	}

	data := gjson.Parse(jsonStr)
	if err := c.checkCommunityPostAlerts(ctx, channelID, data); err != nil {
		return nil, err
	}

	postsContent := extractCommunityPostsContent(data)
	if !postsContent.Exists() {
		c.markCommunityMissing(ctx, channelID)
		return nil, nil
	}

	posts := c.parseCommunityPosts(postsContent, maxResults)
	c.recordChannelSourceSuccess(ctx, channelID, FailureSourceHTML)
	return posts, nil
}

func (c *Client) fetchCommunityPostsPage(ctx context.Context, channelID string) (string, bool, error) {
	url := fmt.Sprintf("https://www.youtube.com/channel/%s/posts", channelID)
	if err := c.ensureChannelSourceAllowed(ctx, channelID, FailureSourceHTML); err != nil {
		return "", false, err
	}
	html, err := c.fetchPage(ctx, url, HighFrequencyChannelFetchPolicy)
	if statusCode, ok := extractHTTPStatusCode(err); ok && statusCode == http.StatusNotFound {
		c.markCommunityMissing(ctx, channelID)
		slog.Info("community posts endpoint missing; channel temporarily skipped",
			"channel_id", channelID)
		return "", true, nil
	}
	if err != nil {
		delay := c.recordChannelSourceFailure(ctx, channelID, ClassifyFailure(err, FailureSourceHTML))
		return "", false, channelSourceCooldownError(FailureSourceHTML, delay, err)
	}
	if strings.TrimSpace(html) == "" {
		err := fmt.Errorf("community_posts empty response from %s", url)
		delay := c.recordChannelSourceFailure(ctx, channelID, ClassifyFailure(err, FailureSourceHTML))
		return "", false, channelSourceCooldownError(FailureSourceHTML, delay, err)
	}
	return html, false, nil
}

func (c *Client) checkCommunityPostAlerts(ctx context.Context, channelID string, data gjson.Result) error {
	if err := checkAlerts(data); err != nil {
		if errors.Is(err, ErrChannelNotFound) {
			c.markCommunityMissing(ctx, channelID)
		}
		return err
	}
	return nil
}

func extractCommunityPostsContent(data gjson.Result) gjson.Result {
	tabPath := "contents.twoColumnBrowseResultsRenderer.tabs"
	var postsContent gjson.Result
	data.Get(tabPath).ForEach(func(_, tab gjson.Result) bool {
		tabTitle := tab.Get("tabRenderer.title").String()
		if tabTitle == "Posts" || tabTitle == "Community" {
			postsContent = tab.Get("tabRenderer.content.sectionListRenderer.contents.0.itemSectionRenderer.contents")
			return false
		}
		return true
	})
	return postsContent
}

func (c *Client) parseCommunityPosts(postsContent gjson.Result, maxResults int) []*CommunityPost {
	posts := make([]*CommunityPost, 0, maxResults)
	postsContent.ForEach(func(_, content gjson.Result) bool {
		if len(posts) >= maxResults {
			return false
		}
		postThread := content.Get("backstagePostThreadRenderer.post.backstagePostRenderer")
		if !postThread.Exists() {
			return true
		}

		post := c.parseBackstagePost(postThread)
		if post != nil {
			posts = append(posts, post)
		}
		return true
	})
	return posts
}

// parseBackstagePost: backstagePostRenderer JSON을 CommunityPost 구조체로 변환
func (c *Client) parseBackstagePost(post gjson.Result) *CommunityPost {
	upstreamPostID := extractCommunityPostID(post)
	if upstreamPostID == "" {
		return nil
	}

	authorPhoto := extractThumbnails(post.Get("authorThumbnail.thumbnails"))

	// 본문 텍스트 추출
	contentText := extractRunText(post.Get("contentText.runs"))

	// 좋아요 수 파싱
	likeCountText := post.Get("voteCount.simpleText").String()
	likeCount := parseShortNumber(likeCountText)

	// 이미지 첨부
	images := extractThumbnails(post.Get("backstageAttachment.backstageImageRenderer.image.thumbnails"))

	// 첨부 비디오
	videoID := post.Get("backstageAttachment.videoRenderer.videoId").String()
	publishedText := firstNonEmptyString(
		post.Get("publishedTimeText.simpleText").String(),
		post.Get("publishedTimeText.runs.0.text").String(),
	)
	publishedAt, _ := normalizePublishedAtCandidate(publishedText)

	return &CommunityPost{
		PostID:         upstreamPostID,
		UpstreamPostID: upstreamPostID,
		AuthorID:       post.Get("authorEndpoint.browseEndpoint.browseId").String(),
		AuthorName:     post.Get("authorText.runs.0.text").String(),
		AuthorPhoto:    authorPhoto,
		ContentText:    contentText,
		PublishedText:  publishedText,
		PublishedAt:    publishedAt,
		LikeCount:      likeCount,
		CommentCount:   post.Get("actionButtons.commentActionButtonsRenderer.replyButton.buttonRenderer.text.simpleText").Int(),
		Images:         images,
		VideoID:        videoID,
	}
}

func extractThumbnails(thumbnails gjson.Result) []Thumbnail {
	var extracted []Thumbnail
	thumbnails.ForEach(func(_, t gjson.Result) bool {
		extracted = append(extracted, Thumbnail{
			URL:    t.Get("url").String(),
			Width:  int(t.Get("width").Int()),
			Height: int(t.Get("height").Int()),
		})
		return true
	})
	return extracted
}

func extractRunText(runs gjson.Result) string {
	var contentBuilder strings.Builder
	runs.ForEach(func(_, run gjson.Result) bool {
		contentBuilder.WriteString(run.Get("text").String())
		return true
	})
	return contentBuilder.String()
}

func extractCommunityPostID(post gjson.Result) string {
	for _, candidate := range []string{
		post.Get("postId").String(),
		post.Get("navigationEndpoint.commandMetadata.webCommandMetadata.url").String(),
		post.Get("actionButtons.commentActionButtonsRenderer.replyButton.buttonRenderer.navigationEndpoint.commandMetadata.webCommandMetadata.url").String(),
	} {
		if postID := extractCommunityPostIDFromCandidate(candidate); postID != "" {
			return postID
		}
	}

	return extractCommunityPostIDFromRaw(post.Raw)
}

func extractCommunityPostIDFromCandidate(value string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(value, `\/`, `/`))
	if normalized == "" {
		return ""
	}

	if postID := extractCommunityPostIDFromURL(normalized); postID != "" {
		return postID
	}

	return normalized
}

func extractCommunityPostIDFromRaw(raw string) string {
	normalized := strings.ReplaceAll(raw, `\/`, `/`)
	if matches := communityPostIDJSONPattern.FindStringSubmatch(normalized); len(matches) == 2 {
		return strings.TrimSpace(matches[1])
	}

	return extractCommunityPostIDFromURL(normalized)
}

func extractCommunityPostIDFromURL(value string) string {
	matches := communityPostURLPattern.FindStringSubmatch(value)
	if len(matches) != 2 {
		return ""
	}

	return strings.TrimSpace(matches[1])
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
