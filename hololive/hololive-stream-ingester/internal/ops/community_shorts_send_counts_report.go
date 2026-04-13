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

type CommunityShortsSendCountQueryMode string

const (
	communityShortsSendCountQueryModeRecent      CommunityShortsSendCountQueryMode          = "recent_window"
	communityShortsSendCountQueryModeObservation CommunityShortsSendCountQueryMode          = "observation_window"
	communityShortsSendCountDuplicateAlarmPass   CommunityShortsSendCountVerificationStatus = "pass"
	communityShortsSendCountDuplicateAlarmFail   CommunityShortsSendCountVerificationStatus = "fail"
	communityShortsSendCountDuplicateAlarmRule                                              = "duplicate_success_posts == 0"
)

type CommunityShortsSendCountCollectOptions struct {
	Since                       *time.Time
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time
}

type CommunityShortsSendCountQuery struct {
	Mode                        CommunityShortsSendCountQueryMode `json:"mode"`
	WindowStart                 *time.Time                        `json:"window_start,omitempty"`
	WindowEnd                   *time.Time                        `json:"window_end,omitempty"`
	ObservationRuntimeName      string                            `json:"observation_runtime_name,omitempty"`
	ObservationBigBangCutoverAt *time.Time                        `json:"observation_bigbang_cutover_at,omitempty"`
}

type CommunityShortsSendCountReport struct {
	GeneratedAt  time.Time                            `json:"generated_at"`
	Query        CommunityShortsSendCountQuery        `json:"query"`
	WindowStart  time.Time                            `json:"window_start"`
	WindowEnd    time.Time                            `json:"window_end"`
	Summary      CommunityShortsSendCountSummary      `json:"summary"`
	Verification CommunityShortsSendCountVerification `json:"verification"`
	Rows         []CommunityShortsSendCountRow        `json:"rows"`
}

type CommunityShortsSendCountSummary struct {
	PostCount                         int `json:"post_count"`
	SuccessfulPostCount               int `json:"successful_post_count"`
	ZeroSuccessPostCount              int `json:"zero_success_post_count"`
	DuplicateSuccessPostCount         int `json:"duplicate_success_post_count"`
	FailedAttemptPostCount            int `json:"failed_attempt_post_count"`
	OutboxMissingPostCount            int `json:"outbox_missing_post_count"`
	ExternalCollectionSourcePostCount int `json:"external_collection_source_post_count"`
	InternalDeliverySourcePostCount   int `json:"internal_delivery_source_post_count"`
	MixedDelaySourcePostCount         int `json:"mixed_delay_source_post_count"`
	QueueWaitCausePostCount           int `json:"queue_wait_cause_post_count"`
	RetryAccumulationCausePostCount   int `json:"retry_accumulation_cause_post_count"`
	JobFailureCausePostCount          int `json:"job_failure_cause_post_count"`
}

type CommunityShortsSendCountVerificationStatus string

type CommunityShortsSendCountVerification struct {
	DuplicateAlarmStatus    CommunityShortsSendCountVerificationStatus `json:"duplicate_alarm_status"`
	DuplicateAlarmPostCount int                                        `json:"duplicate_alarm_post_count"`
	DuplicateAlarmRule      string                                     `json:"duplicate_alarm_rule"`
}

type CommunityShortsSendCountRow struct {
	outbox.PostSendCount
	ReportAlarmType         domain.AlarmType                       `json:"alarm_type"`
	ReportChannelID         string                                 `json:"channel_id"`
	ReportPostID            string                                 `json:"post_id"`
	ReportActualPublishedAt *time.Time                             `json:"actual_published_at,omitempty"`
	ReportAlarmSentAt       *time.Time                             `json:"alarm_sent_at,omitempty"`
	ReportDelaySeconds      *float64                               `json:"delay_seconds,omitempty"`
	PublishToDetectMillis   *int64                                 `json:"publish_to_detect_millis,omitempty"`
	DelaySource             outbox.PostDelaySource                 `json:"delay_source"`
	QueueWaitMillis         *int64                                 `json:"queue_wait_millis,omitempty"`
	RetryAccumulationMillis *int64                                 `json:"retry_accumulation_millis,omitempty"`
	JobFailureDetected      bool                                   `json:"job_failure_detected"`
	InternalDelayCause      outbox.PostInternalDelayCause          `json:"internal_delay_cause"`
	LatencyClassification   outbox.PostLatencyClassificationResult `json:"latency_classification"`
}

