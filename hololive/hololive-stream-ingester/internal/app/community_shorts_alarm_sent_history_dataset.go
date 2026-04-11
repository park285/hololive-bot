package app

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type CommunityShortsAlarmSentHistoryDatasetCollectOptions struct {
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time
}

type CommunityShortsAlarmSentHistoryDatasetQuery struct {
	ObservationRuntimeName      string     `json:"observation_runtime_name"`
	ObservationBigBangCutoverAt *time.Time `json:"observation_bigbang_cutover_at,omitempty"`
	WindowStart                 *time.Time `json:"window_start,omitempty"`
	WindowEnd                   *time.Time `json:"window_end,omitempty"`
}

type CommunityShortsAlarmSentHistoryDatasetSummary struct {
	CollectedRowCount                int        `json:"collected_row_count"`
	DuplicateRowCount                int        `json:"duplicate_row_count"`
	SentPostCount                    int        `json:"sent_post_count"`
	CommunitySentPostCount           int        `json:"community_sent_post_count"`
	ShortsSentPostCount              int        `json:"shorts_sent_post_count"`
	BaselinePostCount                int        `json:"baseline_post_count"`
	MatchedPostCount                 int        `json:"matched_post_count"`
	UnsentPostCount                  int        `json:"unsent_post_count"`
	DuplicateSentPostCount           int        `json:"duplicate_sent_post_count"`
	UnexpectedSentPostCount          int        `json:"unexpected_sent_post_count"`
	IdentifierMismatchCandidateCount int        `json:"identifier_mismatch_candidate_count"`
	VerificationRowCount             int        `json:"verification_row_count"`
	ReferenceRowCount                int        `json:"reference_row_count"`
	SendStatePostCount               int        `json:"send_state_post_count"`
	MissingAlarmPostCount            int        `json:"missing_alarm_post_count"`
	MissingSendStatePostCount        int        `json:"missing_send_state_post_count"`
	AttemptedMissingPostCount        int        `json:"attempted_missing_post_count"`
	NotSentMissingPostCount          int        `json:"not_sent_missing_post_count"`
	EarliestAlarmSentAt              *time.Time `json:"earliest_alarm_sent_at,omitempty"`
	LatestAlarmSentAt                *time.Time `json:"latest_alarm_sent_at,omitempty"`
}

type CommunityShortsAlarmSentHistoryDatasetAlarmTypeComparison struct {
	AlarmType                        domain.AlarmType `json:"alarm_type"`
	BaselinePostCount                int              `json:"baseline_post_count"`
	SentPostCount                    int              `json:"sent_post_count"`
	MatchedPostCount                 int              `json:"matched_post_count"`
	UnsentPostCount                  int              `json:"unsent_post_count"`
	DuplicateSentPostCount           int              `json:"duplicate_sent_post_count"`
	UnexpectedSentPostCount          int              `json:"unexpected_sent_post_count"`
	IdentifierMismatchCandidateCount int              `json:"identifier_mismatch_candidate_count"`
	MissingAlarmPostCount            int              `json:"missing_alarm_post_count"`
}

type CommunityShortsAlarmSentHistoryDatasetChannelComparison struct {
	ChannelID                        string `json:"channel_id"`
	BaselinePostCount                int    `json:"baseline_post_count"`
	SentPostCount                    int    `json:"sent_post_count"`
	MatchedPostCount                 int    `json:"matched_post_count"`
	UnsentPostCount                  int    `json:"unsent_post_count"`
	DuplicateSentPostCount           int    `json:"duplicate_sent_post_count"`
	UnexpectedSentPostCount          int    `json:"unexpected_sent_post_count"`
	IdentifierMismatchCandidateCount int    `json:"identifier_mismatch_candidate_count"`
	MissingAlarmPostCount            int    `json:"missing_alarm_post_count"`
}

type CommunityShortsAlarmSentHistoryDatasetResults struct {
	MissingAlarmEvaluated     bool                                                        `json:"missing_alarm_evaluated"`
	MissingAlarmPostCount     int                                                         `json:"missing_alarm_post_count"`
	MissingSendStatePostCount int                                                         `json:"missing_send_state_post_count"`
	AttemptedMissingPostCount int                                                         `json:"attempted_missing_post_count"`
	NotSentMissingPostCount   int                                                         `json:"not_sent_missing_post_count"`
	MissingAlarmZero          bool                                                        `json:"missing_alarm_zero"`
	AlarmTypeComparisons      []CommunityShortsAlarmSentHistoryDatasetAlarmTypeComparison `json:"alarm_type_comparisons,omitempty"`
	ChannelComparisons        []CommunityShortsAlarmSentHistoryDatasetChannelComparison   `json:"channel_comparisons,omitempty"`
}

type CommunityShortsAlarmSentHistoryDatasetRow struct {
	AlarmType         domain.AlarmType `json:"alarm_type"`
	PostKey           string           `json:"post_key,omitempty"`
	PostID            string           `json:"post_id"`
	ContentID         string           `json:"content_id"`
	ChannelID         string           `json:"channel_id"`
	ActualPublishedAt *time.Time       `json:"actual_published_at,omitempty"`
	DetectedAt        time.Time        `json:"detected_at"`
	AlarmSentAt       time.Time        `json:"alarm_sent_at"`
}

type CommunityShortsAlarmSentHistoryDatasetVerificationRow struct {
	Verdict                trackingrepo.ObservationPostComparisonVerdict                   `json:"verdict"`
	Reason                 trackingrepo.ObservationPostComparisonVerdictReason             `json:"reason"`
	AlarmType              domain.AlarmType                                                `json:"alarm_type"`
	ChannelID              string                                                          `json:"channel_id"`
	PostID                 string                                                          `json:"post_id,omitempty"`
	PostKey                string                                                          `json:"post_key,omitempty"`
	ContentID              string                                                          `json:"content_id,omitempty"`
	ActualPublishedAt      *time.Time                                                      `json:"actual_published_at,omitempty"`
	DetectedAt             *time.Time                                                      `json:"detected_at,omitempty"`
	AlarmSentAt            *time.Time                                                      `json:"alarm_sent_at,omitempty"`
	MatchPublishedAt       *time.Time                                                      `json:"match_published_at,omitempty"`
	MatchTitleHint         string                                                          `json:"match_title_hint,omitempty"`
	MatchBasis             []string                                                        `json:"match_basis,omitempty"`
	ReviewStatus           trackingrepo.ObservationIdentifierMismatchCandidateReviewStatus `json:"review_status,omitempty"`
	BaselineCount          int                                                             `json:"baseline_count"`
	SentCount              int                                                             `json:"sent_count"`
	RelatedBaselinePostIDs []string                                                        `json:"related_baseline_post_ids,omitempty"`
	RelatedSentPostIDs     []string                                                        `json:"related_sent_post_ids,omitempty"`
}

