package tracking

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type ObservationPostComparisonSummary struct {
	BaselineInputCount               int `json:"baseline_input_count"`
	BaselineUniquePostCount          int `json:"baseline_unique_post_count"`
	BaselineDuplicateInputCount      int `json:"baseline_duplicate_input_count"`
	SentInputCount                   int `json:"sent_input_count"`
	SentUniquePostCount              int `json:"sent_unique_post_count"`
	SentDuplicateInputCount          int `json:"sent_duplicate_input_count"`
	MatchedPostCount                 int `json:"matched_post_count"`
	UnsentPostCount                  int `json:"unsent_post_count"`
	DuplicateSentPostCount           int `json:"duplicate_sent_post_count"`
	UnexpectedSentPostCount          int `json:"unexpected_sent_post_count"`
	IdentifierMismatchCandidateCount int `json:"identifier_mismatch_candidate_count"`
}

type ObservationPostComparisonVerdict string

const (
	ObservationPostComparisonVerdictMatched                     ObservationPostComparisonVerdict = "matched"
	ObservationPostComparisonVerdictUnsent                      ObservationPostComparisonVerdict = "unsent"
	ObservationPostComparisonVerdictDuplicateSent               ObservationPostComparisonVerdict = "duplicate_sent"
	ObservationPostComparisonVerdictUnexpectedSent              ObservationPostComparisonVerdict = "unexpected_sent"
	ObservationPostComparisonVerdictIdentifierMismatchCandidate ObservationPostComparisonVerdict = "identifier_mismatch_candidate"
)

type ObservationPostComparisonVerdictReason string

const (
	ObservationPostComparisonVerdictReasonCanonicalIdentifierMatched       ObservationPostComparisonVerdictReason = "canonical_identifier_matched"
	ObservationPostComparisonVerdictReasonBaselineWithoutSentHistory       ObservationPostComparisonVerdictReason = "baseline_without_sent_history"
	ObservationPostComparisonVerdictReasonMultipleSentRowsForCanonicalPost ObservationPostComparisonVerdictReason = "multiple_sent_rows_for_canonical_post"
	ObservationPostComparisonVerdictReasonSentHistoryWithoutBaseline       ObservationPostComparisonVerdictReason = "sent_history_without_baseline"
	ObservationPostComparisonVerdictReasonAuxiliaryMetadataPendingReview   ObservationPostComparisonVerdictReason = "auxiliary_metadata_match_pending_review"
)

type ObservationPostComparisonRow struct {
	Kind              domain.OutboxKind `json:"kind"`
	AlarmType         domain.AlarmType  `json:"alarm_type"`
	ChannelID         string            `json:"channel_id"`
	CanonicalPostID   string            `json:"canonical_post_id"`
	ContentID         string            `json:"content_id,omitempty"`
	TitleHint         string            `json:"title_hint,omitempty"`
	ActualPublishedAt *time.Time        `json:"actual_published_at,omitempty"`
	DetectedAt        *time.Time        `json:"detected_at,omitempty"`
	AlarmSentAt       *time.Time        `json:"alarm_sent_at,omitempty"`
	BaselineCount     int               `json:"baseline_count"`
	SentCount         int               `json:"sent_count"`
}

type ObservationIdentifierMismatchCandidateReviewStatus string

const ObservationIdentifierMismatchCandidateReviewStatusPendingReview ObservationIdentifierMismatchCandidateReviewStatus = "pending_review"

type ObservationIdentifierMismatchCandidate struct {
	Kind             domain.OutboxKind                                  `json:"kind"`
	AlarmType        domain.AlarmType                                   `json:"alarm_type"`
	ChannelID        string                                             `json:"channel_id"`
	MatchPublishedAt *time.Time                                         `json:"match_published_at,omitempty"`
	MatchTitleHint   string                                             `json:"match_title_hint,omitempty"`
	MatchBasis       []string                                           `json:"match_basis"`
	ReviewStatus     ObservationIdentifierMismatchCandidateReviewStatus `json:"review_status"`
	BaselineRows     []ObservationPostComparisonRow                     `json:"baseline_rows"`
	SentRows         []ObservationPostComparisonRow                     `json:"sent_rows"`
}

