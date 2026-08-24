package sourceobservation

import (
	"errors"
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
		return errors.New("community coverage channel does not match subject")
	}

	if err := validateIdentifier("community coverage channel", c.ChannelID, 256); err != nil {
		return fmt.Errorf("validate identifier: %w", err)
	}

	if c.MaxResults < 1 || c.MaxResults > 1000 || c.PageCount < 1 || c.PageCount > 100 {
		return errors.New("community coverage bounds are invalid")
	}

	if err := validateOptionalText("community cursor", c.CursorEnd, 512); err != nil {
		return fmt.Errorf("validate optional text: %w", err)
	}

	return nil
}

func (c *ChannelListCoverageV1) normalizeAndValidate(subject string) error {
	if err := validateChannelListCoverageIdentity(c, subject); err != nil {
		return fmt.Errorf("validate channel list coverage identity: %w", err)
	}

	if err := normalizeChannelListCoverageTimes(c); err != nil {
		return fmt.Errorf("normalize channel list coverage times: %w", err)
	}

	return nil
}

func validateChannelListCoverageIdentity(c *ChannelListCoverageV1, subject string) error {
	if c.ChannelID != subject {
		return errors.New("video coverage channel does not match subject")
	}

	if err := validateIdentifier("video coverage channel", c.ChannelID, 256); err != nil {
		return fmt.Errorf("validate identifier: %w", err)
	}

	if c.MaxResults < 1 || c.MaxResults > 1000 {
		return errors.New("video coverage max results is outside the accepted range")
	}

	if err := validateOptionalText("video cursor start", c.CursorStart, 512); err != nil {
		return fmt.Errorf("validate optional text: %w", err)
	}

	if err := validateOptionalText("video cursor end", c.CursorEnd, 512); err != nil {
		return fmt.Errorf("validate optional text: %w", err)
	}

	return nil
}

func normalizeChannelListCoverageTimes(c *ChannelListCoverageV1) error {
	if optionalTimeIsZero(c.Filters.PublishedAfter) || optionalTimeIsZero(c.Filters.PublishedBefore) {
		return errors.New("video coverage time bound is zero")
	}

	if invalidOptionalTimeRange(c.Filters.PublishedAfter, c.Filters.PublishedBefore) {
		return errors.New("video coverage time range is invalid")
	}

	if err := normalizeOptionalTime(&c.Filters.PublishedAfter); err != nil {
		return fmt.Errorf("video coverage published after: %w", err)
	}

	if err := normalizeOptionalTime(&c.Filters.PublishedBefore); err != nil {
		return fmt.Errorf("video coverage published before: %w", err)
	}

	return nil
}

func invalidOptionalTimeRange(start, end *time.Time) bool {
	return start != nil && end != nil && !start.Before(*end)
}

func (c *ShortsListCoverageV1) normalizeAndValidate(subject string) error {
	if c.ChannelID != subject {
		return errors.New("shorts coverage channel does not match subject")
	}

	if err := validateIdentifier("shorts coverage channel", c.ChannelID, 256); err != nil {
		return fmt.Errorf("validate identifier: %w", err)
	}

	if c.MaxResults < 1 || c.MaxResults > 1000 {
		return errors.New("shorts coverage max results is outside the accepted range")
	}

	if err := validateOptionalText("shorts cursor start", c.CursorStart, 512); err != nil {
		return fmt.Errorf("validate optional text: %w", err)
	}

	if err := validateOptionalText("shorts cursor end", c.CursorEnd, 512); err != nil {
		return fmt.Errorf("validate optional text: %w", err)
	}

	return nil
}