type CommunityShortsAlarmSentHistoryDatasetReferenceRow struct {
	AlarmType           domain.AlarmType                                                `json:"alarm_type"`
	ChannelID           string                                                          `json:"channel_id"`
	ChannelPostKey      string                                                          `json:"channel_post_key"`
	PostID              string                                                          `json:"post_id"`
	ActualPublishedAt   *time.Time                                                      `json:"actual_published_at,omitempty"`
	DetectedAt          *time.Time                                                      `json:"detected_at,omitempty"`
	VerificationVerdict trackingrepo.ObservationPostComparisonVerdict                   `json:"verification_verdict"`
	VerificationReason  trackingrepo.ObservationPostComparisonVerdictReason             `json:"verification_reason"`
	SentCount           int                                                             `json:"sent_count"`
	ReviewStatus        trackingrepo.ObservationIdentifierMismatchCandidateReviewStatus `json:"review_status,omitempty"`
	RelatedSentPostIDs  []string                                                        `json:"related_sent_post_ids,omitempty"`
}

type CommunityShortsMissingAlarmReason string

const (
	CommunityShortsMissingAlarmReasonSendStateMissing CommunityShortsMissingAlarmReason = "send_state_missing"
	CommunityShortsMissingAlarmReasonAttempted        CommunityShortsMissingAlarmReason = "attempted_without_success"
	CommunityShortsMissingAlarmReasonNotSent          CommunityShortsMissingAlarmReason = "not_sent"
)

type CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow struct {
	MissingReason       CommunityShortsMissingAlarmReason                   `json:"missing_reason"`
	SendState           CommunityShortsPerPostSendState                     `json:"send_state,omitempty"`
	AlarmType           domain.AlarmType                                    `json:"alarm_type"`
	ChannelID           string                                              `json:"channel_id"`
	ChannelPostKey      string                                              `json:"channel_post_key"`
	PostKey             string                                              `json:"post_key,omitempty"`
	PostID              string                                              `json:"post_id"`
	ActualPublishedAt   *time.Time                                          `json:"actual_published_at,omitempty"`
	DetectedAt          *time.Time                                          `json:"detected_at,omitempty"`
	StateContentID      string                                              `json:"state_content_id,omitempty"`
	StateDetectedAt     *time.Time                                          `json:"state_detected_at,omitempty"`
	StateAlarmSentAt    *time.Time                                          `json:"state_alarm_sent_at,omitempty"`
	VerificationVerdict trackingrepo.ObservationPostComparisonVerdict       `json:"verification_verdict"`
	VerificationReason  trackingrepo.ObservationPostComparisonVerdictReason `json:"verification_reason"`
	RelatedSentPostIDs  []string                                            `json:"related_sent_post_ids,omitempty"`
}

type CommunityShortsAlarmSentHistoryDatasetReport struct {
	GeneratedAt      time.Time                                               `json:"generated_at"`
	Query            CommunityShortsAlarmSentHistoryDatasetQuery             `json:"query"`
	Summary          CommunityShortsAlarmSentHistoryDatasetSummary           `json:"summary"`
	Results          CommunityShortsAlarmSentHistoryDatasetResults           `json:"results"`
	Comparison       trackingrepo.ObservationPostComparisonResult            `json:"comparison"`
	Rows             []CommunityShortsAlarmSentHistoryDatasetRow             `json:"rows"`
	VerificationRows []CommunityShortsAlarmSentHistoryDatasetVerificationRow `json:"verification_rows"`
	ReferenceRows    []CommunityShortsAlarmSentHistoryDatasetReferenceRow    `json:"reference_rows"`
	MissingAlarmRows []CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow `json:"missing_alarm_rows"`
}

