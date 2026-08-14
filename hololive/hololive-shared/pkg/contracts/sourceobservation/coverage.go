package sourceobservation

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type CoverageRelation uint8

const (
	CoverageDisjoint CoverageRelation = iota
	CoverageEqual
	CoverageCovers
	CoverageCoveredBy
)

type AbsenceCapability string

const (
	AbsencePositiveOnly AbsenceCapability = "POSITIVE_ONLY"
	AbsenceScoped       AbsenceCapability = "SCOPED_ABSENCE"
)

type CommunityPageCoverageV1 struct {
	ChannelID  string `json:"channel_id"`
	MaxResults int    `json:"max_results"`
	PageCount  int    `json:"page_count"`
	CursorEnd  string `json:"cursor_end,omitempty"`
	Exhausted  bool   `json:"exhausted"`
}

type VideoListFiltersV1 struct {
	PublishedAfter  *time.Time `json:"published_after,omitempty"`
	PublishedBefore *time.Time `json:"published_before,omitempty"`
	IncludeUpcoming bool       `json:"include_upcoming"`
}

type ChannelListCoverageV1 struct {
	ChannelID   string             `json:"channel_id"`
	MaxResults  int                `json:"max_results"`
	CursorStart string             `json:"cursor_start,omitempty"`
	CursorEnd   string             `json:"cursor_end,omitempty"`
	Exhausted   bool               `json:"exhausted"`
	Filters     VideoListFiltersV1 `json:"filters"`
}

type ShortsListCoverageV1 struct {
	ChannelID   string `json:"channel_id"`
	MaxResults  int    `json:"max_results"`
	CursorStart string `json:"cursor_start,omitempty"`
	CursorEnd   string `json:"cursor_end,omitempty"`
	Exhausted   bool   `json:"exhausted"`
}

type LiveFiltersV1 struct {
	Statuses []string `json:"statuses"`
}

type GlobalChannelCoverageV1 struct {
	RequestedChannelIDs []string      `json:"requested_channel_ids"`
	GroupKey            string        `json:"group_key,omitempty"`
	Filters             LiveFiltersV1 `json:"filters"`
}

type ViewerSampleCoverageV1 struct {
	VideoID             string    `json:"video_id"`
	SampleWindowStart   time.Time `json:"sample_window_start"`
	SampleWindowSeconds int       `json:"sample_window_seconds"`
}

type ChannelStatsCoverageV1 struct {
	ChannelID string   `json:"channel_id"`
	Fields    []string `json:"fields"`
}

type ChannelProfileCoverageV1 struct {
	ChannelID string   `json:"channel_id"`
	Fields    []string `json:"fields"`
}

type ChannelPhotoCoverageV1 struct {
	ChannelID string   `json:"channel_id"`
	Variants  []string `json:"variants"`
}

type ScheduleCoverageV1 struct {
	GroupKey    string     `json:"group_key"`
	WindowStart *time.Time `json:"window_start,omitempty"`
	WindowEnd   *time.Time `json:"window_end,omitempty"`
}

func (c *CommunityPageCoverageV1) normalizeAndValidate(subject string) error {
	if c.ChannelID != subject {
		return fmt.Errorf("community coverage channel does not match subject")
	}
	if err := validateIdentifier("community coverage channel", c.ChannelID, 256); err != nil {
		return err
	}
	if c.MaxResults < 1 || c.MaxResults > 1000 || c.PageCount < 1 || c.PageCount > 100 {
		return fmt.Errorf("community coverage bounds are invalid")
	}
	return validateOptionalText("community cursor", c.CursorEnd, 512)
}

func (c *ChannelListCoverageV1) normalizeAndValidate(subject string) error {
	if c.ChannelID != subject {
		return fmt.Errorf("video coverage channel does not match subject")
	}
	if err := validateIdentifier("video coverage channel", c.ChannelID, 256); err != nil {
		return err
	}
	if c.MaxResults < 1 || c.MaxResults > 1000 {
		return fmt.Errorf("video coverage max results is outside the accepted range")
	}
	if err := validateOptionalText("video cursor start", c.CursorStart, 512); err != nil {
		return err
	}
	if err := validateOptionalText("video cursor end", c.CursorEnd, 512); err != nil {
		return err
	}
	if c.Filters.PublishedAfter != nil && c.Filters.PublishedAfter.IsZero() ||
		c.Filters.PublishedBefore != nil && c.Filters.PublishedBefore.IsZero() {
		return fmt.Errorf("video coverage time bound is zero")
	}
	if c.Filters.PublishedAfter != nil && c.Filters.PublishedBefore != nil &&
		!c.Filters.PublishedAfter.Before(*c.Filters.PublishedBefore) {
		return fmt.Errorf("video coverage time range is invalid")
	}
	if err := normalizeOptionalTime(&c.Filters.PublishedAfter); err != nil {
		return fmt.Errorf("video coverage published after: %w", err)
	}
	if err := normalizeOptionalTime(&c.Filters.PublishedBefore); err != nil {
		return fmt.Errorf("video coverage published before: %w", err)
	}
	return nil
}