func (c *GlobalChannelCoverageV1) normalizeAndValidate(subject string) error {
	if len(c.RequestedChannelIDs) > 1000 {
		return errors.New("live coverage channel count exceeds 1000")
	}

	ids, err := normalizedUnique(c.RequestedChannelIDs, 256)
	if err != nil {
		return fmt.Errorf("live coverage channel ids: %w", err)
	}

	c.RequestedChannelIDs = ids
	if c.GroupKey != "" {
		if validateErr := validateIdentifier("live coverage group key", c.GroupKey, 256); validateErr != nil {
			return fmt.Errorf("validate identifier: %w", validateErr)
		}

		if subject != c.GroupKey {
			return errors.New("live coverage group key does not match subject")
		}
	}

	statuses, err := normalizedVocabulary(c.Filters.Statuses, []string{"CANCELLED", "ENDED", "LIVE", "UPCOMING"}) //nolint:misspell // YouTube 방송 상태 계약값이 영국식 CANCELLED라, canceled로 바꾸면 상태 판정이 어긋난다.
	if err != nil {
		return fmt.Errorf("live coverage statuses: %w", err)
	}

	c.Filters.Statuses = statuses

	return nil
}

func (c *ViewerSampleCoverageV1) normalizeAndValidate(subject string) error {
	if c.VideoID != subject {
		return errors.New("viewer coverage video does not match subject")
	}

	if err := validateIdentifier("viewer coverage video", c.VideoID, 128); err != nil {
		return fmt.Errorf("validate identifier: %w", err)
	}

	if c.SampleWindowStart.IsZero() || c.SampleWindowSeconds < 1 || c.SampleWindowSeconds > 86400 {
		return errors.New("viewer sample window is invalid")
	}

	c.SampleWindowStart = c.SampleWindowStart.UTC()

	return nil
}

func (c *ChannelStatsCoverageV1) normalizeAndValidate(subject string) error {
	if err := normalizeChannelFields(subject, &c.ChannelID, &c.Fields, []string{"subscriber_count", "video_count", "view_count"}); err != nil {
		return fmt.Errorf("normalize channel fields: %w", err)
	}

	return nil
}

func (c *ChannelProfileCoverageV1) normalizeAndValidate(subject string) error {
	if err := normalizeChannelFields(subject, &c.ChannelID, &c.Fields, []string{"country", "description", "handle", "joined_date"}); err != nil {
		return fmt.Errorf("normalize channel fields: %w", err)
	}

	return nil
}

func (c *ChannelPhotoCoverageV1) normalizeAndValidate(subject string) error {
	if err := normalizeChannelFields(subject, &c.ChannelID, &c.Variants, []string{"avatar", "banner"}); err != nil {
		return fmt.Errorf("normalize channel fields: %w", err)
	}

	return nil
}

func (c *ScheduleCoverageV1) normalizeAndValidate(subject string) error {
	if c.GroupKey != subject {
		return errors.New("schedule coverage group does not match subject")
	}

	if err := validateIdentifier("schedule coverage group", c.GroupKey, 256); err != nil {
		return fmt.Errorf("validate identifier: %w", err)
	}

	if err := normalizeScheduleCoverageWindow(c); err != nil {
		return fmt.Errorf("normalize schedule coverage window: %w", err)
	}

	return nil
}

func normalizeScheduleCoverageWindow(c *ScheduleCoverageV1) error {
	if optionalTimeIsZero(c.WindowStart) || optionalTimeIsZero(c.WindowEnd) {
		return errors.New("schedule coverage window contains zero time")
	}

	if invalidOptionalTimeRange(c.WindowStart, c.WindowEnd) {
		return errors.New("schedule coverage window is invalid")
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
		return errors.New("time must not point to the zero time")
	}

	normalized := (*value).UTC()

	*value = &normalized

	return nil
}

func normalizeChannelFields(subject string, channelID *string, fields *[]string, allowed []string) error {
	if *channelID != subject {
		return errors.New("channel coverage does not match subject")
	}

	if err := validateIdentifier("coverage channel", *channelID, 256); err != nil {
		return fmt.Errorf("validate identifier: %w", err)
	}

	normalized, err := normalizedVocabulary(*fields, allowed)
	if err != nil {
		return fmt.Errorf("normalized vocabulary: %w", err)
	}

	if len(normalized) == 0 {
		return errors.New("coverage fields are empty")
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
			return nil, fmt.Errorf("validate identifier: %w", err)
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
		return nil, fmt.Errorf("normalized unique: %w", err)
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
