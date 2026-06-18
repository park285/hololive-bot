package alarmhistory

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"

	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

func CollectDataset(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	options DatasetCollectOptions,
) (DatasetReport, error) {
	if ctx == nil {
		return DatasetReport{}, fmt.Errorf("collect community shorts alarm sent history dataset: context is nil")
	}
	if appConfig == nil {
		return DatasetReport{}, fmt.Errorf("collect community shorts alarm sent history dataset: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = shared.NormalizeSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}

	query, err := normalizeDatasetCollectOptions(options)
	if err != nil {
		return DatasetReport{}, fmt.Errorf("collect community shorts alarm sent history dataset: %w", err)
	}

	session, cleanupDB, err := shared.OpenOpsSession(ctx, appConfig, logger)
	if err != nil {
		return DatasetReport{}, fmt.Errorf("collect community shorts alarm sent history dataset: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	return collectDatasetWithSession(ctx, session, now, query)
}

func collectDatasetWithSession(
	ctx context.Context,
	session *shared.OpsSession,
	now time.Time,
	query DatasetQuery,
) (DatasetReport, error) {
	if session == nil {
		return DatasetReport{}, fmt.Errorf("collect community shorts alarm sent history dataset: session is nil")
	}

	window, err := session.TrackingRepository.FindClosedCommunityShortsObservationWindow(
		ctx,
		query.ObservationRuntimeName,
		*query.ObservationBigBangCutoverAt,
		now,
	)
	if err != nil {
		return DatasetReport{}, fmt.Errorf("collect community shorts alarm sent history dataset: find observation window: %w", err)
	}
	if window == nil {
		return DatasetReport{}, fmt.Errorf(
			"collect community shorts alarm sent history dataset: observation window not found: runtime=%s cutover=%s",
			query.ObservationRuntimeName,
			shared.FormatSendCountTime(*query.ObservationBigBangCutoverAt),
		)
	}
	query.WindowStart = shared.CloneSendCountTime(&window.ObservationStartedAt)
	query.WindowEnd = shared.CloneSendCountTime(&window.ObservationEndedAt)

	collected, err := collectDatasetRows(ctx, session, query, window)
	if err != nil {
		return DatasetReport{}, err
	}

	comparison, err := buildDatasetComparison(
		ctx,
		session.TrackingRepository,
		collected.Baselines,
		collected.CommunityRows,
		collected.ShortsRows,
	)
	if err != nil {
		return DatasetReport{}, fmt.Errorf("collect community shorts alarm sent history dataset: build comparison: %w", err)
	}

	report := BuildDataset(collected.CommunityRows, collected.ShortsRows, &comparison, query, now)
	attachDatasetMissingAlarmRows(&report, collected.SendStateRows)
	return report, nil
}

type datasetRawRows struct {
	Baselines     []domain.YouTubeCommunityShortsObservationPostBaseline
	CommunityRows []trackingrepo.CommunityAlarmSentHistoryRow
	ShortsRows    []trackingrepo.ShortsAlarmSentHistoryRow
	SendStateRows []outbox.PostSendCount
}

func collectDatasetRows(
	ctx context.Context,
	session *shared.OpsSession,
	query DatasetQuery,
	window *domain.YouTubeCommunityShortsObservationWindow,
) (datasetRawRows, error) {
	baselines, err := session.TrackingRepository.ListCommunityShortsObservationPostBaselines(
		ctx,
		query.ObservationRuntimeName,
		window.BigBangCutoverAt,
	)
	if err != nil {
		return datasetRawRows{}, fmt.Errorf("collect community shorts alarm sent history dataset: list observation baselines: %w", err)
	}

	communityRows, err := session.TrackingRepository.ListCommunityAlarmSentHistoriesWithinObservationWindow(
		ctx,
		window.ObservationStartedAt,
		window.ObservationEndedAt,
		window.ObservationEndedAt,
	)
	if err != nil {
		return datasetRawRows{}, fmt.Errorf("collect community shorts alarm sent history dataset: list community sent histories: %w", err)
	}

	shortsRows, err := session.TrackingRepository.ListShortsAlarmSentHistoriesWithinObservationWindow(
		ctx,
		window.ObservationStartedAt,
		window.ObservationEndedAt,
		window.ObservationEndedAt,
	)
	if err != nil {
		return datasetRawRows{}, fmt.Errorf("collect community shorts alarm sent history dataset: list shorts sent histories: %w", err)
	}

	sendStateRows, err := session.TelemetryRepository.ListPostSendCountsByFinalizedObservationWindow(
		ctx,
		query.ObservationRuntimeName,
		window.BigBangCutoverAt,
	)
	if err != nil {
		return datasetRawRows{}, fmt.Errorf("collect community shorts alarm sent history dataset: list finalized send states: %w", err)
	}

	return datasetRawRows{
		Baselines:     baselines,
		CommunityRows: communityRows,
		ShortsRows:    shortsRows,
		SendStateRows: sendStateRows,
	}, nil
}

func BuildDataset(
	communityRows []trackingrepo.CommunityAlarmSentHistoryRow,
	shortsRows []trackingrepo.ShortsAlarmSentHistoryRow,
	comparison *trackingrepo.ObservationPostComparisonResult,
	query DatasetQuery,
	generatedAt time.Time,
) DatasetReport {
	generatedAt = shared.NormalizeSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	query = normalizeDatasetQuery(query)

	finalizedCommunity := finalizeCommunityAlarmSentHistoryRows(communityRows)
	finalizedShorts := finalizeShortsAlarmSentHistoryRows(shortsRows)
	rows := buildDatasetRows(finalizedCommunity.Rows, finalizedShorts.Rows)
	comparisonValue := trackingrepo.ObservationPostComparisonResult{}
	if comparison != nil {
		comparisonValue = *comparison
	}
	verificationRows := buildDatasetVerificationRows(comparisonValue.VerdictRows)
	referenceRows := buildDatasetReferenceRows(comparisonValue.VerdictRows)
	summary := buildDatasetSummary(
		finalizedCommunity,
		finalizedShorts,
		rows,
		verificationRows,
		referenceRows,
		&comparisonValue.Summary,
	)

	return DatasetReport{
		GeneratedAt:      generatedAt,
		Query:            query,
		Summary:          summary,
		Results:          buildDatasetResults(rows, verificationRows, referenceRows, nil, &summary, false),
		Comparison:       comparisonValue,
		Rows:             rows,
		VerificationRows: verificationRows,
		ReferenceRows:    referenceRows,
	}
}

func buildDatasetRows(
	communityRows []trackingrepo.ObservationAlarmSentHistoryRow,
	shortsRows []trackingrepo.ObservationAlarmSentHistoryRow,
) []DatasetRow {
	rows := make([]DatasetRow, 0, len(communityRows)+len(shortsRows))

	appendRows := func(alarmType domain.AlarmType, inputs []trackingrepo.ObservationAlarmSentHistoryRow) {
		for i := range inputs {
			row := inputs[i]
			postID := strings.TrimSpace(row.PostID)
			channelID := strings.TrimSpace(row.ChannelID)
			rows = append(rows, DatasetRow{
				AlarmType:         alarmType,
				PostKey:           buildObservationPostKey(alarmType, channelID, postID),
				PostID:            postID,
				ContentID:         strings.TrimSpace(row.ContentID),
				ChannelID:         channelID,
				ActualPublishedAt: shared.CloneSendCountTime(row.ActualPublishedAt),
				DetectedAt:        shared.NormalizeSendCountTime(row.DetectedAt),
				AlarmSentAt:       shared.NormalizeSendCountTime(row.AlarmSentAt),
			})
		}
	}
	appendRows(domain.AlarmTypeCommunity, communityRows)
	appendRows(domain.AlarmTypeShorts, shortsRows)

	sortDatasetRows(rows)
	return rows
}

func sortDatasetRows(rows []DatasetRow) {
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
}

func buildDatasetSummary(
	finalizedCommunity finalizationResult,
	finalizedShorts finalizationResult,
	rows []DatasetRow,
	verificationRows []DatasetVerificationRow,
	referenceRows []DatasetReferenceRow,
	comparison *trackingrepo.ObservationPostComparisonSummary,
) DatasetSummary {
	summary := DatasetSummary{
		CollectedRowCount:      finalizedCommunity.CollectedRowCount + finalizedShorts.CollectedRowCount,
		DuplicateRowCount:      finalizedCommunity.DuplicateRowCount + finalizedShorts.DuplicateRowCount,
		SentPostCount:          len(rows),
		CommunitySentPostCount: len(finalizedCommunity.Rows),
		ShortsSentPostCount:    len(finalizedShorts.Rows),
		VerificationRowCount:   len(verificationRows),
		ReferenceRowCount:      len(referenceRows),
	}
	if comparison != nil {
		summary.BaselinePostCount = comparison.BaselineUniquePostCount
		summary.MatchedPostCount = comparison.MatchedPostCount
		summary.UnsentPostCount = comparison.UnsentPostCount
		summary.DuplicateSentPostCount = comparison.DuplicateSentPostCount
		summary.UnexpectedSentPostCount = comparison.UnexpectedSentPostCount
		summary.IdentifierMismatchCandidateCount = comparison.IdentifierMismatchCandidateCount
	}
	for i := range rows {
		row := rows[i]
		if summary.EarliestAlarmSentAt == nil || row.AlarmSentAt.Before(summary.EarliestAlarmSentAt.UTC()) {
			summary.EarliestAlarmSentAt = shared.CloneSendCountTime(&row.AlarmSentAt)
		}
		if summary.LatestAlarmSentAt == nil || row.AlarmSentAt.After(summary.LatestAlarmSentAt.UTC()) {
			summary.LatestAlarmSentAt = shared.CloneSendCountTime(&row.AlarmSentAt)
		}
	}
	return summary
}

func buildDatasetComparison(
	ctx context.Context,
	repository *trackingrepo.PgxRepository,
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

func normalizeDatasetCollectOptions(
	options DatasetCollectOptions,
) (DatasetQuery, error) {
	runtimeName := strings.TrimSpace(options.ObservationRuntimeName)
	cutoverAt := shared.CloneSendCountTime(options.ObservationBigBangCutoverAt)
	if runtimeName == "" || cutoverAt == nil || cutoverAt.IsZero() {
		return DatasetQuery{}, fmt.Errorf("observation runtime name and big-bang cutover at are required")
	}

	return DatasetQuery{
		ObservationRuntimeName:      runtimeName,
		ObservationBigBangCutoverAt: cutoverAt,
	}, nil
}

func normalizeDatasetQuery(
	query DatasetQuery,
) DatasetQuery {
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = shared.CloneSendCountTime(query.ObservationBigBangCutoverAt)
	query.WindowStart = shared.CloneSendCountTime(query.WindowStart)
	query.WindowEnd = shared.CloneSendCountTime(query.WindowEnd)
	return query
}
