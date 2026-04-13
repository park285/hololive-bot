package ops

import (
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func buildCommunityShortsAlarmSentHistoryDatasetReferenceRows(
	verdictRows []trackingrepo.ObservationPostComparisonVerdictRow,
) []CommunityShortsAlarmSentHistoryDatasetReferenceRow {
	if len(verdictRows) == 0 {
		return nil
	}

	rowsByKey := make(map[string]CommunityShortsAlarmSentHistoryDatasetReferenceRow, len(verdictRows))
	orderKeys := make([]string, 0, len(verdictRows))
	for i := range verdictRows {
		verdict := verdictRows[i]
		if verdict.Verdict == trackingrepo.ObservationPostComparisonVerdictUnexpectedSent {
			continue
		}
		channelID := strings.TrimSpace(verdict.ChannelID)
		if channelID == "" {
			continue
		}
		postIDs := communityShortsAlarmSentHistoryDatasetReferencePostIDs(verdict)
		for j := range postIDs {
			postID := strings.TrimSpace(postIDs[j])
			channelPostKey := buildCommunityShortsObservationChannelPostKey(channelID, postID)
			if channelPostKey == "" {
				continue
			}
			candidate := CommunityShortsAlarmSentHistoryDatasetReferenceRow{
				AlarmType:           verdict.AlarmType,
				ChannelID:           channelID,
				ChannelPostKey:      channelPostKey,
				PostID:              postID,
				ActualPublishedAt:   cloneCommunityShortsSendCountTime(verdict.ActualPublishedAt),
				DetectedAt:          cloneCommunityShortsSendCountTime(verdict.DetectedAt),
				VerificationVerdict: verdict.Verdict,
				VerificationReason:  verdict.Reason,
				SentCount:           verdict.SentCount,
				ReviewStatus:        verdict.ReviewStatus,
				RelatedSentPostIDs:  uniqueCommunityShortsAlarmSentHistoryDatasetStrings(verdict.RelatedSentPostIDs),
			}
			if existing, ok := rowsByKey[channelPostKey]; ok {
				rowsByKey[channelPostKey] = mergeCommunityShortsAlarmSentHistoryDatasetReferenceRow(existing, candidate)
				continue
			}
			rowsByKey[channelPostKey] = candidate
			orderKeys = append(orderKeys, channelPostKey)
		}
	}

	if len(orderKeys) == 0 {
		return nil
	}

	rows := make([]CommunityShortsAlarmSentHistoryDatasetReferenceRow, 0, len(orderKeys))
	for i := range orderKeys {
		rows = append(rows, rowsByKey[orderKeys[i]])
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left := communityShortsAlarmSentHistoryDatasetReferenceSortTime(rows[i])
		right := communityShortsAlarmSentHistoryDatasetReferenceSortTime(rows[j])
		if !left.Equal(right) {
			return left.Before(right)
		}
		if rows[i].ChannelID != rows[j].ChannelID {
			return rows[i].ChannelID < rows[j].ChannelID
		}
		if rows[i].PostID != rows[j].PostID {
			return rows[i].PostID < rows[j].PostID
		}
		return rows[i].AlarmType < rows[j].AlarmType
	})

	return rows
}

func communityShortsAlarmSentHistoryDatasetReferencePostIDs(
	verdict trackingrepo.ObservationPostComparisonVerdictRow,
) []string {
	if canonicalPostID := strings.TrimSpace(verdict.CanonicalPostID); canonicalPostID != "" {
		return []string{canonicalPostID}
	}
	return uniqueCommunityShortsAlarmSentHistoryDatasetStrings(verdict.RelatedBaselinePostIDs)
}

