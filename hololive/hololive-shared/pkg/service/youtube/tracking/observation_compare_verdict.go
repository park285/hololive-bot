package tracking

import "strings"

func buildObservationPostComparisonVerdictRows(
	result ObservationPostComparisonResult,
) []ObservationPostComparisonVerdictRow {
	rows := make([]ObservationPostComparisonVerdictRow, 0,
		len(result.MatchedRows)+
			len(result.UnsentRows)+
			len(result.DuplicateSentRows)+
			len(result.UnexpectedSentRows)+
			len(result.IdentifierMismatchCandidates),
	)

	for i := range result.MatchedRows {
		rows = append(rows, buildObservationPostComparisonVerdictRowFromRow(
			result.MatchedRows[i],
			ObservationPostComparisonVerdictMatched,
			ObservationPostComparisonVerdictReasonCanonicalIdentifierMatched,
		))
	}
	for i := range result.UnsentRows {
		rows = append(rows, buildObservationPostComparisonVerdictRowFromRow(
			result.UnsentRows[i],
			ObservationPostComparisonVerdictUnsent,
			ObservationPostComparisonVerdictReasonBaselineWithoutSentHistory,
		))
	}
	for i := range result.DuplicateSentRows {
		rows = append(rows, buildObservationPostComparisonVerdictRowFromRow(
			result.DuplicateSentRows[i],
			ObservationPostComparisonVerdictDuplicateSent,
			ObservationPostComparisonVerdictReasonMultipleSentRowsForCanonicalPost,
		))
	}
	for i := range result.UnexpectedSentRows {
		rows = append(rows, buildObservationPostComparisonVerdictRowFromRow(
			result.UnexpectedSentRows[i],
			ObservationPostComparisonVerdictUnexpectedSent,
			ObservationPostComparisonVerdictReasonSentHistoryWithoutBaseline,
		))
	}
	for i := range result.IdentifierMismatchCandidates {
		rows = append(rows, buildObservationPostComparisonVerdictRowFromCandidate(result.IdentifierMismatchCandidates[i]))
	}

	return rows
}

func buildObservationPostComparisonVerdictRowFromRow(
	row ObservationPostComparisonRow,
	verdict ObservationPostComparisonVerdict,
	reason ObservationPostComparisonVerdictReason,
) ObservationPostComparisonVerdictRow {
	return ObservationPostComparisonVerdictRow{
		Verdict:           verdict,
		Reason:            reason,
		Kind:              row.Kind,
		AlarmType:         row.AlarmType,
		ChannelID:         strings.TrimSpace(row.ChannelID),
		CanonicalPostID:   strings.TrimSpace(row.CanonicalPostID),
		ContentID:         strings.TrimSpace(row.ContentID),
		TitleHint:         observationComparisonNormalizeTitleHint(row.TitleHint),
		ActualPublishedAt: cloneObservationComparisonTime(row.ActualPublishedAt),
		DetectedAt:        cloneObservationComparisonTime(row.DetectedAt),
		AlarmSentAt:       cloneObservationComparisonTime(row.AlarmSentAt),
		BaselineCount:     row.BaselineCount,
		SentCount:         row.SentCount,
	}
}

func buildObservationPostComparisonVerdictRowFromCandidate(
	candidate ObservationIdentifierMismatchCandidate,
) ObservationPostComparisonVerdictRow {
	return ObservationPostComparisonVerdictRow{
		Verdict:                ObservationPostComparisonVerdictIdentifierMismatchCandidate,
		Reason:                 ObservationPostComparisonVerdictReasonAuxiliaryMetadataPendingReview,
		Kind:                   candidate.Kind,
		AlarmType:              candidate.AlarmType,
		ChannelID:              strings.TrimSpace(candidate.ChannelID),
		MatchPublishedAt:       cloneObservationComparisonTime(candidate.MatchPublishedAt),
		MatchTitleHint:         observationComparisonNormalizeTitleHint(candidate.MatchTitleHint),
		MatchBasis:             cloneObservationPostComparisonMatchBasis(candidate.MatchBasis),
		ReviewStatus:           candidate.ReviewStatus,
		BaselineCount:          len(candidate.BaselineRows),
		SentCount:              len(candidate.SentRows),
		RelatedBaselinePostIDs: collectObservationPostComparisonCanonicalPostIDs(candidate.BaselineRows),
		RelatedSentPostIDs:     collectObservationPostComparisonCanonicalPostIDs(candidate.SentRows),
	}
}

func collectObservationPostComparisonCanonicalPostIDs(rows []ObservationPostComparisonRow) []string {
	ids := make([]string, 0, len(rows))
	for i := range rows {
		canonicalPostID := strings.TrimSpace(rows[i].CanonicalPostID)
		if canonicalPostID == "" {
			canonicalPostID = strings.TrimSpace(rows[i].ContentID)
		}
		if canonicalPostID == "" {
			continue
		}
		ids = append(ids, canonicalPostID)
	}
	return ids
}

func cloneObservationPostComparisonMatchBasis(values []string) []string {
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
