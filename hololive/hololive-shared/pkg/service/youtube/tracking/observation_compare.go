package tracking

import (
	"sort"
	"strings"
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

type observationPostComparisonKey struct {
	kind            domain.OutboxKind
	channelID       string
	canonicalPostID string
}

type observationPostComparisonAccumulator struct {
	representative ObservationPostComparisonInput
	count          int
}

type observationIdentifierMismatchAuxiliaryKey struct {
	kind           domain.OutboxKind
	channelID      string
	publishedAtKey time.Time
	titleHintKey   string
}

type observationIdentifierMismatchGroupMember struct {
	key         observationPostComparisonKey
	accumulator *observationPostComparisonAccumulator
}

type observationIdentifierMismatchGroup struct {
	baselines []observationIdentifierMismatchGroupMember
	sent      []observationIdentifierMismatchGroupMember
}

func CompareObservationPostInputs(
	baselineInputs []ObservationPostComparisonInput,
	sentInputs []ObservationPostComparisonInput,
) ObservationPostComparisonResult {
	baselineIndex, baselineKeys, baselineDuplicateInputCount := indexObservationPostComparisonInputs(baselineInputs)
	sentIndex, sentKeys, sentDuplicateInputCount := indexObservationPostComparisonInputs(sentInputs)

	result := ObservationPostComparisonResult{
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

	unmatchedBaselineKeys := make([]observationPostComparisonKey, 0, len(baselineKeys))
	unmatchedSentKeys := make([]observationPostComparisonKey, 0, len(sentKeys))

	for _, key := range baselineKeys {
		baseline := baselineIndex[key]
		sent, ok := sentIndex[key]
		switch {
		case !ok:
			unmatchedBaselineKeys = append(unmatchedBaselineKeys, key)
		case sent.count > 1:
			result.DuplicateSentRows = append(result.DuplicateSentRows, buildObservationPostComparisonRow(baseline, sent))
		default:
			result.MatchedRows = append(result.MatchedRows, buildObservationPostComparisonRow(baseline, sent))
		}
	}

	for _, key := range sentKeys {
		if _, ok := baselineIndex[key]; ok {
			continue
		}
		unmatchedSentKeys = append(unmatchedSentKeys, key)
	}

	mismatchCandidates, consumedBaseline, consumedSent := buildObservationIdentifierMismatchCandidates(
		baselineIndex,
		unmatchedBaselineKeys,
		sentIndex,
		unmatchedSentKeys,
	)
	result.IdentifierMismatchCandidates = mismatchCandidates

	for _, key := range unmatchedBaselineKeys {
		if _, ok := consumedBaseline[key]; ok {
			continue
		}
		result.UnsentRows = append(result.UnsentRows, buildObservationPostComparisonRow(baselineIndex[key], nil))
	}

	for _, key := range unmatchedSentKeys {
		if _, ok := consumedSent[key]; ok {
			continue
		}
		result.UnexpectedSentRows = append(result.UnexpectedSentRows, buildObservationPostComparisonRow(nil, sentIndex[key]))
	}

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
	result.VerdictRows = buildObservationPostComparisonVerdictRows(result)

	return result
}

func indexObservationPostComparisonInputs(
	inputs []ObservationPostComparisonInput,
) (map[observationPostComparisonKey]*observationPostComparisonAccumulator, []observationPostComparisonKey, int) {
	index := make(map[observationPostComparisonKey]*observationPostComparisonAccumulator, len(inputs))
	keys := make([]observationPostComparisonKey, 0, len(inputs))
	duplicateInputCount := 0

	for i := range inputs {
		normalized := normalizeObservationPostComparisonComparableInput(inputs[i])
		key := buildObservationPostComparisonKey(normalized, i)
		if accumulator, ok := index[key]; ok {
			accumulator.representative = mergeObservationPostComparisonInputs(accumulator.representative, normalized)
			accumulator.count++
			duplicateInputCount++
			continue
		}

		index[key] = &observationPostComparisonAccumulator{
			representative: normalized,
			count:          1,
		}
		keys = append(keys, key)
	}

	sort.SliceStable(keys, func(i, j int) bool {
		if keys[i].kind != keys[j].kind {
			return keys[i].kind < keys[j].kind
		}
		if keys[i].channelID != keys[j].channelID {
			return keys[i].channelID < keys[j].channelID
		}
		return keys[i].canonicalPostID < keys[j].canonicalPostID
	})

	return index, keys, duplicateInputCount
}

func normalizeObservationPostComparisonComparableInput(input ObservationPostComparisonInput) ObservationPostComparisonInput {
	normalizedKind := input.Kind
	normalizedAlarmType := normalizedKind.ToAlarmType()
	if normalizedKind == "" && input.AlarmType.IsValid() {
		normalizedAlarmType = input.AlarmType
	}

	canonicalPostID := normalizeObservationComparisonCanonicalPostID(
		normalizedKind,
		input.CanonicalPostID,
		input.ContentID,
	)
	if canonicalPostID == "" {
		canonicalPostID = strings.TrimSpace(input.CanonicalPostID)
	}
	if canonicalPostID == "" {
		canonicalPostID = strings.TrimSpace(input.ContentID)
	}

	return ObservationPostComparisonInput{
		Kind:              normalizedKind,
		AlarmType:         normalizedAlarmType,
		CanonicalPostID:   canonicalPostID,
		ContentID:         strings.TrimSpace(input.ContentID),
		ChannelID:         strings.TrimSpace(input.ChannelID),
		TitleHint:         observationComparisonNormalizeTitleHint(input.TitleHint),
		ActualPublishedAt: cloneObservationComparisonTime(input.ActualPublishedAt),
		DetectedAt:        cloneObservationComparisonTime(input.DetectedAt),
		AlarmSentAt:       cloneObservationComparisonTime(input.AlarmSentAt),
	}
}

func buildObservationPostComparisonKey(
	input ObservationPostComparisonInput,
	index int,
) observationPostComparisonKey {
	canonicalPostID := strings.TrimSpace(input.CanonicalPostID)
	if canonicalPostID == "" {
		canonicalPostID = strings.TrimSpace(input.ContentID)
	}
	if canonicalPostID == "" {
		canonicalPostID = "__missing_post_id__:" + strings.TrimSpace(input.ChannelID) + ":" + timeValueForObservationPostComparison(input.DetectedAt).Format(time.RFC3339Nano)
		if canonicalPostID == "__missing_post_id__::0001-01-01T00:00:00Z" {
			canonicalPostID = "__missing_post_id__:" + strings.TrimSpace(input.ChannelID) + ":" + timeValueForObservationPostComparison(input.AlarmSentAt).Format(time.RFC3339Nano)
		}
		if canonicalPostID == "__missing_post_id__::0001-01-01T00:00:00Z" {
			canonicalPostID = "__missing_post_id__:" + strings.TrimSpace(input.ChannelID) + ":" + timeValueForObservationPostComparison(input.ActualPublishedAt).Format(time.RFC3339Nano)
		}
		if canonicalPostID == "__missing_post_id__::0001-01-01T00:00:00Z" {
			canonicalPostID = "__missing_post_id__:" + strings.TrimSpace(input.ChannelID) + ":idx:" + time.Duration(index).String()
		}
	}

	return observationPostComparisonKey{
		kind:            input.Kind,
		channelID:       strings.TrimSpace(input.ChannelID),
		canonicalPostID: canonicalPostID,
	}
}

func mergeObservationPostComparisonInputs(
	left ObservationPostComparisonInput,
	right ObservationPostComparisonInput,
) ObservationPostComparisonInput {
	merged := left
	if merged.Kind == "" && right.Kind != "" {
		merged.Kind = right.Kind
	}
	if merged.AlarmType == "" && right.AlarmType != "" {
		merged.AlarmType = right.AlarmType
	}
	if strings.TrimSpace(merged.CanonicalPostID) == "" && strings.TrimSpace(right.CanonicalPostID) != "" {
		merged.CanonicalPostID = strings.TrimSpace(right.CanonicalPostID)
	}
	if strings.TrimSpace(merged.ContentID) == "" && strings.TrimSpace(right.ContentID) != "" {
		merged.ContentID = strings.TrimSpace(right.ContentID)
	}
	if strings.TrimSpace(merged.ChannelID) == "" && strings.TrimSpace(right.ChannelID) != "" {
		merged.ChannelID = strings.TrimSpace(right.ChannelID)
	}
	if strings.TrimSpace(merged.TitleHint) == "" && strings.TrimSpace(right.TitleHint) != "" {
		merged.TitleHint = observationComparisonNormalizeTitleHint(right.TitleHint)
	}
	merged.ActualPublishedAt = earliestObservationPostComparisonTime(merged.ActualPublishedAt, right.ActualPublishedAt)
	merged.DetectedAt = earliestObservationPostComparisonTime(merged.DetectedAt, right.DetectedAt)
	merged.AlarmSentAt = earliestObservationPostComparisonTime(merged.AlarmSentAt, right.AlarmSentAt)
	return merged
}

func earliestObservationPostComparisonTime(left *time.Time, right *time.Time) *time.Time {
	if left == nil {
		return cloneObservationComparisonTime(right)
	}
	if right == nil {
		return cloneObservationComparisonTime(left)
	}
	if right.Before(*left) {
		return cloneObservationComparisonTime(right)
	}
	return cloneObservationComparisonTime(left)
}

func buildObservationPostComparisonRow(
	baseline *observationPostComparisonAccumulator,
	sent *observationPostComparisonAccumulator,
) ObservationPostComparisonRow {
	var merged ObservationPostComparisonInput
	if baseline != nil {
		merged = baseline.representative
	}
	if sent != nil {
		merged = mergeObservationPostComparisonInputs(merged, sent.representative)
	}

	row := ObservationPostComparisonRow{
		Kind:              merged.Kind,
		AlarmType:         merged.AlarmType,
		ChannelID:         strings.TrimSpace(merged.ChannelID),
		CanonicalPostID:   strings.TrimSpace(merged.CanonicalPostID),
		ContentID:         strings.TrimSpace(merged.ContentID),
		TitleHint:         observationComparisonNormalizeTitleHint(merged.TitleHint),
		ActualPublishedAt: cloneObservationComparisonTime(merged.ActualPublishedAt),
		DetectedAt:        cloneObservationComparisonTime(merged.DetectedAt),
		AlarmSentAt:       cloneObservationComparisonTime(merged.AlarmSentAt),
	}
	if baseline != nil {
		row.BaselineCount = baseline.count
	}
	if sent != nil {
		row.SentCount = sent.count
	}
	return row
}

func buildObservationIdentifierMismatchCandidates(
	baselineIndex map[observationPostComparisonKey]*observationPostComparisonAccumulator,
	baselineKeys []observationPostComparisonKey,
	sentIndex map[observationPostComparisonKey]*observationPostComparisonAccumulator,
	sentKeys []observationPostComparisonKey,
) ([]ObservationIdentifierMismatchCandidate, map[observationPostComparisonKey]struct{}, map[observationPostComparisonKey]struct{}) {
	groups := make(map[observationIdentifierMismatchAuxiliaryKey]*observationIdentifierMismatchGroup, len(baselineKeys)+len(sentKeys))
	order := make([]observationIdentifierMismatchAuxiliaryKey, 0, len(baselineKeys)+len(sentKeys))

	appendGroup := func(auxKey observationIdentifierMismatchAuxiliaryKey) *observationIdentifierMismatchGroup {
		group, ok := groups[auxKey]
		if ok {
			return group
		}
		group = &observationIdentifierMismatchGroup{}
		groups[auxKey] = group
		order = append(order, auxKey)
		return group
	}

	for _, key := range baselineKeys {
		accumulator := baselineIndex[key]
		auxKey, ok := buildObservationIdentifierMismatchAuxiliaryKey(accumulator.representative)
		if !ok {
			continue
		}
		group := appendGroup(auxKey)
		group.baselines = append(group.baselines, observationIdentifierMismatchGroupMember{key: key, accumulator: accumulator})
	}

	for _, key := range sentKeys {
		accumulator := sentIndex[key]
		auxKey, ok := buildObservationIdentifierMismatchAuxiliaryKey(accumulator.representative)
		if !ok {
			continue
		}
		group := appendGroup(auxKey)
		group.sent = append(group.sent, observationIdentifierMismatchGroupMember{key: key, accumulator: accumulator})
	}

	candidates := make([]ObservationIdentifierMismatchCandidate, 0, len(order))
	consumedBaseline := make(map[observationPostComparisonKey]struct{}, len(baselineKeys))
	consumedSent := make(map[observationPostComparisonKey]struct{}, len(sentKeys))
	for _, auxKey := range order {
		group := groups[auxKey]
		if len(group.baselines) == 0 || len(group.sent) == 0 {
			continue
		}
		candidates = append(candidates, buildObservationIdentifierMismatchCandidate(auxKey, group))
		for _, member := range group.baselines {
			consumedBaseline[member.key] = struct{}{}
		}
		for _, member := range group.sent {
			consumedSent[member.key] = struct{}{}
		}
	}

	return candidates, consumedBaseline, consumedSent
}

func buildObservationIdentifierMismatchAuxiliaryKey(
	input ObservationPostComparisonInput,
) (observationIdentifierMismatchAuxiliaryKey, bool) {
	kind := input.Kind
	channelID := strings.TrimSpace(input.ChannelID)
	publishedAt := timeValueForObservationPostComparison(input.ActualPublishedAt)
	if kind == "" || channelID == "" || publishedAt.IsZero() {
		return observationIdentifierMismatchAuxiliaryKey{}, false
	}

	titleHintKey := observationComparisonTitleHintKey(input.TitleHint)
	publishedAtKey := publishedAt.UTC()
	if titleHintKey != "" {
		publishedAtKey = publishedAtKey.Truncate(time.Minute)
	}

	return observationIdentifierMismatchAuxiliaryKey{
		kind:           kind,
		channelID:      channelID,
		publishedAtKey: publishedAtKey,
		titleHintKey:   titleHintKey,
	}, true
}

func buildObservationIdentifierMismatchCandidate(
	auxKey observationIdentifierMismatchAuxiliaryKey,
	group *observationIdentifierMismatchGroup,
) ObservationIdentifierMismatchCandidate {
	baselineRows := make([]ObservationPostComparisonRow, 0, len(group.baselines))
	for i := range group.baselines {
		baselineRows = append(baselineRows, buildObservationPostComparisonRow(group.baselines[i].accumulator, nil))
	}
	sentRows := make([]ObservationPostComparisonRow, 0, len(group.sent))
	for i := range group.sent {
		sentRows = append(sentRows, buildObservationPostComparisonRow(nil, group.sent[i].accumulator))
	}

	sortObservationPostComparisonRows(baselineRows)
	sortObservationPostComparisonRows(sentRows)

	matchBasis := []string{"actual_published_at"}
	matchTitleHint := resolveObservationIdentifierMismatchTitleHint(baselineRows, sentRows)
	if matchTitleHint != "" {
		matchBasis = append(matchBasis, "title_hint")
	}

	return ObservationIdentifierMismatchCandidate{
		Kind:             auxKey.kind,
		AlarmType:        auxKey.kind.ToAlarmType(),
		ChannelID:        auxKey.channelID,
		MatchPublishedAt: resolveObservationIdentifierMismatchPublishedAt(baselineRows, sentRows),
		MatchTitleHint:   matchTitleHint,
		MatchBasis:       matchBasis,
		ReviewStatus:     ObservationIdentifierMismatchCandidateReviewStatusPendingReview,
		BaselineRows:     baselineRows,
		SentRows:         sentRows,
	}
}

func resolveObservationIdentifierMismatchPublishedAt(
	baselineRows []ObservationPostComparisonRow,
	sentRows []ObservationPostComparisonRow,
) *time.Time {
	var resolved *time.Time
	resolved = resolveObservationIdentifierMismatchPublishedAtRows(resolved, baselineRows)
	resolved = resolveObservationIdentifierMismatchPublishedAtRows(resolved, sentRows)
	return resolved
}

func resolveObservationIdentifierMismatchTitleHint(
	baselineRows []ObservationPostComparisonRow,
	sentRows []ObservationPostComparisonRow,
) string {
	if normalized := resolveObservationIdentifierMismatchTitleHintRows(baselineRows); normalized != "" {
		return normalized
	}
	if normalized := resolveObservationIdentifierMismatchTitleHintRows(sentRows); normalized != "" {
		return normalized
	}
	return ""
}

func resolveObservationIdentifierMismatchPublishedAtRows(
	resolved *time.Time,
	rows []ObservationPostComparisonRow,
) *time.Time {
	for i := range rows {
		if rows[i].ActualPublishedAt == nil {
			continue
		}
		resolved = earliestObservationPostComparisonTime(resolved, rows[i].ActualPublishedAt)
	}

	return resolved
}

func resolveObservationIdentifierMismatchTitleHintRows(rows []ObservationPostComparisonRow) string {
	for i := range rows {
		if normalized := observationComparisonNormalizeTitleHint(rows[i].TitleHint); normalized != "" {
			return normalized
		}
	}

	return ""
}

func sortObservationIdentifierMismatchCandidates(candidates []ObservationIdentifierMismatchCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Kind != candidates[j].Kind {
			return candidates[i].Kind < candidates[j].Kind
		}
		if candidates[i].ChannelID != candidates[j].ChannelID {
			return candidates[i].ChannelID < candidates[j].ChannelID
		}
		leftPublishedAt := timeValueForObservationPostComparison(candidates[i].MatchPublishedAt)
		rightPublishedAt := timeValueForObservationPostComparison(candidates[j].MatchPublishedAt)
		if !leftPublishedAt.Equal(rightPublishedAt) {
			return leftPublishedAt.Before(rightPublishedAt)
		}
		if candidates[i].MatchTitleHint != candidates[j].MatchTitleHint {
			return candidates[i].MatchTitleHint < candidates[j].MatchTitleHint
		}
		return len(candidates[i].BaselineRows) < len(candidates[j].BaselineRows)
	})
}

