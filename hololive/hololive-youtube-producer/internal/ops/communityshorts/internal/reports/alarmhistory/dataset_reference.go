package alarmhistory

import (
	"cmp"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"

	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

func buildDatasetReferenceRows(
	verdictRows []trackingrepo.ObservationPostComparisonVerdictRow,
) []DatasetReferenceRow {
	if len(verdictRows) == 0 {
		return nil
	}

	rowsByKey := make(map[string]DatasetReferenceRow, len(verdictRows))
	orderKeys := make([]string, 0, len(verdictRows))
	for i := range verdictRows {
		candidates := buildReferenceCandidateRows(&verdictRows[i])
		for j := range candidates {
			orderKeys = addReferenceRow(rowsByKey, orderKeys, &candidates[j])
		}
	}

	if len(orderKeys) == 0 {
		return nil
	}

	rows := make([]DatasetReferenceRow, 0, len(orderKeys))
	for i := range orderKeys {
		rows = append(rows, rowsByKey[orderKeys[i]])
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return compareReferenceRows(&rows[i], &rows[j]) < 0
	})

	return rows
}

func buildReferenceCandidateRows(
	verdict *trackingrepo.ObservationPostComparisonVerdictRow,
) []DatasetReferenceRow {
	if verdict == nil {
		return nil
	}
	if verdict.Verdict == trackingrepo.ObservationPostComparisonVerdictUnexpectedSent {
		return nil
	}

	channelID := strings.TrimSpace(verdict.ChannelID)
	if channelID == "" {
		return nil
	}

	postIDs := referencePostIDs(verdict)
	rows := make([]DatasetReferenceRow, 0, len(postIDs))
	for i := range postIDs {
		if row, ok := buildReferenceCandidateRow(verdict, channelID, postIDs[i]); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func buildReferenceCandidateRow(
	verdict *trackingrepo.ObservationPostComparisonVerdictRow,
	channelID string,
	postID string,
) (DatasetReferenceRow, bool) {
	if verdict == nil {
		return DatasetReferenceRow{}, false
	}
	postID = strings.TrimSpace(postID)
	channelPostKey := buildChannelPostKey(channelID, postID)
	if channelPostKey == "" {
		return DatasetReferenceRow{}, false
	}
	return DatasetReferenceRow{
		AlarmType:           verdict.AlarmType,
		ChannelID:           channelID,
		ChannelPostKey:      channelPostKey,
		PostID:              postID,
		ActualPublishedAt:   shared.CloneSendCountTime(verdict.ActualPublishedAt),
		DetectedAt:          shared.CloneSendCountTime(verdict.DetectedAt),
		VerificationVerdict: verdict.Verdict,
		VerificationReason:  verdict.Reason,
		SentCount:           verdict.SentCount,
		ReviewStatus:        verdict.ReviewStatus,
		RelatedSentPostIDs:  uniqueStrings(verdict.RelatedSentPostIDs),
	}, true
}

func addReferenceRow(
	rowsByKey map[string]DatasetReferenceRow,
	orderKeys []string,
	candidate *DatasetReferenceRow,
) []string {
	if candidate == nil {
		return orderKeys
	}
	if existing, ok := rowsByKey[candidate.ChannelPostKey]; ok {
		rowsByKey[candidate.ChannelPostKey] = mergeReferenceRow(&existing, candidate)
		return orderKeys
	}
	rowsByKey[candidate.ChannelPostKey] = *candidate
	return append(orderKeys, candidate.ChannelPostKey)
}

func referencePostIDs(
	verdict *trackingrepo.ObservationPostComparisonVerdictRow,
) []string {
	if verdict == nil {
		return nil
	}
	if canonicalPostID := strings.TrimSpace(verdict.CanonicalPostID); canonicalPostID != "" {
		return []string{canonicalPostID}
	}
	return uniqueStrings(verdict.RelatedBaselinePostIDs)
}

func mergeReferenceRow(
	current *DatasetReferenceRow,
	next *DatasetReferenceRow,
) DatasetReferenceRow {
	if current == nil {
		if next == nil {
			return DatasetReferenceRow{}
		}
		return *next
	}
	if next == nil {
		return *current
	}
	merged := *current
	merged.AlarmType = firstNonEmpty(merged.AlarmType, next.AlarmType)
	merged.ChannelID = firstNonEmptyString(merged.ChannelID, next.ChannelID)
	merged.ChannelPostKey = firstNonEmptyString(merged.ChannelPostKey, next.ChannelPostKey)
	merged.PostID = firstNonEmptyString(merged.PostID, next.PostID)
	merged.ActualPublishedAt = earlierTime(merged.ActualPublishedAt, next.ActualPublishedAt)
	merged.DetectedAt = earlierTime(merged.DetectedAt, next.DetectedAt)
	if hasHigherVerdictPriority(next.VerificationVerdict, merged.VerificationVerdict) {
		merged.VerificationVerdict = next.VerificationVerdict
		merged.VerificationReason = next.VerificationReason
	}
	if next.SentCount > merged.SentCount {
		merged.SentCount = next.SentCount
	}
	merged.ReviewStatus = lastNonEmpty(merged.ReviewStatus, next.ReviewStatus)
	merged.RelatedSentPostIDs = mergeUniqueStrings(merged.RelatedSentPostIDs, next.RelatedSentPostIDs)
	return merged
}

func firstNonEmpty[T ~string](current, next T) T {
	if current == "" {
		return next
	}
	return current
}

func firstNonEmptyString(current, next string) string {
	if current == "" {
		return next
	}
	return current
}

func lastNonEmpty[T ~string](current, next T) T {
	if next != "" {
		return next
	}
	return current
}

func hasHigherVerdictPriority(
	next trackingrepo.ObservationPostComparisonVerdict,
	current trackingrepo.ObservationPostComparisonVerdict,
) bool {
	return referenceVerdictPriority(next) > referenceVerdictPriority(current)
}

var referenceVerdictPriorities = map[trackingrepo.ObservationPostComparisonVerdict]int{
	trackingrepo.ObservationPostComparisonVerdictMatched:                     40,
	trackingrepo.ObservationPostComparisonVerdictDuplicateSent:               30,
	trackingrepo.ObservationPostComparisonVerdictUnsent:                      20,
	trackingrepo.ObservationPostComparisonVerdictIdentifierMismatchCandidate: 10,
}

func referenceVerdictPriority(
	verdict trackingrepo.ObservationPostComparisonVerdict,
) int {
	return referenceVerdictPriorities[verdict]
}

func referenceSortTime(row *DatasetReferenceRow) time.Time {
	if row == nil {
		return time.Time{}
	}
	for _, candidate := range []*time.Time{row.ActualPublishedAt, row.DetectedAt} {
		if candidate != nil {
			return candidate.UTC()
		}
	}
	return time.Time{}
}

func compareReferenceRows(left, right *DatasetReferenceRow) int {
	leftTime := referenceSortTime(left)
	rightTime := referenceSortTime(right)
	if left == nil || right == nil {
		return compareTimes(leftTime, rightTime)
	}
	return cmp.Or(
		compareTimes(leftTime, rightTime),
		cmp.Compare(left.ChannelID, right.ChannelID),
		cmp.Compare(left.PostID, right.PostID),
		cmp.Compare(left.AlarmType, right.AlarmType),
	)
}

func earlierTime(left, right *time.Time) *time.Time {
	if left == nil {
		return shared.CloneSendCountTime(right)
	}
	if right == nil {
		return shared.CloneSendCountTime(left)
	}
	if right.Before(left.UTC()) {
		return shared.CloneSendCountTime(right)
	}
	return shared.CloneSendCountTime(left)
}

func uniqueStrings(values []string) []string {
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

func mergeUniqueStrings(left, right []string) []string {
	merged := make([]string, 0, len(left)+len(right))
	merged = append(merged, left...)
	merged = append(merged, right...)
	return uniqueStrings(merged)
}

func buildChannelPostKey(channelID, postID string) string {
	trimmedChannelID := strings.TrimSpace(channelID)
	trimmedPostID := strings.TrimSpace(postID)
	if trimmedChannelID == "" || trimmedPostID == "" {
		return ""
	}
	return trimmedChannelID + "|" + trimmedPostID
}

func buildDatasetVerificationRows(
	verdictRows []trackingrepo.ObservationPostComparisonVerdictRow,
) []DatasetVerificationRow {
	if len(verdictRows) == 0 {
		return nil
	}

	rows := make([]DatasetVerificationRow, 0, len(verdictRows))
	for i := range verdictRows {
		verdict := verdictRows[i]
		postID := strings.TrimSpace(verdict.CanonicalPostID)
		rows = append(rows, DatasetVerificationRow{
			Verdict:                verdict.Verdict,
			Reason:                 verdict.Reason,
			AlarmType:              verdict.AlarmType,
			ChannelID:              strings.TrimSpace(verdict.ChannelID),
			PostID:                 postID,
			PostKey:                buildObservationPostKey(verdict.AlarmType, verdict.ChannelID, postID),
			ContentID:              strings.TrimSpace(verdict.ContentID),
			ActualPublishedAt:      shared.CloneSendCountTime(verdict.ActualPublishedAt),
			DetectedAt:             shared.CloneSendCountTime(verdict.DetectedAt),
			AlarmSentAt:            shared.CloneSendCountTime(verdict.AlarmSentAt),
			MatchPublishedAt:       shared.CloneSendCountTime(verdict.MatchPublishedAt),
			MatchTitleHint:         strings.TrimSpace(verdict.MatchTitleHint),
			MatchBasis:             cloneStrings(verdict.MatchBasis),
			ReviewStatus:           verdict.ReviewStatus,
			BaselineCount:          verdict.BaselineCount,
			SentCount:              verdict.SentCount,
			RelatedBaselinePostIDs: cloneStrings(verdict.RelatedBaselinePostIDs),
			RelatedSentPostIDs:     cloneStrings(verdict.RelatedSentPostIDs),
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return compareVerificationRows(&rows[i], &rows[j]) < 0
	})

	return rows
}

func compareVerificationRows(left, right *DatasetVerificationRow) int {
	leftTime := verificationSortTime(left)
	rightTime := verificationSortTime(right)
	if left == nil || right == nil {
		return compareTimes(leftTime, rightTime)
	}
	return cmp.Or(
		compareTimes(leftTime, rightTime),
		cmp.Compare(left.AlarmType, right.AlarmType),
		cmp.Compare(left.ChannelID, right.ChannelID),
		cmp.Compare(left.PostKey, right.PostKey),
		cmp.Compare(left.PostID, right.PostID),
		cmp.Compare(left.ContentID, right.ContentID),
		cmp.Compare(left.Verdict, right.Verdict),
	)
}

func verificationSortTime(row *DatasetVerificationRow) time.Time {
	if row == nil {
		return time.Time{}
	}
	for _, candidate := range []*time.Time{row.ActualPublishedAt, row.MatchPublishedAt, row.DetectedAt, row.AlarmSentAt} {
		if candidate != nil {
			return candidate.UTC()
		}
	}
	return time.Time{}
}

func compareTimes(left, right time.Time) int {
	if left.Equal(right) {
		return 0
	}
	if left.Before(right) {
		return -1
	}
	return 1
}

func cloneStrings(values []string) []string {
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

func buildObservationPostKey(alarmType domain.AlarmType, channelID, postID string) string {
	trimmedChannelID := strings.TrimSpace(channelID)
	trimmedPostID := strings.TrimSpace(postID)
	if !alarmType.IsValid() || trimmedChannelID == "" || trimmedPostID == "" {
		return ""
	}
	return strings.Join([]string{string(alarmType), trimmedChannelID, trimmedPostID}, "|")
}
