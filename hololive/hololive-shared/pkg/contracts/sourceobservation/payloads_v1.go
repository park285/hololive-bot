package sourceobservation

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type VideoListItemV1 struct {
	VideoID      string     `json:"video_id"`
	ChannelID    string     `json:"channel_id"`
	Title        string     `json:"title"`
	PublishedAt  *time.Time `json:"published_at,omitempty"`
	ScheduledFor *time.Time `json:"scheduled_for,omitempty"`
}

type VideoListV1 struct {
	ChannelID string                `json:"channel_id"`
	Videos    []VideoListItemV1     `json:"videos"`
	Coverage  ChannelListCoverageV1 `json:"coverage"`
}

type ShortsListV1 struct {
	ChannelID string               `json:"channel_id"`
	Videos    []VideoListItemV1    `json:"videos"`
	Coverage  ShortsListCoverageV1 `json:"coverage"`
}

type LiveSessionV1 struct {
	VideoID     string     `json:"video_id"`
	ChannelID   string     `json:"channel_id"`
	Status      string     `json:"status"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
}

type LiveSnapshotV1 struct {
	Sessions []LiveSessionV1         `json:"sessions"`
	Coverage GlobalChannelCoverageV1 `json:"coverage"`
}

type ViewerSampleV1 struct {
	VideoID             string                 `json:"video_id"`
	ViewerCount         *int64                 `json:"viewer_count,omitempty"`
	Availability        string                 `json:"availability"`
	SampleWindowStart   time.Time              `json:"sample_window_start"`
	SampleWindowSeconds int                    `json:"sample_window_seconds"`
	Coverage            ViewerSampleCoverageV1 `json:"coverage"`
}

type ChannelStatsV1 struct {
	ChannelID       string                 `json:"channel_id"`
	SubscriberCount *int64                 `json:"subscriber_count,omitempty"`
	ViewCount       *int64                 `json:"view_count,omitempty"`
	VideoCount      *int64                 `json:"video_count,omitempty"`
	Coverage        ChannelStatsCoverageV1 `json:"coverage"`
}

type FieldValue[T any] struct {
	Present bool `json:"present"`
	Value   T    `json:"value"`
}

type ChannelProfileV1 struct {
	ChannelID   string                   `json:"channel_id"`
	Handle      FieldValue[string]       `json:"handle"`
	Description FieldValue[string]       `json:"description"`
	Country     FieldValue[string]       `json:"country"`
	JoinedDate  FieldValue[string]       `json:"joined_date"`
	Coverage    ChannelProfileCoverageV1 `json:"coverage"`
}

type PhotoVariantV1 struct {
	Kind               string `json:"kind"`
	URL                string `json:"url"`
	Width              int    `json:"width"`
	Height             int    `json:"height"`
	StableMediaID      string `json:"stable_media_id,omitempty"`
	ContentFingerprint string `json:"content_fingerprint,omitempty"`
}

type ChannelPhotoV1 struct {
	ChannelID string                 `json:"channel_id"`
	Variants  []PhotoVariantV1       `json:"variants"`
	Coverage  ChannelPhotoCoverageV1 `json:"coverage"`
}

type ScheduleItemV1 struct {
	ExternalID  string     `json:"external_id"`
	VideoID     string     `json:"video_id,omitempty"`
	ChannelID   string     `json:"channel_id,omitempty"`
	Title       string     `json:"title"`
	ScheduledAt time.Time  `json:"scheduled_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	IsLive      bool       `json:"is_live"`
}

type ScheduleSnapshotV1 struct {
	GroupKey string             `json:"group_key"`
	Items    []ScheduleItemV1   `json:"items"`
	Coverage ScheduleCoverageV1 `json:"coverage"`
}

