package scraper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

// GetCommunityPosts: 채널의 커뮤니티 포스트 목록 조회 (/channel/{id}/posts)
// 2025년 8월 YouTube URL 변경: /community → /posts
func (c *Client) GetCommunityPosts(ctx context.Context, channelID string, maxResults int) ([]*CommunityPost, error) {
	if c.isCommunityMissing(ctx, channelID) {
		return []*CommunityPost{}, nil
	}

	url := fmt.Sprintf("https://www.youtube.com/channel/%s/posts", channelID)
	html, err := c.fetchPage(ctx, url)
	if err != nil {
		if statusCode, ok := extractHTTPStatusCode(err); ok && statusCode == http.StatusNotFound {
			c.markCommunityMissing(ctx, channelID)
			slog.Info("community posts endpoint missing; channel temporarily skipped",
				"channel_id", channelID)
			return []*CommunityPost{}, nil
		}
		return nil, err
	}
	c.clearCommunityMissing(ctx, channelID)

	jsonStr, err := extractYtInitialData(html)
	if err != nil {
		return nil, fmt.Errorf("failed to extract ytInitialData: %w", err)
	}

	data := gjson.Parse(jsonStr)
	if err := checkAlerts(data); err != nil {
		if errors.Is(err, ErrChannelNotFound) {
			c.markCommunityMissing(ctx, channelID)
		}
		return nil, err
	}

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

	if !postsContent.Exists() {
		c.markCommunityMissing(ctx, channelID)
		return nil, nil
	}

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

	return posts, nil
}

// parseBackstagePost: backstagePostRenderer JSON을 CommunityPost 구조체로 변환
func (c *Client) parseBackstagePost(post gjson.Result) *CommunityPost {
	postID := post.Get("postId").String()
	if postID == "" {
		return nil
	}

	var authorPhoto []Thumbnail
	post.Get("authorThumbnail.thumbnails").ForEach(func(_, t gjson.Result) bool {
		authorPhoto = append(authorPhoto, Thumbnail{
			URL:    t.Get("url").String(),
			Width:  int(t.Get("width").Int()),
			Height: int(t.Get("height").Int()),
		})
		return true
	})

	// 본문 텍스트 추출
	var contentBuilder strings.Builder
	post.Get("contentText.runs").ForEach(func(_, run gjson.Result) bool {
		contentBuilder.WriteString(run.Get("text").String())
		return true
	})
	contentText := contentBuilder.String()

	// 좋아요 수 파싱
	likeCountText := post.Get("voteCount.simpleText").String()
	likeCount := parseShortNumber(likeCountText)

	// 이미지 첨부
	var images []Thumbnail
	post.Get("backstageAttachment.backstageImageRenderer.image.thumbnails").ForEach(func(_, t gjson.Result) bool {
		images = append(images, Thumbnail{
			URL:    t.Get("url").String(),
			Width:  int(t.Get("width").Int()),
			Height: int(t.Get("height").Int()),
		})
		return true
	})

	// 첨부 비디오
	videoID := post.Get("backstageAttachment.videoRenderer.videoId").String()

	return &CommunityPost{
		PostID:        postID,
		AuthorID:      post.Get("authorEndpoint.browseEndpoint.browseId").String(),
		AuthorName:    post.Get("authorText.runs.0.text").String(),
		AuthorPhoto:   authorPhoto,
		ContentText:   contentText,
		PublishedText: post.Get("publishedTimeText.runs.0.text").String(),
		LikeCount:     likeCount,
		CommentCount:  post.Get("actionButtons.commentActionButtonsRenderer.replyButton.buttonRenderer.text.simpleText").Int(),
		Images:        images,
		VideoID:       videoID,
	}
}
