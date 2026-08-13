package sourceobservation

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	MaxCommunityPosts      = 50
	maxCommunityThumbnails = 20
)

type CommunityPayloadV1 struct {
	ChannelID   string            `json:"channel_id"`
	CollectedAt time.Time         `json:"collected_at"`
	Posts       []CommunityPostV1 `json:"posts"`
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

func (p CommunityPayloadV1) Validate(sourceKey string) error {
	if err := validateCommunityIdentifier("channel id", p.ChannelID, 256); err != nil {
		return err
	}
	if p.ChannelID != sourceKey {
		return fmt.Errorf("validate community payload: channel id does not match source key")
	}
	if p.CollectedAt.IsZero() {
		return fmt.Errorf("validate community payload: collected at is zero")
	}
	if len(p.Posts) > MaxCommunityPosts {
		return fmt.Errorf("validate community payload: post count exceeds %d", MaxCommunityPosts)
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
	return nil
}

func (p CommunityPostV1) validate(channelID string) error {
	if err := validateCommunityIdentifier("post id", p.PostID, 256); err != nil {
		return err
	}
	if p.UpstreamPostID != "" {
		if err := validateCommunityIdentifier("upstream post id", p.UpstreamPostID, 256); err != nil {
			return err
		}
	}
	if p.ChannelID != channelID {
		return fmt.Errorf("post channel id does not match payload channel id")
	}
	if err := validateOptionalCommunityText("author id", p.AuthorID, 256); err != nil {
		return err
	}
	if err := validateOptionalCommunityText("author name", p.AuthorName, 512); err != nil {
		return err
	}
	if err := validateOptionalCommunityText("content text", p.ContentText, 100000); err != nil {
		return err
	}
	if err := validateOptionalCommunityText("published text", p.PublishedText, 512); err != nil {
		return err
	}
	if err := validateOptionalCommunityText("video id", p.VideoID, 128); err != nil {
		return err
	}
	if p.PublishedAt != nil && p.PublishedAt.IsZero() {
		return fmt.Errorf("published at must not point to the zero time")
	}
	if p.LikeCount < 0 || p.CommentCount < 0 {
		return fmt.Errorf("like and comment counts must be non-negative")
	}
	if err := validateThumbnails("author photo", p.AuthorPhoto); err != nil {
		return err
	}
	if err := validateThumbnails("images", p.Images); err != nil {
		return err
	}
	return nil
}

func validateThumbnails(name string, thumbnails []Thumbnail) error {
	if len(thumbnails) > maxCommunityThumbnails {
		return fmt.Errorf("%s count exceeds %d", name, maxCommunityThumbnails)
	}
	for i := range thumbnails {
		thumbnail := thumbnails[i]
		if err := validateHTTPSURL(name+" url", thumbnail.URL); err != nil {
			return fmt.Errorf("%s %d: %w", name, i, err)
		}
		if thumbnail.Width < 0 || thumbnail.Width > 10000 || thumbnail.Height < 0 || thumbnail.Height > 10000 {
			return fmt.Errorf("%s %d: dimensions are outside the accepted range", name, i)
		}
	}
	return nil
}

func validateHTTPSURL(name, value string) error {
	if err := validateCommunityIdentifier(name, value, 2048); err != nil {
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

func validateCommunityIdentifier(name, value string, maxLength int) error {
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

func validateOptionalCommunityText(name, value string, maxLength int) error {
	if value == "" {
		return nil
	}
	if len(value) > maxLength {
		return fmt.Errorf("%s exceeds %d bytes", name, maxLength)
	}
	return nil
}
