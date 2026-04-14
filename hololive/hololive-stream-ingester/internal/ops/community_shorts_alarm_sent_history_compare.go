package ops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func buildObservationAlarmSentHistoryComparison(
	ctx context.Context,
	repository *trackingrepo.GormRepository,
	windowRuntimeName string,
	windowBigBangCutoverAt domainTime,
	windowStart domainTime,
	windowEnd domainTime,
	kind domain.OutboxKind,
) (trackingrepo.ObservationPostComparisonResult, error) {
	baselines, err := repository.ListCommunityShortsObservationPostBaselines(ctx, windowRuntimeName, windowBigBangCutoverAt)
	if err != nil {
		return trackingrepo.ObservationPostComparisonResult{}, fmt.Errorf("list baselines: %w", err)
	}

	baselineInputs, err := repository.EnrichObservationPostComparisonInputs(
		ctx,
		trackingrepo.BuildObservationPostComparisonInputsFromBaselines(filterObservationPostBaselinesByKind(baselines, kind)),
	)
	if err != nil {
		return trackingrepo.ObservationPostComparisonResult{}, fmt.Errorf("enrich baseline inputs: %w", err)
	}

	sentRows, err := listObservationAlarmSentHistoryRowsWithinWindow(ctx, repository, kind, windowStart, windowEnd, windowEnd)
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

type domainTime = time.Time

func filterObservationPostBaselinesByKind(
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

func listObservationAlarmSentHistoryRowsWithinWindow(
	ctx context.Context,
	repository *trackingrepo.GormRepository,
	kind domain.OutboxKind,
	windowStart domainTime,
	windowEnd domainTime,
	detectedBefore domainTime,
) ([]trackingrepo.ObservationAlarmSentHistoryRow, error) {
	switch kind {
	case domain.OutboxKindCommunityPost:
		rows, err := repository.ListCommunityAlarmSentHistoriesWithinObservationWindow(ctx, windowStart, windowEnd, detectedBefore)
		return rows, err
	case domain.OutboxKindNewShort:
		rows, err := repository.ListShortsAlarmSentHistoriesWithinObservationWindow(ctx, windowStart, windowEnd, detectedBefore)
		return rows, err
	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
}

func renderObservationPostComparisonSummaryMarkdown(comparison trackingrepo.ObservationPostComparisonResult) string {
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

func renderObservationPostComparisonVerdictsMarkdown(comparison trackingrepo.ObservationPostComparisonResult) string {
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
		builder.WriteString(renderObservationMarkdownCell(row.ChannelID))
		builder.WriteString("` | `")
		builder.WriteString(renderObservationMarkdownCell(renderObservationComparisonVerdictCanonicalPostID(row)))
		builder.WriteString("` | ")
		fmt.Fprintf(&builder, "%d", row.BaselineCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.SentCount)
		builder.WriteString(" | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(renderObservationComparisonVerdictPublishedAt(row)))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.AlarmSentAt))
		builder.WriteString("` | `")
		builder.WriteString(renderObservationMarkdownCell(strings.Join(row.MatchBasis, ",")))
		builder.WriteString("` | `")
		builder.WriteString(string(row.ReviewStatus))
		builder.WriteString("` | `")
		builder.WriteString(renderObservationMarkdownCell(strings.Join(row.RelatedBaselinePostIDs, ", ")))
		builder.WriteString("` | `")
		builder.WriteString(renderObservationMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", ")))
		builder.WriteString("` |\n")
	}

	return builder.String()
}

func renderObservationComparisonVerdictCanonicalPostID(row trackingrepo.ObservationPostComparisonVerdictRow) string {
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

func renderObservationComparisonVerdictPublishedAt(row trackingrepo.ObservationPostComparisonVerdictRow) *time.Time {
	if row.ActualPublishedAt != nil {
		return row.ActualPublishedAt
	}
	return row.MatchPublishedAt
}

func renderObservationIdentifierMismatchCandidatesMarkdown(comparison trackingrepo.ObservationPostComparisonResult) string {
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
		builder.WriteString(renderObservationMarkdownCell(candidate.ChannelID))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(candidate.MatchPublishedAt))
		builder.WriteString("` | `")
		builder.WriteString(renderObservationMarkdownCell(strings.Join(candidate.MatchBasis, ",")))
		builder.WriteString("` | `")
		builder.WriteString(renderObservationMarkdownCell(candidate.MatchTitleHint))
		builder.WriteString("` | `")
		builder.WriteString(renderObservationMarkdownCell(renderObservationCandidatePostIDs(candidate.BaselineRows)))
		builder.WriteString("` | `")
		builder.WriteString(renderObservationMarkdownCell(renderObservationCandidatePostIDs(candidate.SentRows)))
		builder.WriteString("` |\n")
	}

	return builder.String()
}

func renderObservationCandidatePostIDs(rows []trackingrepo.ObservationPostComparisonRow) string {
	ids := make([]string, 0, len(rows))
	for i := range rows {
		ids = append(ids, fallbackCommunityShortsSendCountValue(rows[i].CanonicalPostID))
	}
	return strings.Join(ids, ", ")
}

func renderObservationMarkdownCell(value string) string {
	replacer := strings.NewReplacer("|", "/", "\n", " ", "\r", " ")
	return replacer.Replace(strings.TrimSpace(value))
}