func CollectCommunityShortsAlarmSentHistoryDatasetReport(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CommunityShortsAlarmSentHistoryDatasetCollectOptions,
) (CommunityShortsAlarmSentHistoryDatasetReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return CommunityShortsAlarmSentHistoryDatasetReport{}, fmt.Errorf("collect community shorts alarm sent history dataset: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = normalizeCommunityShortsSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}

	query, err := normalizeCommunityShortsAlarmSentHistoryDatasetCollectOptions(options)
	if err != nil {
		return CommunityShortsAlarmSentHistoryDatasetReport{}, fmt.Errorf("collect community shorts alarm sent history dataset: %w", err)
	}

	databaseResources, cleanupDB, err := sharedproviders.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
	if err != nil {
		return CommunityShortsAlarmSentHistoryDatasetReport{}, fmt.Errorf("collect community shorts alarm sent history dataset: provide database resources: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	db := databaseResources.Service.GetGormDB()
	trackingRepository := trackingrepo.NewRepository(db)
	telemetryRepo := outbox.NewDeliveryTelemetryRepository(db)
	window, err := trackingRepository.FindClosedCommunityShortsObservationWindow(
		ctx,
		query.ObservationRuntimeName,
		*query.ObservationBigBangCutoverAt,
		now,
	)
	if err != nil {
		return CommunityShortsAlarmSentHistoryDatasetReport{}, fmt.Errorf("collect community shorts alarm sent history dataset: find observation window: %w", err)
	}
	if window == nil {
		return CommunityShortsAlarmSentHistoryDatasetReport{}, fmt.Errorf(
			"collect community shorts alarm sent history dataset: observation window not found: runtime=%s cutover=%s",
			query.ObservationRuntimeName,
			formatCommunityShortsSendCountTime(*query.ObservationBigBangCutoverAt),
		)
	}
	query.WindowStart = cloneCommunityShortsSendCountTime(&window.ObservationStartedAt)
	query.WindowEnd = cloneCommunityShortsSendCountTime(&window.ObservationEndedAt)

	baselines, err := trackingRepository.ListCommunityShortsObservationPostBaselines(
		ctx,
		query.ObservationRuntimeName,
		window.BigBangCutoverAt,
	)
	if err != nil {
		return CommunityShortsAlarmSentHistoryDatasetReport{}, fmt.Errorf("collect community shorts alarm sent history dataset: list observation baselines: %w", err)
	}

	communityRows, err := trackingRepository.ListCommunityAlarmSentHistoriesWithinObservationWindow(
		ctx,
		window.ObservationStartedAt,
		window.ObservationEndedAt,
		window.ObservationEndedAt,
	)
	if err != nil {
		return CommunityShortsAlarmSentHistoryDatasetReport{}, fmt.Errorf("collect community shorts alarm sent history dataset: list community sent histories: %w", err)
	}

	shortsRows, err := trackingRepository.ListShortsAlarmSentHistoriesWithinObservationWindow(
		ctx,
		window.ObservationStartedAt,
		window.ObservationEndedAt,
		window.ObservationEndedAt,
	)
	if err != nil {
		return CommunityShortsAlarmSentHistoryDatasetReport{}, fmt.Errorf("collect community shorts alarm sent history dataset: list shorts sent histories: %w", err)
	}

	sendStateRows, err := telemetryRepo.ListPostSendCountsByFinalizedObservationWindow(
		ctx,
		query.ObservationRuntimeName,
		window.BigBangCutoverAt,
	)
	if err != nil {
		return CommunityShortsAlarmSentHistoryDatasetReport{}, fmt.Errorf("collect community shorts alarm sent history dataset: list finalized send states: %w", err)
	}

	comparison, err := buildCommunityShortsAlarmSentHistoryDatasetComparison(
		ctx,
		trackingRepository,
		baselines,
		communityRows,
		shortsRows,
	)
	if err != nil {
		return CommunityShortsAlarmSentHistoryDatasetReport{}, fmt.Errorf("collect community shorts alarm sent history dataset: build comparison: %w", err)
	}

	report := BuildCommunityShortsAlarmSentHistoryDatasetReport(communityRows, shortsRows, comparison, query, now)
	return attachCommunityShortsAlarmSentHistoryDatasetMissingAlarmRows(report, sendStateRows), nil
}

func BuildCommunityShortsAlarmSentHistoryDatasetReport(
	communityRows []trackingrepo.CommunityAlarmSentHistoryRow,
	shortsRows []trackingrepo.ShortsAlarmSentHistoryRow,
	comparison trackingrepo.ObservationPostComparisonResult,
	query CommunityShortsAlarmSentHistoryDatasetQuery,
	generatedAt time.Time,
) CommunityShortsAlarmSentHistoryDatasetReport {
	generatedAt = normalizeCommunityShortsSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	query = normalizeCommunityShortsAlarmSentHistoryDatasetQuery(query)

	finalizedCommunity := finalizeCommunityAlarmSentHistoryRows(communityRows)
	finalizedShorts := finalizeShortsAlarmSentHistoryRows(shortsRows)
	rows := make([]CommunityShortsAlarmSentHistoryDatasetRow, 0, len(finalizedCommunity.Rows)+len(finalizedShorts.Rows))

	appendRows := func(alarmType domain.AlarmType, inputs []trackingrepo.ObservationAlarmSentHistoryRow) {
		for i := range inputs {
			row := inputs[i]
			postID := strings.TrimSpace(row.PostID)
			channelID := strings.TrimSpace(row.ChannelID)
			rows = append(rows, CommunityShortsAlarmSentHistoryDatasetRow{
				AlarmType:         alarmType,
				PostKey:           buildCommunityShortsObservationPostKey(alarmType, channelID, postID),
				PostID:            postID,
				ContentID:         strings.TrimSpace(row.ContentID),
				ChannelID:         channelID,
				ActualPublishedAt: cloneCommunityShortsSendCountTime(row.ActualPublishedAt),
				DetectedAt:        normalizeCommunityShortsSendCountTime(row.DetectedAt),
				AlarmSentAt:       normalizeCommunityShortsSendCountTime(row.AlarmSentAt),
			})
		}
	}
	appendRows(domain.AlarmTypeCommunity, finalizedCommunity.Rows)
	appendRows(domain.AlarmTypeShorts, finalizedShorts.Rows)

	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].AlarmSentAt.Equal(rows[j].AlarmSentAt) {
			return rows[i].AlarmSentAt.Before(rows[j].AlarmSentAt)
		}
		if rows[i].ChannelID != rows[j].ChannelID {
			return rows[i].ChannelID < rows[j].ChannelID
		}
		if rows[i].AlarmType != rows[j].AlarmType {
			return rows[i].AlarmType < rows[j].AlarmType
		}
		if rows[i].PostID != rows[j].PostID {
			return rows[i].PostID < rows[j].PostID
		}
		return rows[i].ContentID < rows[j].ContentID
	})

	verificationRows := buildCommunityShortsAlarmSentHistoryDatasetVerificationRows(comparison.VerdictRows)
	referenceRows := buildCommunityShortsAlarmSentHistoryDatasetReferenceRows(comparison.VerdictRows)
	summary := CommunityShortsAlarmSentHistoryDatasetSummary{
		CollectedRowCount:                finalizedCommunity.CollectedRowCount + finalizedShorts.CollectedRowCount,
		DuplicateRowCount:                finalizedCommunity.DuplicateRowCount + finalizedShorts.DuplicateRowCount,
		SentPostCount:                    len(rows),
		CommunitySentPostCount:           len(finalizedCommunity.Rows),
		ShortsSentPostCount:              len(finalizedShorts.Rows),
		BaselinePostCount:                comparison.Summary.BaselineUniquePostCount,
		MatchedPostCount:                 comparison.Summary.MatchedPostCount,
		UnsentPostCount:                  comparison.Summary.UnsentPostCount,
		DuplicateSentPostCount:           comparison.Summary.DuplicateSentPostCount,
		UnexpectedSentPostCount:          comparison.Summary.UnexpectedSentPostCount,
		IdentifierMismatchCandidateCount: comparison.Summary.IdentifierMismatchCandidateCount,
		VerificationRowCount:             len(verificationRows),
		ReferenceRowCount:                len(referenceRows),
	}
	for i := range rows {
		row := rows[i]
		if summary.EarliestAlarmSentAt == nil || row.AlarmSentAt.Before(summary.EarliestAlarmSentAt.UTC()) {
			summary.EarliestAlarmSentAt = cloneCommunityShortsSendCountTime(&row.AlarmSentAt)
		}
		if summary.LatestAlarmSentAt == nil || row.AlarmSentAt.After(summary.LatestAlarmSentAt.UTC()) {
			summary.LatestAlarmSentAt = cloneCommunityShortsSendCountTime(&row.AlarmSentAt)
		}
	}

	return CommunityShortsAlarmSentHistoryDatasetReport{
		GeneratedAt:      generatedAt,
		Query:            query,
		Summary:          summary,
		Results:          buildCommunityShortsAlarmSentHistoryDatasetResults(rows, verificationRows, referenceRows, nil, summary, false),
		Comparison:       comparison,
		Rows:             rows,
		VerificationRows: verificationRows,
		ReferenceRows:    referenceRows,
	}
}

func RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(report CommunityShortsAlarmSentHistoryDatasetReport) string {
	var builder strings.Builder

	builder.WriteString("# YouTube Community/Shorts Alarm Sent History Dataset\n\n")
	builder.WriteString("- generated at: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.GeneratedAt))
	builder.WriteString("`\n")
	builder.WriteString("- observation runtime: `")
	builder.WriteString(fallbackCommunityShortsSendCountValue(report.Query.ObservationRuntimeName))
	builder.WriteString("`, cutover: `")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.ObservationBigBangCutoverAt))
	builder.WriteString("`\n")
	builder.WriteString("- window: `")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.WindowStart))
	builder.WriteString("` -> `")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.WindowEnd))
	builder.WriteString("`\n")
	builder.WriteString("- summary: collected_rows=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.CollectedRowCount))
	builder.WriteString("`, duplicates_removed=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.DuplicateRowCount))
	builder.WriteString("`, sent_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.SentPostCount))
	builder.WriteString("`, community_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.CommunitySentPostCount))
	builder.WriteString("`, shorts_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.ShortsSentPostCount))
	builder.WriteString("`, baseline_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.BaselinePostCount))
	builder.WriteString("`, matched_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.MatchedPostCount))
	builder.WriteString("`, unsent_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.UnsentPostCount))
	builder.WriteString("`, duplicate_sent_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.DuplicateSentPostCount))
	builder.WriteString("`, unexpected_sent_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.UnexpectedSentPostCount))
	builder.WriteString("`, identifier_mismatch_candidates=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.IdentifierMismatchCandidateCount))
	builder.WriteString("`, verification_rows=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.VerificationRowCount))
	builder.WriteString("`, reference_rows=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.ReferenceRowCount))
	builder.WriteString("`, send_state_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.SendStatePostCount))
	builder.WriteString("`, missing_alarm_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.MissingAlarmPostCount))
	builder.WriteString("`, missing_send_state_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.MissingSendStatePostCount))
	builder.WriteString("`, attempted_missing_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.AttemptedMissingPostCount))
	builder.WriteString("`, not_sent_missing_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.NotSentMissingPostCount))
	builder.WriteString("`, earliest_alarm_sent_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.EarliestAlarmSentAt))
	builder.WriteString("`, latest_alarm_sent_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.LatestAlarmSentAt))
	builder.WriteString("`\n")

	builder.WriteString("\n## Results\n\n")
	if report.Results.MissingAlarmEvaluated {
		builder.WriteString("- missing alarm aggregation: missing_alarm_posts=`")
		builder.WriteString(fmt.Sprintf("%d", report.Results.MissingAlarmPostCount))
		builder.WriteString("`, missing_send_state_posts=`")
		builder.WriteString(fmt.Sprintf("%d", report.Results.MissingSendStatePostCount))
		builder.WriteString("`, attempted_missing_posts=`")
		builder.WriteString(fmt.Sprintf("%d", report.Results.AttemptedMissingPostCount))
		builder.WriteString("`, not_sent_missing_posts=`")
		builder.WriteString(fmt.Sprintf("%d", report.Results.NotSentMissingPostCount))
		builder.WriteString("`\n")
		if report.Results.MissingAlarmZero {
			builder.WriteString("- omission closeout: 누락 0건입니다.\n")
		} else {
			builder.WriteString("- omission closeout: 누락 알람이 `")
			builder.WriteString(fmt.Sprintf("%d", report.Results.MissingAlarmPostCount))
			builder.WriteString("`건 남아 있습니다.\n")
		}
	} else {
		builder.WriteString("- missing alarm aggregation: finalized send-state comparison pending\n")
	}

	if len(report.Results.AlarmTypeComparisons) == 0 {
		builder.WriteString("\n게시물 유형별 대조 결과가 없습니다.\n")
	} else {
		builder.WriteString("\n### By Alarm Type\n\n")
		builder.WriteString("| alarm_type | baseline_posts | sent_posts | matched_posts | unsent_posts | duplicate_sent_posts | unexpected_sent_posts | identifier_mismatch_candidates | missing_alarm_posts |\n")
		builder.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for i := range report.Results.AlarmTypeComparisons {
			row := report.Results.AlarmTypeComparisons[i]
			builder.WriteString("| `")
			builder.WriteString(string(row.AlarmType))
			builder.WriteString("` | ")
			builder.WriteString(fmt.Sprintf("%d", row.BaselinePostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.SentPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.MatchedPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.UnsentPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.DuplicateSentPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.UnexpectedSentPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.IdentifierMismatchCandidateCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.MissingAlarmPostCount))
			builder.WriteString(" |\n")
		}
	}

	if len(report.Results.ChannelComparisons) == 0 {
		builder.WriteString("\n채널별 대조 결과가 없습니다.\n")
	} else {
		builder.WriteString("\n### By Channel\n\n")
		builder.WriteString("| channel_id | baseline_posts | sent_posts | matched_posts | unsent_posts | duplicate_sent_posts | unexpected_sent_posts | identifier_mismatch_candidates | missing_alarm_posts |\n")
		builder.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for i := range report.Results.ChannelComparisons {
			row := report.Results.ChannelComparisons[i]
			builder.WriteString("| `")
			builder.WriteString(renderObservationMarkdownCell(row.ChannelID))
			builder.WriteString("` | ")
			builder.WriteString(fmt.Sprintf("%d", row.BaselinePostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.SentPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.MatchedPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.UnsentPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.DuplicateSentPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.UnexpectedSentPostCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.IdentifierMismatchCandidateCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.MissingAlarmPostCount))
			builder.WriteString(" |\n")
		}
	}

	if len(report.MissingAlarmRows) == 0 {
		builder.WriteString("\n누락 알람 게시물이 없습니다.\n")
	} else {
		builder.WriteString("\n## Missing Alarm Rows\n\n")
		builder.WriteString("| missing_reason | send_state | alarm_type | channel_id | channel_post_key | post_key | post_id | actual_published_at | detected_at | state_detected_at | state_alarm_sent_at | verification_verdict | verification_reason | related_sent_post_ids |\n")
		builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
		for i := range report.MissingAlarmRows {
			row := report.MissingAlarmRows[i]
			builder.WriteString("| `")
			builder.WriteString(string(row.MissingReason))
			builder.WriteString("` | `")
			builder.WriteString(string(row.SendState))
			builder.WriteString("` | `")
			builder.WriteString(string(row.AlarmType))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.ChannelID))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.ChannelPostKey))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.PostKey))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.PostID))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.DetectedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.StateDetectedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.StateAlarmSentAt))
			builder.WriteString("` | `")
			builder.WriteString(string(row.VerificationVerdict))
			builder.WriteString("` | `")
			builder.WriteString(string(row.VerificationReason))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", ")))
			builder.WriteString("` |\n")
		}
	}

	if len(report.VerificationRows) == 0 {
		builder.WriteString("\n검증 verdict row가 없습니다.\n")
	} else {
		builder.WriteString("\n## Verification Rows\n\n")
		builder.WriteString("| verdict | reason | alarm_type | channel_id | post_key | post_id | content_id | baseline_count | sent_count | actual_published_at | detected_at | alarm_sent_at | match_published_at | match_title_hint | match_basis | review_status | related_baseline_post_ids | related_sent_post_ids |\n")
		builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | ---: | ---: | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
		for i := range report.VerificationRows {
			row := report.VerificationRows[i]
			builder.WriteString("| `")
			builder.WriteString(string(row.Verdict))
			builder.WriteString("` | `")
			builder.WriteString(string(row.Reason))
			builder.WriteString("` | `")
			builder.WriteString(string(row.AlarmType))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.ChannelID))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.PostKey))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.PostID))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.ContentID))
			builder.WriteString("` | ")
			builder.WriteString(fmt.Sprintf("%d", row.BaselineCount))
			builder.WriteString(" | ")
			builder.WriteString(fmt.Sprintf("%d", row.SentCount))
			builder.WriteString(" | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.DetectedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.AlarmSentAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.MatchPublishedAt))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.MatchTitleHint))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(strings.Join(row.MatchBasis, ", ")))
			builder.WriteString("` | `")
			builder.WriteString(string(row.ReviewStatus))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(strings.Join(row.RelatedBaselinePostIDs, ", ")))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", ")))
			builder.WriteString("` |\n")
		}
	}

	if len(report.ReferenceRows) == 0 {
		builder.WriteString("\n정규화된 검증 기준 row가 없습니다.\n")
	} else {
		builder.WriteString("\n## Normalized Verification Reference Rows\n\n")
		builder.WriteString("| alarm_type | channel_id | channel_post_key | post_id | actual_published_at | detected_at | verification_verdict | verification_reason | sent_count | review_status | related_sent_post_ids |\n")
		builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | ---: | --- | --- |\n")
		for i := range report.ReferenceRows {
			row := report.ReferenceRows[i]
			builder.WriteString("| `")
			builder.WriteString(string(row.AlarmType))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.ChannelID))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.ChannelPostKey))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(row.PostID))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.DetectedAt))
			builder.WriteString("` | `")
			builder.WriteString(string(row.VerificationVerdict))
			builder.WriteString("` | `")
			builder.WriteString(string(row.VerificationReason))
			builder.WriteString("` | ")
			builder.WriteString(fmt.Sprintf("%d", row.SentCount))
			builder.WriteString(" | `")
			builder.WriteString(string(row.ReviewStatus))
			builder.WriteString("` | `")
			builder.WriteString(renderObservationMarkdownCell(strings.Join(row.RelatedSentPostIDs, ", ")))
			builder.WriteString("` |\n")
		}
	}

	if len(report.Rows) == 0 {
		builder.WriteString("\n정규화된 community/shorts sent history row가 없습니다.\n")
		return builder.String()
	}

	builder.WriteString("\n## Normalized Sent History Rows\n\n")
	builder.WriteString("| alarm_type | channel_id | post_key | post_id | content_id | actual_published_at | detected_at | alarm_sent_at |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for i := range report.Rows {
		row := report.Rows[i]
		builder.WriteString("| `")
		builder.WriteString(string(row.AlarmType))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.ChannelID))
		builder.WriteString("` | `")
		builder.WriteString(renderObservationMarkdownCell(row.PostKey))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.PostID))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.ContentID))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTime(row.DetectedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTime(row.AlarmSentAt))
		builder.WriteString("` |\n")
	}

	return builder.String()
}

