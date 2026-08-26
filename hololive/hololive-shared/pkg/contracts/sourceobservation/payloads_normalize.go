package sourceobservation

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"time"
)

func (p *VideoListV1) normalizeAndValidate(subject string) error {
	if p.ChannelID != subject {
		return errors.New("video list channel does not match subject")
	}

	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return fmt.Errorf("normalize and validate: %w", err)
	}

	if err := normalizeVideos(p.ChannelID, &p.Videos); err != nil {
		return fmt.Errorf("normalize videos: %w", err)
	}

	if err := validateVideosInsideCoverage(p); err != nil {
		return fmt.Errorf("validate videos inside coverage: %w", err)
	}

	return nil
}

func validateVideosInsideCoverage(p *VideoListV1) error {
	for i := range p.Videos {
		publishedAt := p.Videos[i].PublishedAt
		if publishedAt == nil {
			continue
		}

		if p.Coverage.Filters.PublishedAfter != nil && publishedAt.Before(*p.Coverage.Filters.PublishedAfter) ||
			p.Coverage.Filters.PublishedBefore != nil && publishedAt.After(*p.Coverage.Filters.PublishedBefore) {
			return errors.New("video published time is outside coverage")
		}
	}

	return nil
}

func (p *ShortsListV1) normalizeAndValidate(subject string) error {
	if p.ChannelID != subject {
		return errors.New("shorts list channel does not match subject")
	}

	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return fmt.Errorf("normalize and validate: %w", err)
	}

	if err := normalizeVideos(p.ChannelID, &p.Videos); err != nil {
		return fmt.Errorf("normalize videos: %w", err)
	}

	return nil
}

func normalizeVideos(channelID string, videos *[]VideoListItemV1) error {
	if len(*videos) > 1000 {
		return errors.New("video count exceeds 1000")
	}

	if *videos == nil {
		*videos = []VideoListItemV1{}
	}

	seen := make(map[string]struct{}, len(*videos))
	for i := range *videos {
		if err := normalizeVideoItem(channelID, &(*videos)[i], seen); err != nil {
			return fmt.Errorf("normalize video item: %w", err)
		}
	}

	slices.SortFunc(*videos, func(left, right VideoListItemV1) int {
		return cmp.Compare(left.VideoID, right.VideoID)
	})

	return nil
}

func normalizeVideoItem(channelID string, video *VideoListItemV1, seen map[string]struct{}) error {
	if err := validateVideoIdentity(channelID, video); err != nil {
		return fmt.Errorf("validate video identity: %w", err)
	}

	if err := validateVideoTimes(video); err != nil {
		return fmt.Errorf("validate video times: %w", err)
	}

	if _, ok := seen[video.VideoID]; ok {
		return fmt.Errorf("duplicate video id %q", video.VideoID)
	}

	seen[video.VideoID] = struct{}{}

	return nil
}

func validateVideoIdentity(channelID string, video *VideoListItemV1) error {
	if err := validateIdentifier("video id", video.VideoID, 128); err != nil {
		return fmt.Errorf("validate identifier: %w", err)
	}

	if video.ChannelID != channelID {
		return errors.New("video channel does not match payload channel")
	}

	if err := validateOptionalText("video title", video.Title, 4096); err != nil {
		return fmt.Errorf("validate optional text: %w", err)
	}

	return nil
}

func validateVideoTimes(video *VideoListItemV1) error {
	if optionalTimeIsZero(video.PublishedAt) || optionalTimeIsZero(video.ScheduledFor) {
		return errors.New("video time must not be zero")
	}

	if err := normalizeOptionalTime(&video.PublishedAt); err != nil {
		return fmt.Errorf("video published at: %w", err)
	}

	if err := normalizeOptionalTime(&video.ScheduledFor); err != nil {
		return fmt.Errorf("video scheduled for: %w", err)
	}

	return nil
}

func optionalTimeIsZero(value *time.Time) bool {
	return value != nil && value.IsZero()
}

func (p *LiveSnapshotV1) normalizeAndValidate(subject string) error {
	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return fmt.Errorf("normalize and validate: %w", err)
	}

	if err := prepareLiveSessions(p); err != nil {
		return fmt.Errorf("prepare live sessions: %w", err)
	}

	requestedChannels := stringSet(p.Coverage.RequestedChannelIDs)
	requestedStatuses := stringSet(p.Coverage.Filters.Statuses)
	seen := make(map[string]struct{}, len(p.Sessions))

	for i := range p.Sessions {
		if err := normalizeLiveSession(&p.Sessions[i], requestedChannels, requestedStatuses, seen); err != nil {
			return fmt.Errorf("normalize live session: %w", err)
		}
	}

	slices.SortFunc(p.Sessions, func(left, right LiveSessionV1) int {
		return cmp.Compare(left.VideoID, right.VideoID)
	})

	return nil
}

func prepareLiveSessions(p *LiveSnapshotV1) error {
	if len(p.Sessions) > 1000 {
		return errors.New("live session count exceeds 1000")
	}

	if p.Sessions == nil {
		p.Sessions = []LiveSessionV1{}
	}

	return nil
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}

	return result
}