type ObservationPostComparisonVerdictRow struct {
	Verdict                ObservationPostComparisonVerdict                   `json:"verdict"`
	Reason                 ObservationPostComparisonVerdictReason             `json:"reason"`
	Kind                   domain.OutboxKind                                  `json:"kind"`
	AlarmType              domain.AlarmType                                   `json:"alarm_type"`
	ChannelID              string                                             `json:"channel_id"`
	CanonicalPostID        string                                             `json:"canonical_post_id,omitempty"`
	ContentID              string                                             `json:"content_id,omitempty"`
	TitleHint              string                                             `json:"title_hint,omitempty"`
	ActualPublishedAt      *time.Time                                         `json:"actual_published_at,omitempty"`
	DetectedAt             *time.Time                                         `json:"detected_at,omitempty"`
	AlarmSentAt            *time.Time                                         `json:"alarm_sent_at,omitempty"`
	BaselineCount          int                                                `json:"baseline_count"`
	SentCount              int                                                `json:"sent_count"`
	MatchPublishedAt       *time.Time                                         `json:"match_published_at,omitempty"`
	MatchTitleHint         string                                             `json:"match_title_hint,omitempty"`
	MatchBasis             []string                                           `json:"match_basis,omitempty"`
	ReviewStatus           ObservationIdentifierMismatchCandidateReviewStatus `json:"review_status,omitempty"`
	RelatedBaselinePostIDs []string                                           `json:"related_baseline_post_ids,omitempty"`
	RelatedSentPostIDs     []string                                           `json:"related_sent_post_ids,omitempty"`
}

type ObservationPostComparisonResult struct {
	Summary                      ObservationPostComparisonSummary         `json:"summary"`
	MatchedRows                  []ObservationPostComparisonRow           `json:"matched_rows"`
	UnsentRows                   []ObservationPostComparisonRow           `json:"unsent_rows"`
	DuplicateSentRows            []ObservationPostComparisonRow           `json:"duplicate_sent_rows"`
	UnexpectedSentRows           []ObservationPostComparisonRow           `json:"unexpected_sent_rows"`
	IdentifierMismatchCandidates []ObservationIdentifierMismatchCandidate `json:"identifier_mismatch_candidates"`
	VerdictRows                  []ObservationPostComparisonVerdictRow    `json:"verdict_rows"`
}

func CompareObservationPostInputs(
	baselineInputs []ObservationPostComparisonInput,
	sentInputs []ObservationPostComparisonInput,
) ObservationPostComparisonResult {
	baselineIndex, baselineKeys, baselineDuplicateInputCount := indexObservationPostComparisonInputs(baselineInputs)
	sentIndex, sentKeys, sentDuplicateInputCount := indexObservationPostComparisonInputs(sentInputs)
	result := newObservationPostComparisonResult(
		baselineInputs,
		baselineKeys,
		baselineDuplicateInputCount,
		sentInputs,
		sentKeys,
		sentDuplicateInputCount,
	)

	unmatchedBaselineKeys := appendObservationPostComparisonMatchedRows(&result, baselineIndex, baselineKeys, sentIndex)
	unmatchedSentKeys := collectObservationPostComparisonUnmatchedSentKeys(sentKeys, baselineIndex)

	mismatchCandidates, consumedBaseline, consumedSent := buildObservationIdentifierMismatchCandidates(
		baselineIndex,
		unmatchedBaselineKeys,
		sentIndex,
		unmatchedSentKeys,
	)
	result.IdentifierMismatchCandidates = mismatchCandidates

	appendObservationPostComparisonUnsentRows(&result, baselineIndex, unmatchedBaselineKeys, consumedBaseline)
	appendObservationPostComparisonUnexpectedSentRows(&result, sentIndex, unmatchedSentKeys, consumedSent)
	finalizeObservationPostComparisonResult(&result)

	return result
}

func newObservationPostComparisonResult(
	baselineInputs []ObservationPostComparisonInput,
	baselineKeys []observationPostComparisonKey,
	baselineDuplicateInputCount int,
	sentInputs []ObservationPostComparisonInput,
	sentKeys []observationPostComparisonKey,
	sentDuplicateInputCount int,
) ObservationPostComparisonResult {
	return ObservationPostComparisonResult{
		Summary: ObservationPostComparisonSummary{
			BaselineInputCount:          len(baselineInputs),
			BaselineUniquePostCount:     len(baselineKeys),
			BaselineDuplicateInputCount: baselineDuplicateInputCount,
			SentInputCount:              len(sentInputs),
			SentUniquePostCount:         len(sentKeys),
			SentDuplicateInputCount:     sentDuplicateInputCount,
		},
		MatchedRows:                  make([]ObservationPostComparisonRow, 0, len(baselineKeys)),
		UnsentRows:                   make([]ObservationPostComparisonRow, 0, len(baselineKeys)),
		DuplicateSentRows:            make([]ObservationPostComparisonRow, 0, len(sentKeys)),
		UnexpectedSentRows:           make([]ObservationPostComparisonRow, 0, len(sentKeys)),
		IdentifierMismatchCandidates: make([]ObservationIdentifierMismatchCandidate, 0),
		VerdictRows:                  make([]ObservationPostComparisonVerdictRow, 0, len(baselineKeys)+len(sentKeys)),
	}
}