func sortObservationPostComparisonRows(rows []ObservationPostComparisonRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		if rows[i].ChannelID != rows[j].ChannelID {
			return rows[i].ChannelID < rows[j].ChannelID
		}
		if rows[i].CanonicalPostID != rows[j].CanonicalPostID {
			return rows[i].CanonicalPostID < rows[j].CanonicalPostID
		}
		leftDetectedAt := timeValueForObservationPostComparison(rows[i].DetectedAt)
		rightDetectedAt := timeValueForObservationPostComparison(rows[j].DetectedAt)
		if !leftDetectedAt.Equal(rightDetectedAt) {
			return leftDetectedAt.Before(rightDetectedAt)
		}
		leftAlarmSentAt := timeValueForObservationPostComparison(rows[i].AlarmSentAt)
		rightAlarmSentAt := timeValueForObservationPostComparison(rows[j].AlarmSentAt)
		if !leftAlarmSentAt.Equal(rightAlarmSentAt) {
			return leftAlarmSentAt.Before(rightAlarmSentAt)
		}
		if strings.TrimSpace(rows[i].ContentID) != strings.TrimSpace(rows[j].ContentID) {
			return strings.TrimSpace(rows[i].ContentID) < strings.TrimSpace(rows[j].ContentID)
		}
		return observationComparisonTitleHintKey(rows[i].TitleHint) < observationComparisonTitleHintKey(rows[j].TitleHint)
	})
}

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

func observationComparisonTitleHintKey(value string) string {
	return strings.ToLower(observationComparisonNormalizeTitleHint(value))
}

func timeValueForObservationPostComparison(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}