func mergeCommunityShortsAlarmSentHistoryDatasetReferenceRow(
	current CommunityShortsAlarmSentHistoryDatasetReferenceRow,
	next CommunityShortsAlarmSentHistoryDatasetReferenceRow,
) CommunityShortsAlarmSentHistoryDatasetReferenceRow {
	merged := current
	if merged.AlarmType == "" && next.AlarmType != "" {
		merged.AlarmType = next.AlarmType
	}
	if merged.ChannelID == "" && next.ChannelID != "" {
		merged.ChannelID = next.ChannelID
	}
	if merged.ChannelPostKey == "" && next.ChannelPostKey != "" {
		merged.ChannelPostKey = next.ChannelPostKey
	}
	if merged.PostID == "" && next.PostID != "" {
		merged.PostID = next.PostID
	}
	merged.ActualPublishedAt = communityShortsAlarmSentHistoryDatasetEarlierTime(merged.ActualPublishedAt, next.ActualPublishedAt)
	merged.DetectedAt = communityShortsAlarmSentHistoryDatasetEarlierTime(merged.DetectedAt, next.DetectedAt)
	if communityShortsAlarmSentHistoryDatasetReferenceVerdictPriority(next.VerificationVerdict) > communityShortsAlarmSentHistoryDatasetReferenceVerdictPriority(merged.VerificationVerdict) {
		merged.VerificationVerdict = next.VerificationVerdict
		merged.VerificationReason = next.VerificationReason
	}
	if next.SentCount > merged.SentCount {
		merged.SentCount = next.SentCount
	}
	if next.ReviewStatus != "" {
		merged.ReviewStatus = next.ReviewStatus
	}
	merged.RelatedSentPostIDs = mergeUniqueCommunityShortsAlarmSentHistoryDatasetStrings(merged.RelatedSentPostIDs, next.RelatedSentPostIDs)
	return merged
}

func communityShortsAlarmSentHistoryDatasetReferenceVerdictPriority(
	verdict trackingrepo.ObservationPostComparisonVerdict,
) int {
	switch verdict {
	case trackingrepo.ObservationPostComparisonVerdictMatched:
		return 40
	case trackingrepo.ObservationPostComparisonVerdictDuplicateSent:
		return 30
	case trackingrepo.ObservationPostComparisonVerdictUnsent:
		return 20
	case trackingrepo.ObservationPostComparisonVerdictIdentifierMismatchCandidate:
		return 10
	default:
		return 0
	}
}

func communityShortsAlarmSentHistoryDatasetReferenceSortTime(
	row CommunityShortsAlarmSentHistoryDatasetReferenceRow,
) time.Time {
	for _, candidate := range []*time.Time{row.ActualPublishedAt, row.DetectedAt} {
		if candidate != nil {
			return candidate.UTC()
		}
	}
	return time.Time{}
}

func communityShortsAlarmSentHistoryDatasetEarlierTime(left *time.Time, right *time.Time) *time.Time {
	if left == nil {
		return cloneCommunityShortsSendCountTime(right)
	}
	if right == nil {
		return cloneCommunityShortsSendCountTime(left)
	}
	if right.Before(left.UTC()) {
		return cloneCommunityShortsSendCountTime(right)
	}
	return cloneCommunityShortsSendCountTime(left)
}

func uniqueCommunityShortsAlarmSentHistoryDatasetStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for i := range values {
		value := strings.TrimSpace(values[i])
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	if len(unique) == 0 {
		return nil
	}
	return unique
}

func mergeUniqueCommunityShortsAlarmSentHistoryDatasetStrings(left []string, right []string) []string {
	merged := make([]string, 0, len(left)+len(right))
	merged = append(merged, left...)
	merged = append(merged, right...)
	return uniqueCommunityShortsAlarmSentHistoryDatasetStrings(merged)
}

func buildCommunityShortsObservationChannelPostKey(channelID string, postID string) string {
	trimmedChannelID := strings.TrimSpace(channelID)
	trimmedPostID := strings.TrimSpace(postID)
	if trimmedChannelID == "" || trimmedPostID == "" {
		return ""
	}
	return strings.Join([]string{trimmedChannelID, trimmedPostID}, "|")
}