type communityShortsSendCountKey struct {
	channelID string
	alarmType domain.AlarmType
	contentID string
}

func CollectCommunityShortsSendCountReport(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	since time.Time,
) (CommunityShortsSendCountReport, error) {
	return CollectCommunityShortsSendCountReportWithOptions(
		ctx,
		cfg,
		logger,
		now,
		CommunityShortsSendCountCollectOptions{Since: cloneCommunityShortsSendCountTime(&since)},
	)
}

func CollectCommunityShortsSendCountReportWithOptions(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CommunityShortsSendCountCollectOptions,
) (CommunityShortsSendCountReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return CommunityShortsSendCountReport{}, fmt.Errorf("collect community shorts send count report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = normalizeCommunityShortsSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}

	query, err := normalizeCommunityShortsSendCountCollectOptions(options, now)
	if err != nil {
		return CommunityShortsSendCountReport{}, fmt.Errorf("collect community shorts send count report: %w", err)
	}

	databaseResources, cleanupDB, err := sharedproviders.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
	if err != nil {
		return CommunityShortsSendCountReport{}, fmt.Errorf("collect community shorts send count report: provide database resources: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	db := databaseResources.Service.GetGormDB()
	telemetryRepo := outbox.NewDeliveryTelemetryRepository(db)

	var sendCountRows []outbox.PostSendCount
	var timelineRows []outbox.PostDeliveryTimeline
	switch query.Mode {
	case communityShortsSendCountQueryModeObservation:
		observationRepository := trackingrepo.NewRepository(db)
		state, stateErr := resolveCommunityShortsObservationQueryState(
			ctx,
			observationRepository,
			query.ObservationRuntimeName,
			*query.ObservationBigBangCutoverAt,
			now,
		)
		if stateErr != nil {
			return CommunityShortsSendCountReport{}, fmt.Errorf("collect community shorts send count report: find observation window: %w", stateErr)
		}
		if state.Window == nil {
			return CommunityShortsSendCountReport{}, fmt.Errorf(
				"collect community shorts send count report: observation window not found: runtime=%s cutover=%s",
				query.ObservationRuntimeName,
				formatCommunityShortsSendCountTime(*query.ObservationBigBangCutoverAt),
			)
		}
		query.WindowStart = cloneCommunityShortsSendCountTime(&state.Window.ObservationStartedAt)
		query.WindowEnd = cloneCommunityShortsSendCountTime(&state.EffectiveWindowEnd)

		if state.Finalized {
			sendCountRows, err = telemetryRepo.ListPostSendCountsByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, state.Window.BigBangCutoverAt)
			if err != nil {
				return CommunityShortsSendCountReport{}, fmt.Errorf("collect community shorts send count report: list finalized observation-window send counts: %w", err)
			}
			timelineRows, err = telemetryRepo.ListPostDeliveryTimelinesByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, state.Window.BigBangCutoverAt)
			if err != nil {
				return CommunityShortsSendCountReport{}, fmt.Errorf("collect community shorts send count report: list finalized observation-window delivery timelines: %w", err)
			}
			break
		}

		if state.EffectiveWindowEnd.After(state.Window.ObservationStartedAt) {
			sendCountRows, err = telemetryRepo.ListPostSendCountsWithinObservationWindow(ctx, state.Window.ObservationStartedAt, state.EffectiveWindowEnd, state.EffectiveWindowEnd)
			if err != nil {
				return CommunityShortsSendCountReport{}, fmt.Errorf("collect community shorts send count report: list active observation-window send counts: %w", err)
			}
			timelineRows, err = telemetryRepo.ListPostDeliveryTimelinesWithinObservationWindow(ctx, state.Window.ObservationStartedAt, state.EffectiveWindowEnd, state.EffectiveWindowEnd)
			if err != nil {
				return CommunityShortsSendCountReport{}, fmt.Errorf("collect community shorts send count report: list active observation-window delivery timelines: %w", err)
			}
		}
	default:
		sendCountRows, err = telemetryRepo.ListPostSendCountsSince(ctx, *query.WindowStart)
		if err != nil {
			return CommunityShortsSendCountReport{}, fmt.Errorf("collect community shorts send count report: list post send counts: %w", err)
		}
		timelineRows, err = telemetryRepo.ListPostDeliveryTimelinesSince(ctx, *query.WindowStart)
		if err != nil {
			return CommunityShortsSendCountReport{}, fmt.Errorf("collect community shorts send count report: list post delivery timelines: %w", err)
		}
	}

	return BuildCommunityShortsSendCountReportWithQuery(sendCountRows, timelineRows, query, now), nil
}

func BuildCommunityShortsSendCountReport(
	sendCountRows []outbox.PostSendCount,
	timelineRows []outbox.PostDeliveryTimeline,
	generatedAt time.Time,
	since time.Time,
) CommunityShortsSendCountReport {
	query := CommunityShortsSendCountQuery{
		Mode:        communityShortsSendCountQueryModeRecent,
		WindowStart: cloneCommunityShortsSendCountTime(&since),
		WindowEnd:   cloneCommunityShortsSendCountTime(&generatedAt),
	}
	return BuildCommunityShortsSendCountReportWithQuery(sendCountRows, timelineRows, query, generatedAt)
}

func BuildCommunityShortsSendCountReportWithQuery(
	sendCountRows []outbox.PostSendCount,
	timelineRows []outbox.PostDeliveryTimeline,
	query CommunityShortsSendCountQuery,
	generatedAt time.Time,
) CommunityShortsSendCountReport {
	generatedAt = normalizeCommunityShortsSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	query = normalizeCommunityShortsSendCountQuery(query)
	if query.Mode == "" {
		query.Mode = communityShortsSendCountQueryModeRecent
	}
	if query.WindowEnd == nil {
		query.WindowEnd = cloneCommunityShortsSendCountTime(&generatedAt)
	}

	timelineIndex := make(map[communityShortsSendCountKey]outbox.PostDeliveryTimeline, len(timelineRows))
	for i := range timelineRows {
		timeline := normalizeCommunityShortsDeliveryTimeline(timelineRows[i])
		key := buildCommunityShortsSendCountKey(timeline.ChannelID, timeline.AlarmType, timeline.ContentID)
		if key.contentID == "" {
			continue
		}
		timelineIndex[key] = timeline
	}

	normalizedRows := make([]CommunityShortsSendCountRow, 0, len(sendCountRows))
	summary := CommunityShortsSendCountSummary{}
	for i := range sendCountRows {
		row := CommunityShortsSendCountRow{PostSendCount: normalizeCommunityShortsPostSendCount(sendCountRows[i])}
		row.ReportAlarmType = row.AlarmType
		row.ReportChannelID = row.ChannelID
		row.ReportPostID = resolveCommunityShortsSendCountPostID(row)
		row.ReportActualPublishedAt = cloneCommunityShortsSendCountTime(row.ActualPublishedAt)
		row.ReportAlarmSentAt = resolveCommunityShortsSendCountAlarmSentAt(row)
		row.ReportDelaySeconds = buildCommunityShortsSendCountDelaySeconds(
			row.AlarmLatencyMillis,
			row.ReportActualPublishedAt,
			row.ReportAlarmSentAt,
		)
		if timeline, ok := timelineIndex[buildCommunityShortsSendCountKey(row.ChannelID, row.AlarmType, row.ContentID)]; ok {
			row.PublishToDetectMillis = cloneCommunityShortsSendCountInt64(timeline.PublishToDetectMillis)
			row.DelaySource = timeline.DelaySource
			row.QueueWaitMillis = cloneCommunityShortsSendCountInt64(timeline.QueueWaitMillis)
			row.RetryAccumulationMillis = cloneCommunityShortsSendCountInt64(timeline.RetryAccumulationMillis)
			row.JobFailureDetected = timeline.JobFailureDetected
			row.InternalDelayCause = timeline.InternalDelayCause
			row.LatencyClassification = cloneCommunityShortsLatencyClassification(timeline.LatencyClassification)
		}
		if row.DelaySource == "" {
			row.DelaySource = outbox.PostDelaySourceNone
		}
		if row.InternalDelayCause == "" {
			row.InternalDelayCause = outbox.PostInternalDelayCauseNone
		}

		normalizedRows = append(normalizedRows, row)
		summary.PostCount++
		if row.SuccessSendCount > 0 {
			summary.SuccessfulPostCount++
		} else {
			summary.ZeroSuccessPostCount++
		}
		if row.DuplicateSuccessCount > 0 {
			summary.DuplicateSuccessPostCount++
		}
		if row.FailedAttemptCount > 0 {
			summary.FailedAttemptPostCount++
		}
		if row.OutboxCount == 0 {
			summary.OutboxMissingPostCount++
		}
		switch row.DelaySource {
		case outbox.PostDelaySourceExternalCollection:
			summary.ExternalCollectionSourcePostCount++
		case outbox.PostDelaySourceInternalDelivery:
			summary.InternalDeliverySourcePostCount++
		case outbox.PostDelaySourceMixed:
			summary.MixedDelaySourcePostCount++
		}
		switch row.InternalDelayCause {
		case outbox.PostInternalDelayCauseQueueWait:
			summary.QueueWaitCausePostCount++
		case outbox.PostInternalDelayCauseRetryAccumulation:
			summary.RetryAccumulationCausePostCount++
		case outbox.PostInternalDelayCauseJobFailure:
			summary.JobFailureCausePostCount++
		}
	}

	sort.SliceStable(normalizedRows, func(i, j int) bool {
		left := communityShortsSendCountSortTime(normalizedRows[i])
		right := communityShortsSendCountSortTime(normalizedRows[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		if normalizedRows[i].AlarmType != normalizedRows[j].AlarmType {
			return normalizedRows[i].AlarmType < normalizedRows[j].AlarmType
		}
		if normalizedRows[i].ChannelID != normalizedRows[j].ChannelID {
			return normalizedRows[i].ChannelID < normalizedRows[j].ChannelID
		}
		if normalizedRows[i].PostID != normalizedRows[j].PostID {
			return normalizedRows[i].PostID < normalizedRows[j].PostID
		}
		return normalizedRows[i].ContentID < normalizedRows[j].ContentID
	})

	windowStart := normalizeCommunityShortsSendCountTimePtrValue(query.WindowStart)
	windowEnd := normalizeCommunityShortsSendCountTimePtrValue(query.WindowEnd)
	return CommunityShortsSendCountReport{
		GeneratedAt:  generatedAt,
		Query:        query,
		WindowStart:  windowStart,
		WindowEnd:    windowEnd,
		Summary:      summary,
		Verification: buildCommunityShortsSendCountVerification(summary),
		Rows:         normalizedRows,
	}
}

func normalizeCommunityShortsSendCountCollectOptions(
	options CommunityShortsSendCountCollectOptions,
	now time.Time,
) (CommunityShortsSendCountQuery, error) {
	observationRuntimeName := strings.TrimSpace(options.ObservationRuntimeName)
	hasObservationCutover := options.ObservationBigBangCutoverAt != nil && !options.ObservationBigBangCutoverAt.IsZero()
	hasObservationQuery := observationRuntimeName != "" || hasObservationCutover
	hasRecentQuery := options.Since != nil && !options.Since.IsZero()

	if hasObservationQuery {
		if hasRecentQuery {
			return CommunityShortsSendCountQuery{}, fmt.Errorf("recent window and observation window are mutually exclusive")
		}
		if observationRuntimeName == "" || !hasObservationCutover {
			return CommunityShortsSendCountQuery{}, fmt.Errorf("observation runtime name and cutover must both be set")
		}
		return CommunityShortsSendCountQuery{
			Mode:                        communityShortsSendCountQueryModeObservation,
			ObservationRuntimeName:      observationRuntimeName,
			ObservationBigBangCutoverAt: cloneCommunityShortsSendCountTime(options.ObservationBigBangCutoverAt),
		}, nil
	}

	if !hasRecentQuery {
		return CommunityShortsSendCountQuery{}, fmt.Errorf("recent window since is empty")
	}
	since := normalizeCommunityShortsSendCountTime(*options.Since)
	if since.IsZero() {
		return CommunityShortsSendCountQuery{}, fmt.Errorf("recent window since is empty")
	}
	if since.After(now) {
		return CommunityShortsSendCountQuery{}, fmt.Errorf("recent window since is after now")
	}

	return CommunityShortsSendCountQuery{
		Mode:        communityShortsSendCountQueryModeRecent,
		WindowStart: cloneCommunityShortsSendCountTime(&since),
		WindowEnd:   cloneCommunityShortsSendCountTime(&now),
	}, nil
}

func buildCommunityShortsSendCountKey(channelID string, alarmType domain.AlarmType, contentID string) communityShortsSendCountKey {
	return communityShortsSendCountKey{
		channelID: strings.TrimSpace(channelID),
		alarmType: alarmType,
		contentID: strings.TrimSpace(contentID),
	}
}

func normalizeCommunityShortsPostSendCount(row outbox.PostSendCount) outbox.PostSendCount {
	row.ChannelID = strings.TrimSpace(row.ChannelID)
	row.PostID = strings.TrimSpace(row.PostID)
	row.ContentID = strings.TrimSpace(row.ContentID)
	row.ActualPublishedAt = cloneCommunityShortsSendCountTime(row.ActualPublishedAt)
	row.DetectedAt = cloneCommunityShortsSendCountTime(row.DetectedAt)
	row.AlarmSentAt = cloneCommunityShortsSendCountTime(row.AlarmSentAt)
	row.FirstEventAt = cloneCommunityShortsSendCountTime(row.FirstEventAt)
	row.LastEventAt = cloneCommunityShortsSendCountTime(row.LastEventAt)
	row.FirstSuccessAt = cloneCommunityShortsSendCountTime(row.FirstSuccessAt)
	row.LastSuccessAt = cloneCommunityShortsSendCountTime(row.LastSuccessAt)
	return row
}

func normalizeCommunityShortsDeliveryTimeline(row outbox.PostDeliveryTimeline) outbox.PostDeliveryTimeline {
	row.ChannelID = strings.TrimSpace(row.ChannelID)
	row.PostID = strings.TrimSpace(row.PostID)
	row.ContentID = strings.TrimSpace(row.ContentID)
	row.PublishToDetectMillis = cloneCommunityShortsSendCountInt64(row.PublishToDetectMillis)
	if row.DelaySource == "" {
		row.DelaySource = outbox.PostDelaySourceNone
	}
	row.QueueWaitMillis = cloneCommunityShortsSendCountInt64(row.QueueWaitMillis)
	row.RetryAccumulationMillis = cloneCommunityShortsSendCountInt64(row.RetryAccumulationMillis)
	if row.InternalDelayCause == "" {
		row.InternalDelayCause = outbox.PostInternalDelayCauseNone
	}
	row.LatencyClassification = cloneCommunityShortsLatencyClassification(row.LatencyClassification)
	return row
}

func normalizeCommunityShortsSendCountQuery(query CommunityShortsSendCountQuery) CommunityShortsSendCountQuery {
	query.Mode = CommunityShortsSendCountQueryMode(strings.TrimSpace(string(query.Mode)))
	query.WindowStart = cloneCommunityShortsSendCountTime(query.WindowStart)
	query.WindowEnd = cloneCommunityShortsSendCountTime(query.WindowEnd)
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = cloneCommunityShortsSendCountTime(query.ObservationBigBangCutoverAt)
	return query
}

func communityShortsSendCountSortTime(row CommunityShortsSendCountRow) time.Time {
	for _, candidate := range []*time.Time{row.LastSuccessAt, row.LastEventAt, row.AlarmSentAt, row.ActualPublishedAt, row.DetectedAt} {
		if candidate != nil {
			return candidate.UTC()
		}
	}
	return time.Time{}
}

func resolveCommunityShortsSendCountAlarmSentAt(row CommunityShortsSendCountRow) *time.Time {
	for _, candidate := range []*time.Time{row.AlarmSentAt, row.FirstSuccessAt, row.LastSuccessAt} {
		if candidate != nil {
			return cloneCommunityShortsSendCountTime(candidate)
		}
	}
	return nil
}

func resolveCommunityShortsSendCountPostID(row CommunityShortsSendCountRow) string {
	if strings.TrimSpace(row.PostID) != "" {
		return strings.TrimSpace(row.PostID)
	}
	return strings.TrimSpace(row.ContentID)
}

func buildCommunityShortsSendCountDelaySeconds(
	latencyMillis *int64,
	actualPublishedAt *time.Time,
	alarmSentAt *time.Time,
) *float64 {
	if latencyMillis != nil {
		seconds := float64(*latencyMillis) / float64(time.Second/time.Millisecond)
		return &seconds
	}
	if actualPublishedAt == nil || alarmSentAt == nil {
		return nil
	}
	seconds := alarmSentAt.UTC().Sub(actualPublishedAt.UTC()).Seconds()
	return &seconds
}