func canonicalPayloadAndScope(kind ObservationKind, subjectKey string, completeness Completeness, raw []byte) ([]byte, []byte, error) {
	if len(raw) == 0 || len(raw) > MaxPayloadBytes {
		return nil, nil, fmt.Errorf("payload size is outside the accepted range")
	}
	var payload any
	var coverage any
	switch kind {
	case KindCommunityPage:
		value := CommunityPayloadV1{}
		if err := decodeStrictJSON(raw, &value); err != nil {
			return nil, nil, fmt.Errorf("decode community payload: %w", err)
		}
		if err := value.normalizeAndValidate(subjectKey); err != nil {
			return nil, nil, err
		}
		if err := validatePaginatedCompleteness(kind, completeness, value.Coverage.Exhausted); err != nil {
			return nil, nil, err
		}
		payload, coverage = value, value.Coverage
	case KindVideoList:
		value := VideoListV1{}
		if err := decodeStrictJSON(raw, &value); err != nil {
			return nil, nil, fmt.Errorf("decode video list payload: %w", err)
		}
		if err := value.normalizeAndValidate(subjectKey); err != nil {
			return nil, nil, err
		}
		if err := validatePaginatedCompleteness(kind, completeness, value.Coverage.Exhausted); err != nil {
			return nil, nil, err
		}
		payload, coverage = value, value.Coverage
	case KindShortsList:
		value := ShortsListV1{}
		if err := decodeStrictJSON(raw, &value); err != nil {
			return nil, nil, fmt.Errorf("decode shorts list payload: %w", err)
		}
		if err := value.normalizeAndValidate(subjectKey); err != nil {
			return nil, nil, err
		}
		if err := validatePaginatedCompleteness(kind, completeness, value.Coverage.Exhausted); err != nil {
			return nil, nil, err
		}
		payload, coverage = value, value.Coverage
	case KindLiveSnapshot:
		value := LiveSnapshotV1{}
		if err := decodeStrictJSON(raw, &value); err != nil {
			return nil, nil, fmt.Errorf("decode live snapshot payload: %w", err)
		}
		if err := value.normalizeAndValidate(subjectKey); err != nil {
			return nil, nil, err
		}
		payload, coverage = value, value.Coverage
	case KindViewerSample:
		value := ViewerSampleV1{}
		if err := decodeStrictJSON(raw, &value); err != nil {
			return nil, nil, fmt.Errorf("decode viewer sample payload: %w", err)
		}
		if err := value.normalizeAndValidate(subjectKey); err != nil {
			return nil, nil, err
		}
		payload, coverage = value, value.Coverage
	case KindChannelStats:
		value := ChannelStatsV1{}
		if err := decodeStrictJSON(raw, &value); err != nil {
			return nil, nil, fmt.Errorf("decode channel stats payload: %w", err)
		}
		if err := value.normalizeAndValidate(subjectKey); err != nil {
			return nil, nil, err
		}
		payload, coverage = value, value.Coverage
	case KindChannelProfile:
		value := ChannelProfileV1{}
		if err := decodeStrictJSON(raw, &value); err != nil {
			return nil, nil, fmt.Errorf("decode channel profile payload: %w", err)
		}
		if err := value.normalizeAndValidate(subjectKey); err != nil {
			return nil, nil, err
		}
		payload, coverage = value, value.Coverage
	case KindChannelPhoto:
		value := ChannelPhotoV1{}
		if err := decodeStrictJSON(raw, &value); err != nil {
			return nil, nil, fmt.Errorf("decode channel photo payload: %w", err)
		}
		if err := value.normalizeAndValidate(subjectKey); err != nil {
			return nil, nil, err
		}
		payload, coverage = value, value.Coverage
	case KindSchedule:
		value := ScheduleSnapshotV1{}
		if err := decodeStrictJSON(raw, &value); err != nil {
			return nil, nil, fmt.Errorf("decode schedule payload: %w", err)
		}
		if err := value.normalizeAndValidate(subjectKey); err != nil {
			return nil, nil, err
		}
		payload, coverage = value, value.Coverage
	default:
		return nil, nil, fmt.Errorf("unsupported observation kind %q", kind)
	}
	canonicalPayload, err := canonicalJSON(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("canonicalize payload: %w", err)
	}
	canonicalScope, err := canonicalJSON(coverage)
	if err != nil {
		return nil, nil, fmt.Errorf("canonicalize coverage: %w", err)
	}
	return canonicalPayload, canonicalScope, nil
}

func validatePaginatedCompleteness(kind ObservationKind, completeness Completeness, exhausted bool) error {
	if completeness == CompletenessComplete && !exhausted {
		return fmt.Errorf("%s payload cannot be COMPLETE when coverage is not exhausted", kind)
	}
	return nil
}