func normalizeCommunityShortsAlarmSentHistoryDatasetCollectOptions(
	options CommunityShortsAlarmSentHistoryDatasetCollectOptions,
) (CommunityShortsAlarmSentHistoryDatasetQuery, error) {
	runtimeName := strings.TrimSpace(options.ObservationRuntimeName)
	cutoverAt := cloneCommunityShortsSendCountTime(options.ObservationBigBangCutoverAt)
	if runtimeName == "" || cutoverAt == nil || cutoverAt.IsZero() {
		return CommunityShortsAlarmSentHistoryDatasetQuery{}, fmt.Errorf("observation runtime name and big-bang cutover at are required")
	}

	return CommunityShortsAlarmSentHistoryDatasetQuery{
		ObservationRuntimeName:      runtimeName,
		ObservationBigBangCutoverAt: cutoverAt,
	}, nil
}

func normalizeCommunityShortsAlarmSentHistoryDatasetQuery(
	query CommunityShortsAlarmSentHistoryDatasetQuery,
) CommunityShortsAlarmSentHistoryDatasetQuery {
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = cloneCommunityShortsSendCountTime(query.ObservationBigBangCutoverAt)
	query.WindowStart = cloneCommunityShortsSendCountTime(query.WindowStart)
	query.WindowEnd = cloneCommunityShortsSendCountTime(query.WindowEnd)
	return query
}