func appendObservationPostComparisonMatchedRows(
	result *ObservationPostComparisonResult,
	baselineIndex map[observationPostComparisonKey]*observationPostComparisonAccumulator,
	baselineKeys []observationPostComparisonKey,
	sentIndex map[observationPostComparisonKey]*observationPostComparisonAccumulator,
) []observationPostComparisonKey {
	unmatchedBaselineKeys := make([]observationPostComparisonKey, 0, len(baselineKeys))
	for _, key := range baselineKeys {
		baseline := baselineIndex[key]
		sent, ok := sentIndex[key]
		if appendObservationPostComparisonKnownBaselineRow(result, baseline, sent, ok) {
			unmatchedBaselineKeys = append(unmatchedBaselineKeys, key)
		}
	}
	return unmatchedBaselineKeys
}

func appendObservationPostComparisonKnownBaselineRow(
	result *ObservationPostComparisonResult,
	baseline *observationPostComparisonAccumulator,
	sent *observationPostComparisonAccumulator,
	sentExists bool,
) bool {
	if !sentExists {
		return true
	}
	if sent.count > 1 {
		result.DuplicateSentRows = append(result.DuplicateSentRows, buildObservationPostComparisonRow(baseline, sent))
		return false
	}
	result.MatchedRows = append(result.MatchedRows, buildObservationPostComparisonRow(baseline, sent))
	return false
}

func collectObservationPostComparisonUnmatchedSentKeys(
	sentKeys []observationPostComparisonKey,
	baselineIndex map[observationPostComparisonKey]*observationPostComparisonAccumulator,
) []observationPostComparisonKey {
	unmatchedSentKeys := make([]observationPostComparisonKey, 0, len(sentKeys))
	for _, key := range sentKeys {
		if _, ok := baselineIndex[key]; ok {
			continue
		}
		unmatchedSentKeys = append(unmatchedSentKeys, key)
	}
	return unmatchedSentKeys
}

func appendObservationPostComparisonUnsentRows(
	result *ObservationPostComparisonResult,
	baselineIndex map[observationPostComparisonKey]*observationPostComparisonAccumulator,
	unmatchedBaselineKeys []observationPostComparisonKey,
	consumedBaseline map[observationPostComparisonKey]struct{},
) {
	for _, key := range unmatchedBaselineKeys {
		if _, ok := consumedBaseline[key]; ok {
			continue
		}
		result.UnsentRows = append(result.UnsentRows, buildObservationPostComparisonRow(baselineIndex[key], nil))
	}
}

func appendObservationPostComparisonUnexpectedSentRows(
	result *ObservationPostComparisonResult,
	sentIndex map[observationPostComparisonKey]*observationPostComparisonAccumulator,
	unmatchedSentKeys []observationPostComparisonKey,
	consumedSent map[observationPostComparisonKey]struct{},
) {
	for _, key := range unmatchedSentKeys {
		if _, ok := consumedSent[key]; ok {
			continue
		}
		result.UnexpectedSentRows = append(result.UnexpectedSentRows, buildObservationPostComparisonRow(nil, sentIndex[key]))
	}
}

func finalizeObservationPostComparisonResult(result *ObservationPostComparisonResult) {
	sortObservationPostComparisonRows(result.MatchedRows)
	sortObservationPostComparisonRows(result.UnsentRows)
	sortObservationPostComparisonRows(result.DuplicateSentRows)
	sortObservationPostComparisonRows(result.UnexpectedSentRows)
	sortObservationIdentifierMismatchCandidates(result.IdentifierMismatchCandidates)

	result.Summary.MatchedPostCount = len(result.MatchedRows)
	result.Summary.UnsentPostCount = len(result.UnsentRows)
	result.Summary.DuplicateSentPostCount = len(result.DuplicateSentRows)
	result.Summary.UnexpectedSentPostCount = len(result.UnexpectedSentRows)
	result.Summary.IdentifierMismatchCandidateCount = len(result.IdentifierMismatchCandidates)
	result.VerdictRows = buildObservationPostComparisonVerdictRows(*result)
}