func (p *VideoListV1) normalizeAndValidate(subject string) error {
	if p.ChannelID != subject {
		return fmt.Errorf("video list channel does not match subject")
	}
	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return err
	}
	if err := normalizeVideos(p.ChannelID, &p.Videos); err != nil {
		return err
	}
	for i := range p.Videos {
		publishedAt := p.Videos[i].PublishedAt
		if publishedAt == nil {
			continue
		}
		if p.Coverage.Filters.PublishedAfter != nil && publishedAt.Before(*p.Coverage.Filters.PublishedAfter) ||
			p.Coverage.Filters.PublishedBefore != nil && publishedAt.After(*p.Coverage.Filters.PublishedBefore) {
			return fmt.Errorf("video published time is outside coverage")
		}
	}
	return nil
}

func (p *ShortsListV1) normalizeAndValidate(subject string) error {
	if p.ChannelID != subject {
		return fmt.Errorf("shorts list channel does not match subject")
	}
	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return err
	}
	return normalizeVideos(p.ChannelID, &p.Videos)
}

func normalizeVideos(channelID string, videos *[]VideoListItemV1) error {
	if len(*videos) > 1000 {
		return fmt.Errorf("video count exceeds 1000")
	}
	if *videos == nil {
		*videos = []VideoListItemV1{}
	}
	seen := make(map[string]struct{}, len(*videos))
	for i := range *videos {
		video := &(*videos)[i]
		if err := validateIdentifier("video id", video.VideoID, 128); err != nil {
			return err
		}
		if video.ChannelID != channelID {
			return fmt.Errorf("video channel does not match payload channel")
		}
		if err := validateOptionalText("video title", video.Title, 4096); err != nil {
			return err
		}
		if video.PublishedAt != nil && video.PublishedAt.IsZero() ||
			video.ScheduledFor != nil && video.ScheduledFor.IsZero() {
			return fmt.Errorf("video time must not be zero")
		}
		if err := normalizeOptionalTime(&video.PublishedAt); err != nil {
			return fmt.Errorf("video published at: %w", err)
		}
		if err := normalizeOptionalTime(&video.ScheduledFor); err != nil {
			return fmt.Errorf("video scheduled for: %w", err)
		}
		if _, ok := seen[video.VideoID]; ok {
			return fmt.Errorf("duplicate video id %q", video.VideoID)
		}
		seen[video.VideoID] = struct{}{}
	}
	sort.Slice(*videos, func(i, j int) bool { return (*videos)[i].VideoID < (*videos)[j].VideoID })
	return nil
}

func (p *LiveSnapshotV1) normalizeAndValidate(subject string) error {
	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return err
	}
	requestedChannels := make(map[string]struct{}, len(p.Coverage.RequestedChannelIDs))
	for _, channelID := range p.Coverage.RequestedChannelIDs {
		requestedChannels[channelID] = struct{}{}
	}
	requestedStatuses := make(map[string]struct{}, len(p.Coverage.Filters.Statuses))
	for _, status := range p.Coverage.Filters.Statuses {
		requestedStatuses[status] = struct{}{}
	}
	if len(p.Sessions) > 1000 {
		return fmt.Errorf("live session count exceeds 1000")
	}
	if p.Sessions == nil {
		p.Sessions = []LiveSessionV1{}
	}
	seen := make(map[string]struct{}, len(p.Sessions))
	for i := range p.Sessions {
		session := &p.Sessions[i]
		if err := validateIdentifier("live video id", session.VideoID, 128); err != nil {
			return err
		}
		if err := validateIdentifier("live channel id", session.ChannelID, 256); err != nil {
			return err
		}
		if _, ok := requestedChannels[session.ChannelID]; !ok {
			return fmt.Errorf("live session channel %q is outside coverage", session.ChannelID)
		}
		if session.Status != "UPCOMING" && session.Status != "LIVE" && session.Status != "ENDED" && session.Status != "CANCELLED" {
			return fmt.Errorf("unsupported live status %q", session.Status)
		}
		if _, ok := requestedStatuses[session.Status]; !ok {
			return fmt.Errorf("live session status %q is outside coverage", session.Status)
		}
		if err := normalizeOptionalTime(&session.ScheduledAt); err != nil {
			return fmt.Errorf("live scheduled at: %w", err)
		}
		if err := normalizeOptionalTime(&session.StartedAt); err != nil {
			return fmt.Errorf("live started at: %w", err)
		}
		if err := normalizeOptionalTime(&session.EndedAt); err != nil {
			return fmt.Errorf("live ended at: %w", err)
		}
		if _, ok := seen[session.VideoID]; ok {
			return fmt.Errorf("duplicate live video id %q", session.VideoID)
		}
		seen[session.VideoID] = struct{}{}
	}
	sort.Slice(p.Sessions, func(i, j int) bool { return p.Sessions[i].VideoID < p.Sessions[j].VideoID })
	return nil
}