func buildCommunityShortsAlarmSentHistoryDatasetComparison(
	ctx context.Context,
	repository *trackingrepo.GormRepository,
	baselines []domain.YouTubeCommunityShortsObservationPostBaseline,
	communityRows []trackingrepo.CommunityAlarmSentHistoryRow,
	shortsRows []trackingrepo.ShortsAlarmSentHistoryRow,
) (trackingrepo.ObservationPostComparisonResult, error) {
	baselineInputs, err := repository.EnrichObservationPostComparisonInputs(
		ctx,
		trackingrepo.BuildObservationPostComparisonInputsFromBaselines(baselines),
	)
	if err != nil {
		return trackingrepo.ObservationPostComparisonResult{}, fmt.Errorf("enrich baseline inputs: %w", err)
	}

	communityInputs := trackingrepo.BuildObservationPostComparisonInputsFromSentHistories(domain.OutboxKindCommunityPost, communityRows)
	shortsInputs := trackingrepo.BuildObservationPostComparisonInputsFromSentHistories(domain.OutboxKindNewShort, shortsRows)
	sentInputs := make([]trackingrepo.ObservationPostComparisonInput, 0, len(communityInputs)+len(shortsInputs))
	sentInputs = append(sentInputs, communityInputs...)
	sentInputs = append(sentInputs, shortsInputs...)
	sentInputs, err = repository.EnrichObservationPostComparisonInputs(ctx, sentInputs)
	if err != nil {
		return trackingrepo.ObservationPostComparisonResult{}, fmt.Errorf("enrich sent inputs: %w", err)
	}

	return trackingrepo.CompareObservationPostInputs(baselineInputs, sentInputs), nil
}

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

type communityShortsAlarmSentHistoryDatasetComparisonAccumulator struct {
	BaselinePostCount                int
	SentPostCount                    int
	MatchedPostCount                 int
	UnsentPostCount                  int
	DuplicateSentPostCount           int
	UnexpectedSentPostCount          int
	IdentifierMismatchCandidateCount int
	MissingAlarmPostCount            int
}