func normalizeLiveSession(
	session *LiveSessionV1,
	requestedChannels, requestedStatuses, seen map[string]struct{},
) error {
	if err := validateLiveSessionIdentity(session, requestedChannels, requestedStatuses); err != nil {
		return fmt.Errorf("validate live session identity: %w", err)
	}

	if err := normalizeLiveSessionTimes(session); err != nil {
		return fmt.Errorf("normalize live session times: %w", err)
	}

	if _, ok := seen[session.VideoID]; ok {
		return fmt.Errorf("duplicate live video id %q", session.VideoID)
	}

	seen[session.VideoID] = struct{}{}

	return nil
}

func validateLiveSessionIdentity(
	session *LiveSessionV1,
	requestedChannels, requestedStatuses map[string]struct{},
) error {
	if err := validateIdentifier("live video id", session.VideoID, 128); err != nil {
		return fmt.Errorf("validate identifier: %w", err)
	}

	if err := validateIdentifier("live channel id", session.ChannelID, 256); err != nil {
		return fmt.Errorf("validate identifier: %w", err)
	}

	if _, ok := requestedChannels[session.ChannelID]; !ok {
		return fmt.Errorf("live session channel %q is outside coverage", session.ChannelID)
	}

	if !validLiveStatus(session.Status) {
		return fmt.Errorf("unsupported live status %q", session.Status)
	}

	if _, ok := requestedStatuses[session.Status]; !ok {
		return fmt.Errorf("live session status %q is outside coverage", session.Status)
	}

	return nil
}

func validLiveStatus(status string) bool {
	return status == "UPCOMING" || status == "LIVE" || status == "ENDED" || status == "CANCELLED" //nolint:misspell // YouTube 방송 상태 계약값이 영국식 CANCELLED라, canceled로 바꾸면 상태 판정이 어긋난다.
}

func normalizeLiveSessionTimes(session *LiveSessionV1) error {
	if err := normalizeOptionalTime(&session.ScheduledAt); err != nil {
		return fmt.Errorf("live scheduled at: %w", err)
	}

	if err := normalizeOptionalTime(&session.StartedAt); err != nil {
		return fmt.Errorf("live started at: %w", err)
	}

	if err := normalizeOptionalTime(&session.EndedAt); err != nil {
		return fmt.Errorf("live ended at: %w", err)
	}

	return nil
}

func (p *ViewerSampleV1) normalizeAndValidate(subject string) error {
	if p.VideoID != subject || p.VideoID != p.Coverage.VideoID {
		return errors.New("viewer sample video does not match subject or coverage")
	}

	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return fmt.Errorf("normalize and validate: %w", err)
	}

	if err := validateViewerSampleWindow(p); err != nil {
		return fmt.Errorf("validate viewer sample window: %w", err)
	}

	if err := validateViewerAvailability(p); err != nil {
		return fmt.Errorf("validate viewer availability: %w", err)
	}

	p.SampleWindowStart = p.SampleWindowStart.UTC()

	return nil
}

func validateViewerSampleWindow(p *ViewerSampleV1) error {
	if p.SampleWindowStart.IsZero() || !p.SampleWindowStart.Equal(p.Coverage.SampleWindowStart) ||
		p.SampleWindowSeconds != p.Coverage.SampleWindowSeconds {
		return errors.New("viewer sample window does not match coverage")
	}

	return nil
}

func validateViewerAvailability(p *ViewerSampleV1) error {
	if p.Availability != "AVAILABLE" && p.Availability != "HIDDEN" && p.Availability != "UNAVAILABLE" {
		return fmt.Errorf("unsupported viewer availability %q", p.Availability)
	}

	if p.Availability == "AVAILABLE" && (p.ViewerCount == nil || *p.ViewerCount < 0) {
		return errors.New("available viewer sample requires a non-negative count")
	}

	if p.Availability != "AVAILABLE" && p.ViewerCount != nil {
		return errors.New("hidden or unavailable viewer sample must not contain a count")
	}

	return nil
}

func (p *ChannelStatsV1) normalizeAndValidate(subject string) error {
	if p.ChannelID != subject {
		return errors.New("channel stats channel does not match subject")
	}

	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return fmt.Errorf("normalize and validate: %w", err)
	}

	if err := validateNonNegativeCounts(p.SubscriberCount, p.ViewCount, p.VideoCount); err != nil {
		return fmt.Errorf("validate non negative counts: %w", err)
	}

	if err := validateCoveredStatsFields(p); err != nil {
		return fmt.Errorf("validate covered stats fields: %w", err)
	}

	return nil
}

func validateNonNegativeCounts(counts ...*int64) error {
	for _, count := range counts {
		if count != nil && *count < 0 {
			return errors.New("channel stats count must be non-negative")
		}
	}

	return nil
}

func validateCoveredStatsFields(p *ChannelStatsV1) error {
	coveredFields := stringSet(p.Coverage.Fields)
	for field, present := range map[string]bool{
		"subscriber_count": p.SubscriberCount != nil,
		"view_count":       p.ViewCount != nil,
		"video_count":      p.VideoCount != nil,
	} {
		if err := requireCoveredField(coveredFields, field, present); err != nil {
			return fmt.Errorf("channel stats field %q is outside coverage", field)
		}
	}

	return nil
}

func requireCoveredField(covered map[string]struct{}, field string, present bool) error {
	if !present {
		return nil
	}

	if _, ok := covered[field]; ok {
		return nil
	}

	return fmt.Errorf("field %q is outside coverage", field)
}