func (p *ViewerSampleV1) normalizeAndValidate(subject string) error {
	if p.VideoID != subject || p.VideoID != p.Coverage.VideoID {
		return fmt.Errorf("viewer sample video does not match subject or coverage")
	}
	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return err
	}
	if p.SampleWindowStart.IsZero() || !p.SampleWindowStart.Equal(p.Coverage.SampleWindowStart) ||
		p.SampleWindowSeconds != p.Coverage.SampleWindowSeconds {
		return fmt.Errorf("viewer sample window does not match coverage")
	}
	if p.Availability != "AVAILABLE" && p.Availability != "HIDDEN" && p.Availability != "UNAVAILABLE" {
		return fmt.Errorf("unsupported viewer availability %q", p.Availability)
	}
	if p.Availability == "AVAILABLE" && (p.ViewerCount == nil || *p.ViewerCount < 0) {
		return fmt.Errorf("available viewer sample requires a non-negative count")
	}
	if p.Availability != "AVAILABLE" && p.ViewerCount != nil {
		return fmt.Errorf("hidden or unavailable viewer sample must not contain a count")
	}
	p.SampleWindowStart = p.SampleWindowStart.UTC()
	return nil
}

func (p *ChannelStatsV1) normalizeAndValidate(subject string) error {
	if p.ChannelID != subject {
		return fmt.Errorf("channel stats channel does not match subject")
	}
	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return err
	}
	for _, count := range []*int64{p.SubscriberCount, p.ViewCount, p.VideoCount} {
		if count != nil && *count < 0 {
			return fmt.Errorf("channel stats count must be non-negative")
		}
	}
	coveredFields := make(map[string]struct{}, len(p.Coverage.Fields))
	for _, field := range p.Coverage.Fields {
		coveredFields[field] = struct{}{}
	}
	for field, present := range map[string]bool{
		"subscriber_count": p.SubscriberCount != nil,
		"view_count":       p.ViewCount != nil,
		"video_count":      p.VideoCount != nil,
	} {
		if present {
			if _, ok := coveredFields[field]; !ok {
				return fmt.Errorf("channel stats field %q is outside coverage", field)
			}
		}
	}
	return nil
}

func (p *ChannelProfileV1) normalizeAndValidate(subject string) error {
	if p.ChannelID != subject {
		return fmt.Errorf("channel profile channel does not match subject")
	}
	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return err
	}
	for name, field := range map[string]FieldValue[string]{
		"handle": p.Handle, "description": p.Description, "country": p.Country, "joined date": p.JoinedDate,
	} {
		limit := 4096
		if name == "handle" || name == "country" || name == "joined date" {
			limit = 256
		}
		if !field.Present && field.Value != "" {
			return fmt.Errorf("absent profile field %s contains a value", name)
		}
		if len(field.Value) > limit {
			return fmt.Errorf("profile field %s exceeds %d bytes", name, limit)
		}
	}
	coveredFields := make(map[string]struct{}, len(p.Coverage.Fields))
	for _, field := range p.Coverage.Fields {
		coveredFields[field] = struct{}{}
	}
	for name, field := range map[string]FieldValue[string]{
		"handle": p.Handle, "description": p.Description, "country": p.Country, "joined_date": p.JoinedDate,
	} {
		if field.Present {
			if _, ok := coveredFields[name]; !ok {
				return fmt.Errorf("profile field %q is outside coverage", name)
			}
		}
	}
	return nil
}

