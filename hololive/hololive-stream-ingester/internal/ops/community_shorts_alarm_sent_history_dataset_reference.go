package ops

import (
	"cmp"
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
		candidates := buildCommunityShortsAlarmSentHistoryDatasetReferenceCandidateRows(verdictRows[i])
		for j := range candidates {
			orderKeys = addCommunityShortsAlarmSentHistoryDatasetReferenceRow(rowsByKey, orderKeys, candidates[j])
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
		return compareCommunityShortsAlarmSentHistoryDatasetReferenceRows(rows[i], rows[j]) < 0
	})

	return rows
}

func buildCommunityShortsAlarmSentHistoryDatasetReferenceCandidateRows(
	verdict trackingrepo.ObservationPostComparisonVerdictRow,
) []CommunityShortsAlarmSentHistoryDatasetReferenceRow {
	if verdict.Verdict == trackingrepo.ObservationPostComparisonVerdictUnexpectedSent {
		return nil
	}

	channelID := strings.TrimSpace(verdict.ChannelID)
	if channelID == "" {
		return nil
	}

	postIDs := communityShortsAlarmSentHistoryDatasetReferencePostIDs(verdict)
	rows := make([]CommunityShortsAlarmSentHistoryDatasetReferenceRow, 0, len(postIDs))
	for i := range postIDs {
		if row, ok := buildCommunityShortsAlarmSentHistoryDatasetReferenceCandidateRow(verdict, channelID, postIDs[i]); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func buildCommunityShortsAlarmSentHistoryDatasetReferenceCandidateRow(
	verdict trackingrepo.ObservationPostComparisonVerdictRow,
	channelID string,
	postID string,
) (CommunityShortsAlarmSentHistoryDatasetReferenceRow, bool) {
	postID = strings.TrimSpace(postID)
	channelPostKey := buildCommunityShortsObservationChannelPostKey(channelID, postID)
	if channelPostKey == "" {
		return CommunityShortsAlarmSentHistoryDatasetReferenceRow{}, false
	}
	return CommunityShortsAlarmSentHistoryDatasetReferenceRow{
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
	}, true
}

func addCommunityShortsAlarmSentHistoryDatasetReferenceRow(
	rowsByKey map[string]CommunityShortsAlarmSentHistoryDatasetReferenceRow,
	orderKeys []string,
	candidate CommunityShortsAlarmSentHistoryDatasetReferenceRow,
) []string {
	if existing, ok := rowsByKey[candidate.ChannelPostKey]; ok {
		rowsByKey[candidate.ChannelPostKey] = mergeCommunityShortsAlarmSentHistoryDatasetReferenceRow(existing, candidate)
		return orderKeys
	}
	rowsByKey[candidate.ChannelPostKey] = candidate
	return append(orderKeys, candidate.ChannelPostKey)
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
	merged.AlarmType = firstNonEmptyCommunityShortsAlarmSentHistoryDatasetValue(merged.AlarmType, next.AlarmType)
	merged.ChannelID = firstNonEmptyCommunityShortsAlarmSentHistoryDatasetValue(merged.ChannelID, next.ChannelID)
	merged.ChannelPostKey = firstNonEmptyCommunityShortsAlarmSentHistoryDatasetValue(merged.ChannelPostKey, next.ChannelPostKey)
	merged.PostID = firstNonEmptyCommunityShortsAlarmSentHistoryDatasetValue(merged.PostID, next.PostID)
	merged.ActualPublishedAt = communityShortsAlarmSentHistoryDatasetEarlierTime(merged.ActualPublishedAt, next.ActualPublishedAt)
	merged.DetectedAt = communityShortsAlarmSentHistoryDatasetEarlierTime(merged.DetectedAt, next.DetectedAt)
	if hasHigherCommunityShortsAlarmSentHistoryDatasetReferenceVerdictPriority(next.VerificationVerdict, merged.VerificationVerdict) {
		merged.VerificationVerdict = next.VerificationVerdict
		merged.VerificationReason = next.VerificationReason
	}
	if next.SentCount > merged.SentCount {
		merged.SentCount = next.SentCount
	}
	merged.ReviewStatus = lastNonEmptyCommunityShortsAlarmSentHistoryDatasetValue(merged.ReviewStatus, next.ReviewStatus)
	merged.RelatedSentPostIDs = mergeUniqueCommunityShortsAlarmSentHistoryDatasetStrings(merged.RelatedSentPostIDs, next.RelatedSentPostIDs)
	return merged
}

func firstNonEmptyCommunityShortsAlarmSentHistoryDatasetValue[T ~string](current T, next T) T {
	if current == "" {
		return next
	}
	return current
}

func lastNonEmptyCommunityShortsAlarmSentHistoryDatasetValue[T ~string](current T, next T) T {
	if next != "" {
		return next
	}
	return current
}

func hasHigherCommunityShortsAlarmSentHistoryDatasetReferenceVerdictPriority(
	next trackingrepo.ObservationPostComparisonVerdict,
	current trackingrepo.ObservationPostComparisonVerdict,
) bool {
	return communityShortsAlarmSentHistoryDatasetReferenceVerdictPriority(next) >
		communityShortsAlarmSentHistoryDatasetReferenceVerdictPriority(current)
}

var communityShortsAlarmSentHistoryDatasetReferenceVerdictPriorities = map[trackingrepo.ObservationPostComparisonVerdict]int{
	trackingrepo.ObservationPostComparisonVerdictMatched:                     40,
	trackingrepo.ObservationPostComparisonVerdictDuplicateSent:               30,
	trackingrepo.ObservationPostComparisonVerdictUnsent:                      20,
	trackingrepo.ObservationPostComparisonVerdictIdentifierMismatchCandidate: 10,
}

func communityShortsAlarmSentHistoryDatasetReferenceVerdictPriority(
	verdict trackingrepo.ObservationPostComparisonVerdict,
) int {
	return communityShortsAlarmSentHistoryDatasetReferenceVerdictPriorities[verdict]
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

func compareCommunityShortsAlarmSentHistoryDatasetReferenceRows(
	left CommunityShortsAlarmSentHistoryDatasetReferenceRow,
	right CommunityShortsAlarmSentHistoryDatasetReferenceRow,
) int {
	leftTime := communityShortsAlarmSentHistoryDatasetReferenceSortTime(left)
	rightTime := communityShortsAlarmSentHistoryDatasetReferenceSortTime(right)
	return cmp.Or(
		compareCommunityShortsAlarmSentHistoryDatasetTimes(leftTime, rightTime),
		cmp.Compare(left.ChannelID, right.ChannelID),
		cmp.Compare(left.PostID, right.PostID),
		cmp.Compare(left.AlarmType, right.AlarmType),
	)
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
	return trimmedChannelID + "|" + trimmedPostID
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
		return compareCommunityShortsAlarmSentHistoryDatasetVerificationRows(rows[i], rows[j]) < 0
	})

	return rows
}

func compareCommunityShortsAlarmSentHistoryDatasetVerificationRows(
	left CommunityShortsAlarmSentHistoryDatasetVerificationRow,
	right CommunityShortsAlarmSentHistoryDatasetVerificationRow,
) int {
	leftTime := communityShortsAlarmSentHistoryDatasetVerificationSortTime(left)
	rightTime := communityShortsAlarmSentHistoryDatasetVerificationSortTime(right)
	return cmp.Or(
		compareCommunityShortsAlarmSentHistoryDatasetTimes(leftTime, rightTime),
		cmp.Compare(left.AlarmType, right.AlarmType),
		cmp.Compare(left.ChannelID, right.ChannelID),
		cmp.Compare(left.PostKey, right.PostKey),
		cmp.Compare(left.PostID, right.PostID),
		cmp.Compare(left.ContentID, right.ContentID),
		cmp.Compare(left.Verdict, right.Verdict),
	)
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

func compareCommunityShortsAlarmSentHistoryDatasetTimes(left time.Time, right time.Time) int {
	if left.Equal(right) {
		return 0
	}
	if left.Before(right) {
		return -1
	}
	return 1
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
