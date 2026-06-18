package observation

import (
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

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

type observationIdentifierMismatchGroups struct {
	groups map[observationIdentifierMismatchAuxiliaryKey]*observationIdentifierMismatchGroup
	order  []observationIdentifierMismatchAuxiliaryKey
}

func buildObservationIdentifierMismatchCandidates(
	baselineIndex map[observationPostComparisonKey]*observationPostComparisonAccumulator,
	baselineKeys []observationPostComparisonKey,
	sentIndex map[observationPostComparisonKey]*observationPostComparisonAccumulator,
	sentKeys []observationPostComparisonKey,
) (result1 []ObservationIdentifierMismatchCandidate, result2, result3 map[observationPostComparisonKey]struct{}) {
	groups := newObservationIdentifierMismatchGroups(len(baselineKeys) + len(sentKeys))
	appendObservationIdentifierMismatchBaselines(groups, baselineIndex, baselineKeys)
	appendObservationIdentifierMismatchSent(groups, sentIndex, sentKeys)
	return buildObservationIdentifierMismatchCandidateSet(groups, baselineKeys, sentKeys)
}

func newObservationIdentifierMismatchGroups(capacity int) *observationIdentifierMismatchGroups {
	return &observationIdentifierMismatchGroups{
		groups: make(map[observationIdentifierMismatchAuxiliaryKey]*observationIdentifierMismatchGroup, capacity),
		order:  make([]observationIdentifierMismatchAuxiliaryKey, 0, capacity),
	}
}

func (groups *observationIdentifierMismatchGroups) appendGroup(
	auxKey observationIdentifierMismatchAuxiliaryKey,
) *observationIdentifierMismatchGroup {
	group, ok := groups.groups[auxKey]
	if ok {
		return group
	}
	group = &observationIdentifierMismatchGroup{}
	groups.groups[auxKey] = group
	groups.order = append(groups.order, auxKey)
	return group
}

func appendObservationIdentifierMismatchBaselines(
	groups *observationIdentifierMismatchGroups,
	baselineIndex map[observationPostComparisonKey]*observationPostComparisonAccumulator,
	baselineKeys []observationPostComparisonKey,
) {
	for _, key := range baselineKeys {
		accumulator, ok := baselineIndex[key]
		if !ok || accumulator == nil {
			continue
		}
		auxKey, ok := buildObservationIdentifierMismatchAuxiliaryKey(&accumulator.representative)
		if !ok {
			continue
		}
		group := groups.appendGroup(auxKey)
		group.baselines = append(group.baselines, observationIdentifierMismatchGroupMember{key: key, accumulator: accumulator})
	}
}

func appendObservationIdentifierMismatchSent(
	groups *observationIdentifierMismatchGroups,
	sentIndex map[observationPostComparisonKey]*observationPostComparisonAccumulator,
	sentKeys []observationPostComparisonKey,
) {
	for _, key := range sentKeys {
		accumulator, ok := sentIndex[key]
		if !ok || accumulator == nil {
			continue
		}
		auxKey, ok := buildObservationIdentifierMismatchAuxiliaryKey(&accumulator.representative)
		if !ok {
			continue
		}
		group := groups.appendGroup(auxKey)
		group.sent = append(group.sent, observationIdentifierMismatchGroupMember{key: key, accumulator: accumulator})
	}
}

func buildObservationIdentifierMismatchCandidateSet(
	groups *observationIdentifierMismatchGroups,
	baselineKeys []observationPostComparisonKey,
	sentKeys []observationPostComparisonKey,
) (result1 []ObservationIdentifierMismatchCandidate, result2, result3 map[observationPostComparisonKey]struct{}) {
	candidates := make([]ObservationIdentifierMismatchCandidate, 0, len(groups.order))
	consumedBaseline := make(map[observationPostComparisonKey]struct{}, len(baselineKeys))
	consumedSent := make(map[observationPostComparisonKey]struct{}, len(sentKeys))
	for _, auxKey := range groups.order {
		group, ok := groups.groups[auxKey]
		if !ok || group == nil || len(group.baselines) == 0 || len(group.sent) == 0 {
			continue
		}
		candidate := buildObservationIdentifierMismatchCandidate(auxKey, group)
		candidates = append(candidates, candidate)
		consumeObservationIdentifierMismatchGroup(group, consumedBaseline, consumedSent)
	}

	return candidates, consumedBaseline, consumedSent
}

func consumeObservationIdentifierMismatchGroup(
	group *observationIdentifierMismatchGroup,
	consumedBaseline map[observationPostComparisonKey]struct{},
	consumedSent map[observationPostComparisonKey]struct{},
) {
	for _, member := range group.baselines {
		consumedBaseline[member.key] = struct{}{}
	}
	for _, member := range group.sent {
		consumedSent[member.key] = struct{}{}
	}
}

func buildObservationIdentifierMismatchAuxiliaryKey(
	input *ObservationPostComparisonInput,
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
