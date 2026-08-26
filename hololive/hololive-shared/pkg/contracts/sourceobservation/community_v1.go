package sourceobservation

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"slices"
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
	if err := validateCommunityPayloadIdentity(p, subjectKey); err != nil {
		return fmt.Errorf("validate community payload identity: %w", err)
	}

	if err := prepareCommunityPosts(p); err != nil {
		return fmt.Errorf("prepare community posts: %w", err)
	}

	if err := validateCommunityPosts(p); err != nil {
		return fmt.Errorf("validate community posts: %w", err)
	}

	slices.SortFunc(p.Posts, func(left, right CommunityPostV1) int {
		return cmp.Compare(left.PostID, right.PostID)
	})

	return nil
}

func validateCommunityPayloadIdentity(p *CommunityPayloadV1, subjectKey string) error {
	if err := validateIdentifier("channel id", p.ChannelID, 256); err != nil {
		return fmt.Errorf("validate identifier: %w", err)
	}

	if p.ChannelID != subjectKey {
		return errors.New("validate community payload: channel id does not match subject key")
	}

	if err := p.Coverage.normalizeAndValidate(subjectKey); err != nil {
		return fmt.Errorf("normalize and validate: %w", err)
	}

	return nil
}

func prepareCommunityPosts(p *CommunityPayloadV1) error {
	if len(p.Posts) > MaxCommunityPosts {
		return fmt.Errorf("validate community payload: post count exceeds %d", MaxCommunityPosts)
	}

	if p.Posts == nil {
		p.Posts = []CommunityPostV1{}
	}

	return nil
}

func validateCommunityPosts(p *CommunityPayloadV1) error {
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

func (p *CommunityPayloadV1) Validate(subjectKey string) error {
	if err := p.normalizeAndValidate(subjectKey); err != nil {
		return fmt.Errorf("normalize and validate: %w", err)
	}

	return nil
}

func (p *CommunityPostV1) validate(channelID string) error {
	if err := validateCommunityPostIdentity(p, channelID); err != nil {
		return fmt.Errorf("validate community post identity: %w", err)
	}

	if err := validateCommunityPostText(p); err != nil {
		return fmt.Errorf("validate community post text: %w", err)
	}

	if err := validateCommunityPostTimesAndCounts(p); err != nil {
		return fmt.Errorf("validate community post times and counts: %w", err)
	}

	if err := validateThumbnails("author photo", p.AuthorPhoto); err != nil {
		return fmt.Errorf("validate thumbnails: %w", err)
	}

	if err := validateThumbnails("images", p.Images); err != nil {
		return fmt.Errorf("validate thumbnails: %w", err)
	}

	return nil
}

func validateCommunityPostIdentity(p *CommunityPostV1, channelID string) error {
	if err := validateIdentifier("post id", p.PostID, 256); err != nil {
		return fmt.Errorf("validate identifier: %w", err)
	}

	if err := validateOptionalIdentifier("upstream post id", p.UpstreamPostID, 256); err != nil {
		return fmt.Errorf("validate optional identifier: %w", err)
	}

	if p.ChannelID != channelID {
		return errors.New("post channel id does not match payload channel id")
	}

	return nil
}

func validateCommunityPostText(p *CommunityPostV1) error {
	for name, value := range map[string]string{
		"author id": p.AuthorID, "author name": p.AuthorName, "content text": p.ContentText,
		"published text": p.PublishedText, "video id": p.VideoID,
	} {
		if err := validateOptionalText(name, value, communityTextLimit(name)); err != nil {
			return fmt.Errorf("validate optional text: %w", err)
		}
	}

	return nil
}

func communityTextLimit(name string) int {
	if name == "content text" {
		return 100000
	}

	if name == "video id" {
		return 128
	}

	return 512
}

func validateCommunityPostTimesAndCounts(p *CommunityPostV1) error {
	if p.PublishedAt != nil && p.PublishedAt.IsZero() {
		return errors.New("published at must not point to the zero time")
	}

	if err := normalizeOptionalTime(&p.PublishedAt); err != nil {
		return fmt.Errorf("published at: %w", err)
	}

	if p.LikeCount < 0 || p.CommentCount < 0 {
		return errors.New("like and comment counts must be non-negative")
	}

	return nil
}

func validateThumbnails(name string, thumbnails []Thumbnail) error {
	if len(thumbnails) > maxCommunityThumbnails {
		return fmt.Errorf("%s count exceeds %d", name, maxCommunityThumbnails)
	}

	for i := range thumbnails {
		if err := validateThumbnail(name, i, thumbnails[i]); err != nil {
			return fmt.Errorf("validate thumbnail: %w", err)
		}
	}

	slices.SortFunc(thumbnails, compareThumbnail)

	return nil
}

func validateThumbnail(name string, index int, thumbnail Thumbnail) error {
	if err := validateHTTPSURL(name+" url", thumbnail.URL); err != nil {
		return fmt.Errorf("%s %d: %w", name, index, err)
	}

	if thumbnail.Width < 0 || thumbnail.Width > 10000 || thumbnail.Height < 0 || thumbnail.Height > 10000 {
		return fmt.Errorf("%s %d: dimensions are outside the accepted range", name, index)
	}

	return nil
}

func compareThumbnail(left, right Thumbnail) int {
	return cmp.Or(
		cmp.Compare(left.URL, right.URL),
		cmp.Compare(left.Width, right.Width),
		cmp.Compare(left.Height, right.Height),
	)
}

func validateHTTPSURL(name, value string) error {
	if err := validateIdentifier(name, value, 2048); err != nil {
		return fmt.Errorf("validate identifier: %w", err)
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
