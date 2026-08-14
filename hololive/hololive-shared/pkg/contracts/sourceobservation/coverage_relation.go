package sourceobservation

import "time"

func AbsenceCapabilityFor(kind ObservationKind) AbsenceCapability {
	switch kind {
	case KindVideoList, KindShortsList, KindLiveSnapshot:
		return AbsenceScoped
	default:
		return AbsencePositiveOnly
	}
}

func RelateChannelList(candidate, evidence ChannelListCoverageV1) CoverageRelation {
	if candidate.ChannelID != evidence.ChannelID {
		return CoverageDisjoint
	}
	if candidate.Filters.IncludeUpcoming != evidence.Filters.IncludeUpcoming {
		return CoverageDisjoint
	}
	candidateRange := filterTimeRange(candidate.Filters)
	evidenceRange := filterTimeRange(evidence.Filters)
	if !timeRangesOverlap(candidateRange, evidenceRange) {
		return CoverageDisjoint
	}
	if timeRangesEqual(candidateRange, evidenceRange) {
		return CoverageEqual
	}
	if timeRangeContains(evidenceRange, candidateRange) && evidence.Exhausted {
		return CoverageCovers
	}
	if timeRangeContains(candidateRange, evidenceRange) && candidate.Exhausted {
		return CoverageCoveredBy
	}
	return CoverageDisjoint
}

func RelateShortsList(candidate, evidence ShortsListCoverageV1) CoverageRelation {
	if candidate.ChannelID != evidence.ChannelID {
		return CoverageDisjoint
	}
	if candidate.Exhausted && evidence.Exhausted {
		return CoverageEqual
	}
	if evidence.Exhausted {
		return CoverageCovers
	}
	if candidate.Exhausted {
		return CoverageCoveredBy
	}
	if candidate.CursorStart == evidence.CursorStart && candidate.CursorEnd == evidence.CursorEnd {
		return CoverageEqual
	}
	return CoverageDisjoint
}

func ChannelListCoversVideo(coverage ChannelListCoverageV1, video VideoListItemV1) bool {
	if coverage.ChannelID != video.ChannelID {
		return false
	}
	if upcomingOnly(video) && !coverage.Filters.IncludeUpcoming {
		return false
	}
	return publishedAtInFilters(coverage.Filters, video.PublishedAt)
}

func publishedAtInFilters(filters VideoListFiltersV1, publishedAt *time.Time) bool {
	if publishedAt == nil {
		return filters.PublishedAfter == nil && filters.PublishedBefore == nil
	}
	if filters.PublishedAfter != nil && publishedAt.Before(*filters.PublishedAfter) {
		return false
	}
	if filters.PublishedBefore != nil && publishedAt.After(*filters.PublishedBefore) {
		return false
	}
	return true
}

func ShortsListCoversVideo(coverage ShortsListCoverageV1, video VideoListItemV1) bool {
	return coverage.ChannelID == video.ChannelID
}

func CoverageAllowsAbsence(relation CoverageRelation) bool {
	return relation == CoverageEqual || relation == CoverageCovers
}

func LiveCoverageCoversChannel(coverage GlobalChannelCoverageV1, channelID string) bool {
	if channelID == "" {
		return false
	}
	for _, requested := range coverage.RequestedChannelIDs {
		if requested == channelID {
			return true
		}
	}
	return false
}

func LiveCoverageCoversSession(coverage GlobalChannelCoverageV1, channelID, status string) bool {
	if !LiveCoverageCoversChannel(coverage, channelID) {
		return false
	}
	if len(coverage.Filters.Statuses) == 0 {
		return true
	}
	for _, requested := range coverage.Filters.Statuses {
		if requested == status {
			return true
		}
	}
	return false
}

func upcomingOnly(video VideoListItemV1) bool {
	return video.PublishedAt == nil && video.ScheduledFor != nil
}
