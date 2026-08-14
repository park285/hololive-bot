package sourceobservation

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	MaxCommunityPosts      = 50
	maxCommunityThumbnails = 20
)

type CommunityPayloadV1 struct {
	ChannelID string                  `json:"channel_id"`
	Posts     []CommunityPostV1       `json:"posts"`
	Coverage  CommunityPageCoverageV1 `json:"coverage"`
}

type CommunityPostV1 struct {
	PostID         string      `json:"post_id"`
	UpstreamPostID string      `json:"upstream_post_id,omitempty"`
	ChannelID      string      `json:"channel_id"`
	AuthorID       string      `json:"author_id,omitempty"`
	AuthorName     string      `json:"author_name,omitempty"`
	AuthorPhoto    []Thumbnail `json:"author_photo,omitempty"`
	ContentText    string      `json:"content_text,omitempty"`
	PublishedText  string      `json:"published_text,omitempty"`
	PublishedAt    *time.Time  `json:"published_at,omitempty"`
	LikeCount      int64       `json:"like_count"`
	CommentCount   int64       `json:"comment_count"`
	Images         []Thumbnail `json:"images,omitempty"`
	VideoID        string      `json:"video_id,omitempty"`
}

type Thumbnail struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func (p *CommunityPayloadV1) normalizeAndValidate(subjectKey string) error {
	if err := validateIdentifier("channel id", p.ChannelID, 256); err != nil {
		return err
	}
	if p.ChannelID != subjectKey {
		return fmt.Errorf("validate community payload: channel id does not match subject key")
	}
	if err := p.Coverage.normalizeAndValidate(subjectKey); err != nil {
		return err
	}
	if len(p.Posts) > MaxCommunityPosts {
		return fmt.Errorf("validate community payload: post count exceeds %d", MaxCommunityPosts)
	}
	if p.Posts == nil {
		p.Posts = []CommunityPostV1{}
	}
	seen := make(map[string]struct{}, len(p.Posts))
	for i := range p.Posts {
		if err := p.Posts[i].validate(p.ChannelID); err != nil {
			return fmt.Errorf("validate community payload: post %d: %w", i, err)
		}
		if _, exists := seen[p.Posts[i].PostID]; exists {
			return fmt.Errorf("validate community payload: duplicate post id %q", p.Posts[i].PostID)
		}
		seen[p.Posts[i].PostID] = struct{}{}
	}
	sort.Slice(p.Posts, func(i, j int) bool { return p.Posts[i].PostID < p.Posts[j].PostID })
	return nil
}

func (p CommunityPayloadV1) Validate(subjectKey string) error {
	return p.normalizeAndValidate(subjectKey)
}

func (p CommunityPostV1) validate(channelID string) error {
	if err := validateIdentifier("post id", p.PostID, 256); err != nil {
		return err
	}
	if p.UpstreamPostID != "" {
		if err := validateIdentifier("upstream post id", p.UpstreamPostID, 256); err != nil {
			return err
		}
	}
	if p.ChannelID != channelID {
		return fmt.Errorf("post channel id does not match payload channel id")
	}
	for name, value := range map[string]string{
		"author id": p.AuthorID, "author name": p.AuthorName, "content text": p.ContentText,
		"published text": p.PublishedText, "video id": p.VideoID,
	} {
		limit := 512
		if name == "content text" {
			limit = 100000
		}
		if name == "video id" {
			limit = 128
		}
		if err := validateOptionalText(name, value, limit); err != nil {
			return err
		}
	}
	if p.PublishedAt != nil && p.PublishedAt.IsZero() {
		return fmt.Errorf("published at must not point to the zero time")
	}
	if err := normalizeOptionalTime(&p.PublishedAt); err != nil {
		return fmt.Errorf("published at: %w", err)
	}
	if p.LikeCount < 0 || p.CommentCount < 0 {
		return fmt.Errorf("like and comment counts must be non-negative")
	}
	if err := validateThumbnails("author photo", p.AuthorPhoto); err != nil {
		return err
	}
	return validateThumbnails("images", p.Images)
}

func validateThumbnails(name string, thumbnails []Thumbnail) error {
	if len(thumbnails) > maxCommunityThumbnails {
		return fmt.Errorf("%s count exceeds %d", name, maxCommunityThumbnails)
	}
	for i := range thumbnails {
		if err := validateHTTPSURL(name+" url", thumbnails[i].URL); err != nil {
			return fmt.Errorf("%s %d: %w", name, i, err)
		}
		if thumbnails[i].Width < 0 || thumbnails[i].Width > 10000 ||
			thumbnails[i].Height < 0 || thumbnails[i].Height > 10000 {
			return fmt.Errorf("%s %d: dimensions are outside the accepted range", name, i)
		}
	}
	sort.Slice(thumbnails, func(i, j int) bool {
		if thumbnails[i].URL != thumbnails[j].URL {
			return thumbnails[i].URL < thumbnails[j].URL
		}
		if thumbnails[i].Width != thumbnails[j].Width {
			return thumbnails[i].Width < thumbnails[j].Width
		}
		return thumbnails[i].Height < thumbnails[j].Height
	})
	return nil
}

func validateHTTPSURL(name, value string) error {
	if err := validateIdentifier(name, value, 2048); err != nil {
		return err
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s is invalid: %w", name, err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("%s must be an absolute HTTPS URL without user information", name)
	}
	return nil
}

func validateIdentifier(name, value string, maxLength int) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is empty", name)
	}
	if trimmed != value {
		return fmt.Errorf("%s must not contain surrounding whitespace", name)
	}
	if len(value) > maxLength {
		return fmt.Errorf("%s exceeds %d bytes", name, maxLength)
	}
	return nil
}

func validateOptionalText(name, value string, maxLength int) error {
	if value == "" {
		return nil
	}
	if len(value) > maxLength {
		return fmt.Errorf("%s exceeds %d bytes", name, maxLength)
	}
	if !strings.ContainsAny(value, "\n\r\t") && strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain surrounding whitespace", name)
	}
	return nil
}