func (p *ChannelPhotoV1) normalizeAndValidate(subject string) error {
	if p.ChannelID != subject {
		return fmt.Errorf("channel photo channel does not match subject")
	}
	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return err
	}
	coveredVariants := make(map[string]struct{}, len(p.Coverage.Variants))
	for _, variant := range p.Coverage.Variants {
		coveredVariants[variant] = struct{}{}
	}
	if len(p.Variants) > 20 {
		return fmt.Errorf("photo variant count exceeds 20")
	}
	if p.Variants == nil {
		p.Variants = []PhotoVariantV1{}
	}
	for i := range p.Variants {
		variant := &p.Variants[i]
		if variant.Kind != "avatar" && variant.Kind != "banner" {
			return fmt.Errorf("unsupported photo variant kind %q", variant.Kind)
		}
		if _, ok := coveredVariants[variant.Kind]; !ok {
			return fmt.Errorf("photo variant %q is outside coverage", variant.Kind)
		}
		if err := validateHTTPSURL("photo URL", variant.URL); err != nil {
			return err
		}
		if variant.Width < 0 || variant.Width > 20000 || variant.Height < 0 || variant.Height > 20000 {
			return fmt.Errorf("photo dimensions are outside the accepted range")
		}
		if err := validateOptionalText("stable media id", variant.StableMediaID, 512); err != nil {
			return err
		}
		if variant.ContentFingerprint != "" &&
			(len(variant.ContentFingerprint) != 64 || !lowercaseSHA256Pattern.MatchString(variant.ContentFingerprint)) {
			return fmt.Errorf("photo content fingerprint must be a lowercase sha256")
		}
	}
	sort.Slice(p.Variants, func(i, j int) bool {
		left, right := p.Variants[i], p.Variants[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.StableMediaID != right.StableMediaID {
			return left.StableMediaID < right.StableMediaID
		}
		if left.ContentFingerprint != right.ContentFingerprint {
			return left.ContentFingerprint < right.ContentFingerprint
		}
		if left.URL != right.URL {
			return left.URL < right.URL
		}
		if left.Width != right.Width {
			return left.Width < right.Width
		}
		return left.Height < right.Height
	})
	return nil
}

func (p *ScheduleSnapshotV1) normalizeAndValidate(subject string) error {
	if p.GroupKey != subject {
		return fmt.Errorf("schedule group does not match subject")
	}
	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return err
	}
	if len(p.Items) > 2000 {
		return fmt.Errorf("schedule item count exceeds 2000")
	}
	if p.Items == nil {
		p.Items = []ScheduleItemV1{}
	}
	seen := make(map[string]struct{}, len(p.Items))
	for i := range p.Items {
		item := &p.Items[i]
		if err := validateIdentifier("schedule external id", item.ExternalID, 256); err != nil {
			return err
		}
		if item.VideoID != "" {
			if err := validateIdentifier("schedule video id", item.VideoID, 128); err != nil {
				return err
			}
		}
		if item.ChannelID != "" {
			if err := validateIdentifier("schedule channel id", item.ChannelID, 256); err != nil {
				return err
			}
		}
		if len(strings.TrimSpace(item.Title)) == 0 || len(item.Title) > 4096 || item.ScheduledAt.IsZero() {
			return fmt.Errorf("schedule item title or scheduled time is invalid")
		}
		item.ScheduledAt = item.ScheduledAt.UTC()
		if p.Coverage.WindowStart != nil && item.ScheduledAt.Before(*p.Coverage.WindowStart) ||
			p.Coverage.WindowEnd != nil && item.ScheduledAt.After(*p.Coverage.WindowEnd) {
			return fmt.Errorf("schedule item time is outside coverage")
		}
		if err := normalizeOptionalTime(&item.EndedAt); err != nil {
			return fmt.Errorf("schedule ended at: %w", err)
		}
		if _, ok := seen[item.ExternalID]; ok {
			return fmt.Errorf("duplicate schedule external id %q", item.ExternalID)
		}
		seen[item.ExternalID] = struct{}{}
	}
	sort.Slice(p.Items, func(i, j int) bool { return p.Items[i].ExternalID < p.Items[j].ExternalID })
	return nil
}

func MarshalPayloadV1(value any) (json.RawMessage, error) {
	if err := validateCanonicalJSONStrings(value); err != nil {
		return nil, fmt.Errorf("marshal source observation payload: %w", err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal source observation payload: %w", err)
	}
	canonical, err := CanonicalizeJSON(raw)
	if err != nil {
		return nil, err
	}
	return canonical, nil
}