func buildCommunityShortsAlarmSentHistoryDatasetResults(
	rows []CommunityShortsAlarmSentHistoryDatasetRow,
	verificationRows []CommunityShortsAlarmSentHistoryDatasetVerificationRow,
	referenceRows []CommunityShortsAlarmSentHistoryDatasetReferenceRow,
	missingAlarmRows []CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow,
	summary CommunityShortsAlarmSentHistoryDatasetSummary,
	missingAlarmEvaluated bool,
) CommunityShortsAlarmSentHistoryDatasetResults {
	alarmTypeAccumulators := make(map[domain.AlarmType]*communityShortsAlarmSentHistoryDatasetComparisonAccumulator)
	channelAccumulators := make(map[string]*communityShortsAlarmSentHistoryDatasetComparisonAccumulator)

	ensureAlarmTypeAccumulator := func(alarmType domain.AlarmType) *communityShortsAlarmSentHistoryDatasetComparisonAccumulator {
		if alarmType == "" {
			return nil
		}
		if existing, ok := alarmTypeAccumulators[alarmType]; ok {
			return existing
		}
		accumulator := &communityShortsAlarmSentHistoryDatasetComparisonAccumulator{}
		alarmTypeAccumulators[alarmType] = accumulator
		return accumulator
	}
	ensureChannelAccumulator := func(channelID string) *communityShortsAlarmSentHistoryDatasetComparisonAccumulator {
		trimmed := strings.TrimSpace(channelID)
		if trimmed == "" {
			return nil
		}
		if existing, ok := channelAccumulators[trimmed]; ok {
			return existing
		}
		accumulator := &communityShortsAlarmSentHistoryDatasetComparisonAccumulator{}
		channelAccumulators[trimmed] = accumulator
		return accumulator
	}

	for i := range rows {
		row := rows[i]
		if accumulator := ensureAlarmTypeAccumulator(row.AlarmType); accumulator != nil {
			accumulator.SentPostCount++
		}
		if accumulator := ensureChannelAccumulator(row.ChannelID); accumulator != nil {
			accumulator.SentPostCount++
		}
	}

	for i := range referenceRows {
		row := referenceRows[i]
		if accumulator := ensureAlarmTypeAccumulator(row.AlarmType); accumulator != nil {
			accumulator.BaselinePostCount++
			applyCommunityShortsAlarmSentHistoryDatasetVerdict(accumulator, row.VerificationVerdict)
		}
		if accumulator := ensureChannelAccumulator(row.ChannelID); accumulator != nil {
			accumulator.BaselinePostCount++
			applyCommunityShortsAlarmSentHistoryDatasetVerdict(accumulator, row.VerificationVerdict)
		}
	}

	for i := range verificationRows {
		row := verificationRows[i]
		if row.Verdict != trackingrepo.ObservationPostComparisonVerdictUnexpectedSent {
			continue
		}
		if accumulator := ensureAlarmTypeAccumulator(row.AlarmType); accumulator != nil {
			accumulator.UnexpectedSentPostCount++
		}
		if accumulator := ensureChannelAccumulator(row.ChannelID); accumulator != nil {
			accumulator.UnexpectedSentPostCount++
		}
	}

	for i := range missingAlarmRows {
		row := missingAlarmRows[i]
		if accumulator := ensureAlarmTypeAccumulator(row.AlarmType); accumulator != nil {
			accumulator.MissingAlarmPostCount++
		}
		if accumulator := ensureChannelAccumulator(row.ChannelID); accumulator != nil {
			accumulator.MissingAlarmPostCount++
		}
	}

	results := CommunityShortsAlarmSentHistoryDatasetResults{
		MissingAlarmEvaluated:     missingAlarmEvaluated,
		MissingAlarmPostCount:     summary.MissingAlarmPostCount,
		MissingSendStatePostCount: summary.MissingSendStatePostCount,
		AttemptedMissingPostCount: summary.AttemptedMissingPostCount,
		NotSentMissingPostCount:   summary.NotSentMissingPostCount,
		MissingAlarmZero:          missingAlarmEvaluated && summary.MissingAlarmPostCount == 0,
	}

	if len(alarmTypeAccumulators) > 0 {
		keys := make([]domain.AlarmType, 0, len(alarmTypeAccumulators))
		for alarmType := range alarmTypeAccumulators {
			keys = append(keys, alarmType)
		}
		sort.SliceStable(keys, func(i, j int) bool {
			return keys[i] < keys[j]
		})
		results.AlarmTypeComparisons = make([]CommunityShortsAlarmSentHistoryDatasetAlarmTypeComparison, 0, len(keys))
		for i := range keys {
			alarmType := keys[i]
			accumulator := alarmTypeAccumulators[alarmType]
			results.AlarmTypeComparisons = append(results.AlarmTypeComparisons, CommunityShortsAlarmSentHistoryDatasetAlarmTypeComparison{
				AlarmType:                        alarmType,
				BaselinePostCount:                accumulator.BaselinePostCount,
				SentPostCount:                    accumulator.SentPostCount,
				MatchedPostCount:                 accumulator.MatchedPostCount,
				UnsentPostCount:                  accumulator.UnsentPostCount,
				DuplicateSentPostCount:           accumulator.DuplicateSentPostCount,
				UnexpectedSentPostCount:          accumulator.UnexpectedSentPostCount,
				IdentifierMismatchCandidateCount: accumulator.IdentifierMismatchCandidateCount,
				MissingAlarmPostCount:            accumulator.MissingAlarmPostCount,
			})
		}
	}

	if len(channelAccumulators) > 0 {
		keys := make([]string, 0, len(channelAccumulators))
		for channelID := range channelAccumulators {
			keys = append(keys, channelID)
		}
		sort.Strings(keys)
		results.ChannelComparisons = make([]CommunityShortsAlarmSentHistoryDatasetChannelComparison, 0, len(keys))
		for i := range keys {
			channelID := keys[i]
			accumulator := channelAccumulators[channelID]
			results.ChannelComparisons = append(results.ChannelComparisons, CommunityShortsAlarmSentHistoryDatasetChannelComparison{
				ChannelID:                        channelID,
				BaselinePostCount:                accumulator.BaselinePostCount,
				SentPostCount:                    accumulator.SentPostCount,
				MatchedPostCount:                 accumulator.MatchedPostCount,
				UnsentPostCount:                  accumulator.UnsentPostCount,
				DuplicateSentPostCount:           accumulator.DuplicateSentPostCount,
				UnexpectedSentPostCount:          accumulator.UnexpectedSentPostCount,
				IdentifierMismatchCandidateCount: accumulator.IdentifierMismatchCandidateCount,
				MissingAlarmPostCount:            accumulator.MissingAlarmPostCount,
			})
		}
	}

	return results
}

func applyCommunityShortsAlarmSentHistoryDatasetVerdict(
	accumulator *communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
	verdict trackingrepo.ObservationPostComparisonVerdict,
) {
	if accumulator == nil {
		return
	}
	switch verdict {
	case trackingrepo.ObservationPostComparisonVerdictMatched:
		accumulator.MatchedPostCount++
	case trackingrepo.ObservationPostComparisonVerdictUnsent:
		accumulator.UnsentPostCount++
	case trackingrepo.ObservationPostComparisonVerdictDuplicateSent:
		accumulator.DuplicateSentPostCount++
	case trackingrepo.ObservationPostComparisonVerdictIdentifierMismatchCandidate:
		accumulator.IdentifierMismatchCandidateCount++
	}
}

type communityShortsAlarmSentHistoryDatasetMissingAlarmSummary struct {
	SendStatePostCount        int
	MissingAlarmPostCount     int
	MissingSendStatePostCount int
	AttemptedMissingPostCount int
	NotSentMissingPostCount   int
}

func attachCommunityShortsAlarmSentHistoryDatasetMissingAlarmRows(
	report CommunityShortsAlarmSentHistoryDatasetReport,
	sendStateRows []outbox.PostSendCount,
) CommunityShortsAlarmSentHistoryDatasetReport {
	sendStateReport := BuildCommunityShortsSendStateReport(
		sendStateRows,
		CommunityShortsSendStateQuery{
			ObservationRuntimeName:      report.Query.ObservationRuntimeName,
			ObservationBigBangCutoverAt: report.Query.ObservationBigBangCutoverAt,
			WindowStart:                 report.Query.WindowStart,
			WindowEnd:                   report.Query.WindowEnd,
			Finalized:                   true,
		},
		report.GeneratedAt,
	)
	missingRows, missingSummary := buildCommunityShortsAlarmSentHistoryDatasetMissingAlarmRows(
		report.ReferenceRows,
		sendStateReport.Rows,
	)
	report.Summary.SendStatePostCount = missingSummary.SendStatePostCount
	report.Summary.MissingAlarmPostCount = missingSummary.MissingAlarmPostCount
	report.Summary.MissingSendStatePostCount = missingSummary.MissingSendStatePostCount
	report.Summary.AttemptedMissingPostCount = missingSummary.AttemptedMissingPostCount
	report.Summary.NotSentMissingPostCount = missingSummary.NotSentMissingPostCount
	report.MissingAlarmRows = missingRows
	report.Results = buildCommunityShortsAlarmSentHistoryDatasetResults(
		report.Rows,
		report.VerificationRows,
		report.ReferenceRows,
		report.MissingAlarmRows,
		report.Summary,
		true,
	)
	return report
}

