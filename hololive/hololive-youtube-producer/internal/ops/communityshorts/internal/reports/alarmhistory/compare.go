package alarmhistory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"

	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

func buildObservationAlarmSentHistoryComparison(
	ctx context.Context,
	repository *trackingrepo.GormRepository,
	windowRuntimeName string,
	windowBigBangCutoverAt time.Time,
	windowStart time.Time,
	windowEnd time.Time,
	kind domain.OutboxKind,
) (trackingrepo.ObservationPostComparisonResult, error) {
	baselines, err := repository.ListCommunityShortsObservationPostBaselines(ctx, windowRuntimeName, windowBigBangCutoverAt)
	if err != nil {
		return trackingrepo.ObservationPostComparisonResult{}, fmt.Errorf("list baselines: %w", err)
	}

	baselineInputs, err := repository.EnrichObservationPostComparisonInputs(
		ctx,
		trackingrepo.BuildObservationPostComparisonInputsFromBaselines(filterBaselinesByKind(baselines, kind)),
	)
	if err != nil {
		return trackingrepo.ObservationPostComparisonResult{}, fmt.Errorf("enrich baseline inputs: %w", err)
	}

	sentRows, err := listSentHistoryRowsWithinWindow(ctx, repository, kind, windowStart, windowEnd, windowEnd)
	if err != nil {
		return trackingrepo.ObservationPostComparisonResult{}, fmt.Errorf("list sent rows: %w", err)
	}

	sentInputs, err := repository.EnrichObservationPostComparisonInputs(
		ctx,
		trackingrepo.BuildObservationPostComparisonInputsFromSentHistories(kind, sentRows),
	)
	if err != nil {
		return trackingrepo.ObservationPostComparisonResult{}, fmt.Errorf("enrich sent inputs: %w", err)
	}

	return trackingrepo.CompareObservationPostInputs(baselineInputs, sentInputs), nil
}

func filterBaselinesByKind(
	rows []domain.YouTubeCommunityShortsObservationPostBaseline,
	kind domain.OutboxKind,
) []domain.YouTubeCommunityShortsObservationPostBaseline {
	filtered := make([]domain.YouTubeCommunityShortsObservationPostBaseline, 0, len(rows))
	for i := range rows {
		if rows[i].Kind != kind {
			continue
		}
		filtered = append(filtered, rows[i])
	}
	return filtered
}

func listSentHistoryRowsWithinWindow(
	ctx context.Context,
	repository *trackingrepo.GormRepository,
	kind domain.OutboxKind,
	windowStart time.Time,
	windowEnd time.Time,
	detectedBefore time.Time,
) ([]trackingrepo.ObservationAlarmSentHistoryRow, error) {
	switch kind {
	case domain.OutboxKindCommunityPost:
		return repository.ListCommunityAlarmSentHistoriesWithinObservationWindow(ctx, windowStart, windowEnd, detectedBefore)
	case domain.OutboxKindNewShort:
		return repository.ListShortsAlarmSentHistoriesWithinObservationWindow(ctx, windowStart, windowEnd, detectedBefore)
	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
}

func renderComparisonSummaryMarkdown(comparison trackingrepo.ObservationPostComparisonResult) string {
	summary := comparison.Summary
	return fmt.Sprintf(
		"- comparison: baseline_posts=`%d`, matched_posts=`%d`, unsent_posts=`%d`, duplicate_sent_posts=`%d`, unexpected_sent_posts=`%d`, identifier_mismatch_candidates=`%d`\n",
		summary.BaselineUniquePostCount,
		summary.MatchedPostCount,
		summary.UnsentPostCount,
		summary.DuplicateSentPostCount,
		summary.UnexpectedSentPostCount,
		summary.IdentifierMismatchCandidateCount,
	)
}

func renderComparisonVerdictsMarkdown(comparison trackingrepo.ObservationPostComparisonResult) string {
	if len(comparison.VerdictRows) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("\n## Comparison Verdicts\n\n")
	builder.WriteString("| verdict | reason | alarm_type | channel_id | canonical_post_id | baseline_count | sent_count | actual_published_at | alarm_sent_at | match_basis | review_status | related_baseline_post_ids | related_sent_post_ids |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | ---: | ---: | --- | --- | --- | --- | --- | --- |\n")
	for i := range comparison.VerdictRows {
		row := comparison.VerdictRows[i]
		builder.WriteString("| `")
		builder.WriteString(string(row.Verdict))
		builder.WriteString("` | `")
		builder.WriteString(string(row.Reason))
		builder.WriteString("` | `")
		builder.WriteString(string(row.AlarmType))
		builder.WriteString("` | `")
		builder.WriteString(renderMarkdownCell(row.ChannelID))
		builder.WriteString("` | `")
		builder.WriteString(renderMarkdownCell(renderVerdictCanonicalPostID(row)))
		builder.WriteString("` | ")
		fmt.Fprintf(&builder, "%d", row.BaselineCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.SentCount)
		builder.WriteString(" | `")
		builder.WriteString(shared.FormatSendCountTimePtr(renderVerdictPublishedAt(row)))
		builder.WriteString("` | `")
		builder.WriteString(shared.FormatSendCountTimePtr(row.AlarmSentAt))
		builder.WriteString("` | `")
		builder.WriteString(renderMarkdownCell(strings.Join(row.MatchBasis, ",")))
		builder.WriteString("` | `")
		builder.WriteString(string(row.ReviewStatus))
		builder.WriteString("` | `")
		builder.WriteString(renderMarkdownCell(strings.Join(row.RelatedBaselinePostIDs, ", ")))
		builder.WriteString("` | `")
		builder.WriteString(renderMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", ")))
		builder.WriteString("` |\n")
	}

	return builder.String()
}