func buildCommunityShortsAlarmSentHistoryDatasetVerificationRows(
	verdictRows []trackingrepo.ObservationPostComparisonVerdictRow,
) []CommunityShortsAlarmSentHistoryDatasetVerificationRow {
	if len(verdictRows) == 0 {
		return nil
	}

	rows := make([]CommunityShortsAlarmSentHistoryDatasetVerificationRow, 0, len(verdictRows))
	for i := range verdictRows {
		verdict := verdictRows[i]
		postID := strings.TrimSpace(verdict.CanonicalPostID)
		rows = append(rows, CommunityShortsAlarmSentHistoryDatasetVerificationRow{
			Verdict:                verdict.Verdict,
			Reason:                 verdict.Reason,
			AlarmType:              verdict.AlarmType,
			ChannelID:              strings.TrimSpace(verdict.ChannelID),
			PostID:                 postID,
			PostKey:                buildCommunityShortsObservationPostKey(verdict.AlarmType, verdict.ChannelID, postID),
			ContentID:              strings.TrimSpace(verdict.ContentID),
			ActualPublishedAt:      cloneCommunityShortsSendCountTime(verdict.ActualPublishedAt),
			DetectedAt:             cloneCommunityShortsSendCountTime(verdict.DetectedAt),
			AlarmSentAt:            cloneCommunityShortsSendCountTime(verdict.AlarmSentAt),
			MatchPublishedAt:       cloneCommunityShortsSendCountTime(verdict.MatchPublishedAt),
			MatchTitleHint:         strings.TrimSpace(verdict.MatchTitleHint),
			MatchBasis:             cloneCommunityShortsAlarmSentHistoryDatasetStrings(verdict.MatchBasis),
			ReviewStatus:           verdict.ReviewStatus,
			BaselineCount:          verdict.BaselineCount,
			SentCount:              verdict.SentCount,
			RelatedBaselinePostIDs: cloneCommunityShortsAlarmSentHistoryDatasetStrings(verdict.RelatedBaselinePostIDs),
			RelatedSentPostIDs:     cloneCommunityShortsAlarmSentHistoryDatasetStrings(verdict.RelatedSentPostIDs),
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		left := communityShortsAlarmSentHistoryDatasetVerificationSortTime(rows[i])
		right := communityShortsAlarmSentHistoryDatasetVerificationSortTime(rows[j])
		if !left.Equal(right) {
			return left.Before(right)
		}
		if rows[i].AlarmType != rows[j].AlarmType {
			return rows[i].AlarmType < rows[j].AlarmType
		}
		if rows[i].ChannelID != rows[j].ChannelID {
			return rows[i].ChannelID < rows[j].ChannelID
		}
		if rows[i].PostKey != rows[j].PostKey {
			return rows[i].PostKey < rows[j].PostKey
		}
		if rows[i].PostID != rows[j].PostID {
			return rows[i].PostID < rows[j].PostID
		}
		if rows[i].ContentID != rows[j].ContentID {
			return rows[i].ContentID < rows[j].ContentID
		}
		return rows[i].Verdict < rows[j].Verdict
	})

	return rows
}

func communityShortsAlarmSentHistoryDatasetVerificationSortTime(
	row CommunityShortsAlarmSentHistoryDatasetVerificationRow,
) time.Time {
	for _, candidate := range []*time.Time{row.ActualPublishedAt, row.MatchPublishedAt, row.DetectedAt, row.AlarmSentAt} {
		if candidate != nil {
			return candidate.UTC()
		}
	}
	return time.Time{}
}

func cloneCommunityShortsAlarmSentHistoryDatasetStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]string, 0, len(values))
	for i := range values {
		value := strings.TrimSpace(values[i])
		if value == "" {
			continue
		}
		cloned = append(cloned, value)
	}
	if len(cloned) == 0 {
		return nil
	}
	return cloned
}

func buildCommunityShortsObservationPostKey(alarmType domain.AlarmType, channelID string, postID string) string {
	trimmedChannelID := strings.TrimSpace(channelID)
	trimmedPostID := strings.TrimSpace(postID)
	if !alarmType.IsValid() || trimmedChannelID == "" || trimmedPostID == "" {
		return ""
	}
	return strings.Join([]string{string(alarmType), trimmedChannelID, trimmedPostID}, "|")
}