func buildCommunityShortsAlarmSentHistoryDatasetMissingAlarmRows(
	referenceRows []CommunityShortsAlarmSentHistoryDatasetReferenceRow,
	sendStateRows []CommunityShortsSendStateRow,
) ([]CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow, communityShortsAlarmSentHistoryDatasetMissingAlarmSummary) {
	summary := communityShortsAlarmSentHistoryDatasetMissingAlarmSummary{
		SendStatePostCount: len(sendStateRows),
	}
	if len(referenceRows) == 0 {
		return nil, summary
	}

	sendStateByPostKey := make(map[string]CommunityShortsSendStateRow, len(sendStateRows))
	for i := range sendStateRows {
		row := sendStateRows[i]
		postKey := strings.TrimSpace(row.PostKey)
		if postKey == "" {
			postKey = buildCommunityShortsObservationPostKey(row.ReportAlarmType, row.ReportChannelID, row.ReportPostID)
		}
		if postKey == "" {
			continue
		}
		if existing, ok := sendStateByPostKey[postKey]; ok {
			sendStateByPostKey[postKey] = mergeCommunityShortsAlarmSentHistoryDatasetMissingAlarmStateRow(existing, row)
			continue
		}
		sendStateByPostKey[postKey] = row
	}

	rows := make([]CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow, 0, len(referenceRows))
	for i := range referenceRows {
		referenceRow := referenceRows[i]
		postKey := buildCommunityShortsObservationPostKey(referenceRow.AlarmType, referenceRow.ChannelID, referenceRow.PostID)
		stateRow, ok := sendStateByPostKey[postKey]
		if ok && stateRow.SendState == CommunityShortsPerPostSendStateSent {
			continue
		}

		missingRow := CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow{
			AlarmType:           referenceRow.AlarmType,
			ChannelID:           referenceRow.ChannelID,
			ChannelPostKey:      referenceRow.ChannelPostKey,
			PostKey:             postKey,
			PostID:              referenceRow.PostID,
			ActualPublishedAt:   cloneCommunityShortsSendCountTime(referenceRow.ActualPublishedAt),
			DetectedAt:          cloneCommunityShortsSendCountTime(referenceRow.DetectedAt),
			VerificationVerdict: referenceRow.VerificationVerdict,
			VerificationReason:  referenceRow.VerificationReason,
			RelatedSentPostIDs:  cloneCommunityShortsAlarmSentHistoryDatasetStrings(referenceRow.RelatedSentPostIDs),
		}

		switch {
		case !ok:
			summary.MissingSendStatePostCount++
			missingRow.MissingReason = CommunityShortsMissingAlarmReasonSendStateMissing
		case stateRow.SendState == CommunityShortsPerPostSendStateAttemptedWithoutSuccess:
			summary.AttemptedMissingPostCount++
			missingRow.MissingReason = CommunityShortsMissingAlarmReasonAttempted
			missingRow.SendState = stateRow.SendState
			missingRow.StateContentID = strings.TrimSpace(stateRow.ContentID)
			missingRow.StateDetectedAt = cloneCommunityShortsSendCountTime(stateRow.ReportDetectedAt)
			missingRow.StateAlarmSentAt = cloneCommunityShortsSendCountTime(stateRow.ReportAlarmSentAt)
		default:
			summary.NotSentMissingPostCount++
			missingRow.MissingReason = CommunityShortsMissingAlarmReasonNotSent
			missingRow.SendState = stateRow.SendState
			missingRow.StateContentID = strings.TrimSpace(stateRow.ContentID)
			missingRow.StateDetectedAt = cloneCommunityShortsSendCountTime(stateRow.ReportDetectedAt)
			missingRow.StateAlarmSentAt = cloneCommunityShortsSendCountTime(stateRow.ReportAlarmSentAt)
		}

		summary.MissingAlarmPostCount++
		rows = append(rows, missingRow)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		left := communityShortsAlarmSentHistoryDatasetMissingAlarmSortTime(rows[i])
		right := communityShortsAlarmSentHistoryDatasetMissingAlarmSortTime(rows[j])
		if !left.Equal(right) {
			return left.Before(right)
		}
		if rows[i].AlarmType != rows[j].AlarmType {
			return rows[i].AlarmType < rows[j].AlarmType
		}
		if rows[i].ChannelID != rows[j].ChannelID {
			return rows[i].ChannelID < rows[j].ChannelID
		}
		return rows[i].PostID < rows[j].PostID
	})

	return rows, summary
}

func mergeCommunityShortsAlarmSentHistoryDatasetMissingAlarmStateRow(
	current CommunityShortsSendStateRow,
	next CommunityShortsSendStateRow,
) CommunityShortsSendStateRow {
	if communityShortsAlarmSentHistoryDatasetMissingAlarmStatePriority(next.SendState) > communityShortsAlarmSentHistoryDatasetMissingAlarmStatePriority(current.SendState) {
		return next
	}
	return current
}

func communityShortsAlarmSentHistoryDatasetMissingAlarmStatePriority(state CommunityShortsPerPostSendState) int {
	switch state {
	case CommunityShortsPerPostSendStateSent:
		return 30
	case CommunityShortsPerPostSendStateAttemptedWithoutSuccess:
		return 20
	case CommunityShortsPerPostSendStateNotSent:
		return 10
	default:
		return 0
	}
}

func communityShortsAlarmSentHistoryDatasetMissingAlarmSortTime(
	row CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow,
) time.Time {
	for _, candidate := range []*time.Time{row.ActualPublishedAt, row.DetectedAt, row.StateDetectedAt, row.StateAlarmSentAt} {
		if candidate != nil {
			return candidate.UTC()
		}
	}
	return time.Time{}
}

func buildCommunityShortsObservationPostKey(alarmType domain.AlarmType, channelID string, postID string) string {
	trimmedChannelID := strings.TrimSpace(channelID)
	trimmedPostID := strings.TrimSpace(postID)
	if !alarmType.IsValid() || trimmedChannelID == "" || trimmedPostID == "" {
		return ""
	}
	return strings.Join([]string{string(alarmType), trimmedChannelID, trimmedPostID}, "|")
}
