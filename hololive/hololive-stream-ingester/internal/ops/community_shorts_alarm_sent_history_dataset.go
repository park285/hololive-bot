package ops

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