func (c *ShortsListCoverageV1) normalizeAndValidate(subject string) error {
	if c.ChannelID != subject {
		return fmt.Errorf("shorts coverage channel does not match subject")
	}
	if err := validateIdentifier("shorts coverage channel", c.ChannelID, 256); err != nil {
		return err
	}
	if c.MaxResults < 1 || c.MaxResults > 1000 {
		return fmt.Errorf("shorts coverage max results is outside the accepted range")
	}
	if err := validateOptionalText("shorts cursor start", c.CursorStart, 512); err != nil {
		return err
	}
	return validateOptionalText("shorts cursor end", c.CursorEnd, 512)
}

func (c *GlobalChannelCoverageV1) normalizeAndValidate(subject string) error {
	if len(c.RequestedChannelIDs) > 1000 {
		return fmt.Errorf("live coverage channel count exceeds 1000")
	}
	ids, err := normalizedUnique(c.RequestedChannelIDs, 256)
	if err != nil {
		return fmt.Errorf("live coverage channel ids: %w", err)
	}
	c.RequestedChannelIDs = ids
	if c.GroupKey != "" {
		if err := validateIdentifier("live coverage group key", c.GroupKey, 256); err != nil {
			return err
		}
		if subject != c.GroupKey {
			return fmt.Errorf("live coverage group key does not match subject")
		}
	}
	statuses, err := normalizedVocabulary(c.Filters.Statuses, []string{"CANCELLED", "ENDED", "LIVE", "UPCOMING"})
	if err != nil {
		return fmt.Errorf("live coverage statuses: %w", err)
	}
	c.Filters.Statuses = statuses
	return nil
}

func (c *ViewerSampleCoverageV1) normalizeAndValidate(subject string) error {
	if c.VideoID != subject {
		return fmt.Errorf("viewer coverage video does not match subject")
	}
	if err := validateIdentifier("viewer coverage video", c.VideoID, 128); err != nil {
		return err
	}
	if c.SampleWindowStart.IsZero() || c.SampleWindowSeconds < 1 || c.SampleWindowSeconds > 86400 {
		return fmt.Errorf("viewer sample window is invalid")
	}
	c.SampleWindowStart = c.SampleWindowStart.UTC()
	return nil
}

func (c *ChannelStatsCoverageV1) normalizeAndValidate(subject string) error {
	return normalizeChannelFields(subject, &c.ChannelID, &c.Fields, []string{"subscriber_count", "video_count", "view_count"})
}

func (c *ChannelProfileCoverageV1) normalizeAndValidate(subject string) error {
	return normalizeChannelFields(subject, &c.ChannelID, &c.Fields, []string{"country", "description", "handle", "joined_date"})
}

func (c *ChannelPhotoCoverageV1) normalizeAndValidate(subject string) error {
	return normalizeChannelFields(subject, &c.ChannelID, &c.Variants, []string{"avatar", "banner"})
}

func (c *ScheduleCoverageV1) normalizeAndValidate(subject string) error {
	if c.GroupKey != subject {
		return fmt.Errorf("schedule coverage group does not match subject")
	}
	if err := validateIdentifier("schedule coverage group", c.GroupKey, 256); err != nil {
		return err
	}
	if c.WindowStart != nil && c.WindowStart.IsZero() || c.WindowEnd != nil && c.WindowEnd.IsZero() {
		return fmt.Errorf("schedule coverage window contains zero time")
	}
	if c.WindowStart != nil && c.WindowEnd != nil && !c.WindowStart.Before(*c.WindowEnd) {
		return fmt.Errorf("schedule coverage window is invalid")
	}
	if err := normalizeOptionalTime(&c.WindowStart); err != nil {
		return fmt.Errorf("schedule coverage window start: %w", err)
	}
	if err := normalizeOptionalTime(&c.WindowEnd); err != nil {
		return fmt.Errorf("schedule coverage window end: %w", err)
	}
	return nil
}

func normalizeOptionalTime(value **time.Time) error {
	if *value == nil {
		return nil
	}
	if (*value).IsZero() {
		return fmt.Errorf("time must not point to the zero time")
	}
	normalized := (*value).UTC()
	*value = &normalized
	return nil
}

func normalizeChannelFields(subject string, channelID *string, fields *[]string, allowed []string) error {
	if *channelID != subject {
		return fmt.Errorf("channel coverage does not match subject")
	}
	if err := validateIdentifier("coverage channel", *channelID, 256); err != nil {
		return err
	}
	normalized, err := normalizedVocabulary(*fields, allowed)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return fmt.Errorf("coverage fields are empty")
	}
	*fields = normalized
	return nil
}

func normalizedUnique(values []string, maxLength int) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for i := range values {
		value := strings.TrimSpace(values[i])
		if err := validateIdentifier("value", value, maxLength); err != nil {
			return nil, err
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func normalizedVocabulary(values, allowed []string) ([]string, error) {
	result, err := normalizedUnique(values, 128)
	if err != nil {
		return nil, err
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, value := range allowed {
		allowedSet[value] = struct{}{}
	}
	for _, value := range result {
		if _, ok := allowedSet[value]; !ok {
			return nil, fmt.Errorf("unsupported value %q", value)
		}
	}
	return result, nil
}