func renderVerdictCanonicalPostID(row trackingrepo.ObservationPostComparisonVerdictRow) string {
	if canonicalPostID := strings.TrimSpace(row.CanonicalPostID); canonicalPostID != "" {
		return canonicalPostID
	}
	if len(row.RelatedBaselinePostIDs) > 0 {
		return row.RelatedBaselinePostIDs[0]
	}
	if len(row.RelatedSentPostIDs) > 0 {
		return row.RelatedSentPostIDs[0]
	}
	return ""
}

func renderVerdictPublishedAt(row trackingrepo.ObservationPostComparisonVerdictRow) *time.Time {
	if row.ActualPublishedAt != nil {
		return row.ActualPublishedAt
	}
	return row.MatchPublishedAt
}

func renderIdentifierMismatchCandidatesMarkdown(comparison trackingrepo.ObservationPostComparisonResult) string {
	if len(comparison.IdentifierMismatchCandidates) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("\n## Identifier Mismatch Candidates\n\n")
	builder.WriteString("| review_status | alarm_type | channel_id | match_published_at | match_basis | title_hint | baseline_post_ids | sent_post_ids |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for i := range comparison.IdentifierMismatchCandidates {
		candidate := comparison.IdentifierMismatchCandidates[i]
		builder.WriteString("| `")
		builder.WriteString(string(candidate.ReviewStatus))
		builder.WriteString("` | `")
		builder.WriteString(string(candidate.AlarmType))
		builder.WriteString("` | `")
		builder.WriteString(renderMarkdownCell(candidate.ChannelID))
		builder.WriteString("` | `")
		builder.WriteString(shared.FormatSendCountTimePtr(candidate.MatchPublishedAt))
		builder.WriteString("` | `")
		builder.WriteString(renderMarkdownCell(strings.Join(candidate.MatchBasis, ",")))
		builder.WriteString("` | `")
		builder.WriteString(renderMarkdownCell(candidate.MatchTitleHint))
		builder.WriteString("` | `")
		builder.WriteString(renderMarkdownCell(renderCandidatePostIDs(candidate.BaselineRows)))
		builder.WriteString("` | `")
		builder.WriteString(renderMarkdownCell(renderCandidatePostIDs(candidate.SentRows)))
		builder.WriteString("` |\n")
	}

	return builder.String()
}

func renderCandidatePostIDs(rows []trackingrepo.ObservationPostComparisonRow) string {
	ids := make([]string, 0, len(rows))
	for i := range rows {
		ids = append(ids, shared.FallbackSendCountValue(rows[i].CanonicalPostID))
	}
	return strings.Join(ids, ", ")
}
