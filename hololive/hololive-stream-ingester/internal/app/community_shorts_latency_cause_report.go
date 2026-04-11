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

const (
	communityShortsLatencyCauseObservedAtBasis        = "COALESCE(actual_published_at, detected_at)"
	communityShortsLatencyCauseObservationPeriodLabel = "observation_window"
	communityShortsLatencyCauseInternalCauseRule      = "internal_system if delay_source in {internal_delivery,mixed} OR (internal_delay_cause != none AND delay_source != external_collection)"
	communityShortsLatencyCauseNonInternalCauseRule   = "non_internal if delay_source = external_collection OR (delay_source = none AND internal_delay_cause = none)"
	communityShortsLatencyCauseExcludedExternalRule   = "delay_source = external_collection rows stay logged as reference-only excluded_external_delay_posts and do not contribute to failure-driving counts"
	communityShortsLatencyCauseInsufficientEvidence   = "latency_classification.status = insufficient_evidence keeps the row in non_internal and increments insufficient_evidence_posts"
)

var communityShortsLatencyCauseEvidenceFieldCatalog = []string{
	"delay_source",
	"internal_delay_cause",
	"alarm_latency_millis",
	"publish_to_detect_millis",
	"internal_latency_millis",
	"queue_wait_millis",
	"retry_accumulation_millis",
	"job_failure_detected",
	"latency_classification.status",
	"latency_classification.evidence",
}

type CommunityShortsLatencyCauseQueryMode string

const (
	communityShortsLatencyCauseQueryModeRecent      CommunityShortsLatencyCauseQueryMode = "recent_window"
	communityShortsLatencyCauseQueryModeObservation CommunityShortsLatencyCauseQueryMode = "observation_window"
)

type CommunityShortsInternalCauseJudgment string

const (
	CommunityShortsInternalCauseJudgmentInternalSystem CommunityShortsInternalCauseJudgment = "internal_system"
	CommunityShortsInternalCauseJudgmentNonInternal    CommunityShortsInternalCauseJudgment = "non_internal"
)

type CommunityShortsLatencyCauseCollectOptions struct {
	PeriodSpecs                 []CommunityShortsLatencyPeriodSpec
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time
}

type CommunityShortsLatencyCauseQuery struct {
	Mode                        CommunityShortsLatencyCauseQueryMode `json:"mode"`
	WindowStart                 *time.Time                           `json:"window_start,omitempty"`
	WindowEnd                   *time.Time                           `json:"window_end,omitempty"`
	ObservationRuntimeName      string                               `json:"observation_runtime_name,omitempty"`
	ObservationBigBangCutoverAt *time.Time                           `json:"observation_bigbang_cutover_at,omitempty"`
}

type CommunityShortsLatencyCauseVerification struct {
	InternalCauseRule        string   `json:"internal_cause_rule"`
	NonInternalCauseRule     string   `json:"non_internal_cause_rule"`
	ExcludedExternalRule     string   `json:"excluded_external_rule"`
	InsufficientEvidenceRule string   `json:"insufficient_evidence_rule"`
	EvidenceFieldCatalog     []string `json:"evidence_field_catalog"`
}

type CommunityShortsLatencyCauseEvidence struct {
	Fields                     []string                                      `json:"fields"`
	SelectedClassificationKeys []outbox.PostLatencyClassificationEvidenceKey `json:"selected_classification_keys,omitempty"`
}

type CommunityShortsLatencyCauseReport struct {
	GeneratedAt      time.Time                               `json:"generated_at"`
	Query            CommunityShortsLatencyCauseQuery        `json:"query"`
	ObservedAtBasis  string                                  `json:"observed_at_basis"`
	ThresholdMillis  int64                                   `json:"threshold_millis"`
	Verification     CommunityShortsLatencyCauseVerification `json:"verification"`
	RequestedPeriods []CommunityShortsLatencyPeriodSpec      `json:"requested_periods"`
	Periods          []CommunityShortsLatencyCausePeriodView `json:"periods"`
}

type CommunityShortsLatencyCausePeriodView struct {
	Summary      outbox.PostLatencyPeriodSummary    `json:"summary"`
	CauseSummary CommunityShortsLatencyCauseSummary `json:"cause_summary"`
	Rows         []CommunityShortsLatencyCauseRow   `json:"rows"`
}

type CommunityShortsLatencyCauseSummary struct {
	ExceededPostCount                  int64 `json:"exceeded_post_count"`
	InternalSystemCausePostCount       int64 `json:"internal_system_cause_post_count"`
	NonInternalSystemCausePostCount    int64 `json:"non_internal_system_cause_post_count"`
	ExcludedExternalDelayPostCount     int64 `json:"excluded_external_delay_post_count"`
	CommunityExceededPostCount         int64 `json:"community_exceeded_post_count"`
	ShortsExceededPostCount            int64 `json:"shorts_exceeded_post_count"`
	ExternalCollectionSourcePostCount  int64 `json:"external_collection_source_post_count"`
	InternalDeliverySourcePostCount    int64 `json:"internal_delivery_source_post_count"`
	MixedDelaySourcePostCount          int64 `json:"mixed_delay_source_post_count"`
	NoDominantSourcePostCount          int64 `json:"no_dominant_source_post_count"`
	InternalCauseCandidatePostCount    int64 `json:"internal_cause_candidate_post_count"`
	QueueWaitCausePostCount            int64 `json:"queue_wait_cause_post_count"`
	RetryAccumulationCausePostCount    int64 `json:"retry_accumulation_cause_post_count"`
	JobFailureCausePostCount           int64 `json:"job_failure_cause_post_count"`
	UnclassifiedInternalCausePostCount int64 `json:"unclassified_internal_cause_post_count"`
	InsufficientEvidencePostCount      int64 `json:"insufficient_evidence_post_count"`
}

type CommunityShortsLatencyCauseRow struct {
	AlarmType               domain.AlarmType                       `json:"alarm_type"`
	ChannelID               string                                 `json:"channel_id"`
	PostID                  string                                 `json:"post_id"`
	ContentID               string                                 `json:"content_id"`
	ObservedAt              *time.Time                             `json:"observed_at,omitempty"`
	ActualPublishedAt       *time.Time                             `json:"actual_published_at,omitempty"`
	DetectedAt              *time.Time                             `json:"detected_at,omitempty"`
	AlarmSentAt             *time.Time                             `json:"alarm_sent_at,omitempty"`
	AlarmLatencyMillis      *int64                                 `json:"alarm_latency_millis,omitempty"`
	PublishToDetectMillis   *int64                                 `json:"publish_to_detect_millis,omitempty"`
	InternalLatencyMillis   *int64                                 `json:"internal_latency_millis,omitempty"`
	QueueWaitMillis         *int64                                 `json:"queue_wait_millis,omitempty"`
	RetryAccumulationMillis *int64                                 `json:"retry_accumulation_millis,omitempty"`
	JobFailureDetected      bool                                   `json:"job_failure_detected"`
	DelaySource             outbox.PostDelaySource                 `json:"delay_source"`
	InternalDelayCause      outbox.PostInternalDelayCause          `json:"internal_delay_cause"`
	InternalCauseJudgment   CommunityShortsInternalCauseJudgment   `json:"internal_cause_judgment"`
	InternalCauseBasis      string                                 `json:"internal_cause_basis"`
	CauseEvidence           CommunityShortsLatencyCauseEvidence    `json:"cause_evidence"`
	LatencyClassification   outbox.PostLatencyClassificationResult `json:"latency_classification"`
}

func CollectCommunityShortsLatencyCauseReport(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	specs []CommunityShortsLatencyPeriodSpec,
) (CommunityShortsLatencyCauseReport, error) {
	return CollectCommunityShortsLatencyCauseReportWithOptions(
		ctx,
		cfg,
		logger,
		now,
		CommunityShortsLatencyCauseCollectOptions{PeriodSpecs: specs},
	)
}

func CollectCommunityShortsLatencyCauseReportWithOptions(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CommunityShortsLatencyCauseCollectOptions,
) (CommunityShortsLatencyCauseReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = normalizeCommunityShortsSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}

	query, periods, err := normalizeCommunityShortsLatencyCauseCollectOptions(options, now)
	if err != nil {
		return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: %w", err)
	}

	databaseResources, cleanupDB, err := sharedproviders.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
	if err != nil {
		return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: provide database resources: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	db := databaseResources.Service.GetGormDB()
	telemetryRepo := outbox.NewDeliveryTelemetryRepository(db)

	var sendCountRows []outbox.PostSendCount
	var timelineRows []outbox.PostDeliveryTimeline
	switch query.Mode {
	case communityShortsLatencyCauseQueryModeObservation:
		observationRepository := trackingrepo.NewRepository(db)
		state, stateErr := resolveCommunityShortsObservationQueryState(
			ctx,
			observationRepository,
			query.ObservationRuntimeName,
			*query.ObservationBigBangCutoverAt,
			now,
		)
		if stateErr != nil {
			return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: find observation window: %w", stateErr)
		}
		if state.Window == nil {
			return CommunityShortsLatencyCauseReport{}, fmt.Errorf(
				"collect community shorts latency cause report: observation window not found: runtime=%s cutover=%s",
				query.ObservationRuntimeName,
				formatCommunityShortsSendCountTime(*query.ObservationBigBangCutoverAt),
			)
		}
		query.WindowStart = cloneCommunityShortsSendCountTime(&state.Window.ObservationStartedAt)
		query.WindowEnd = cloneCommunityShortsSendCountTime(&state.EffectiveWindowEnd)
		if state.EffectiveWindowEnd.After(state.Window.ObservationStartedAt) {
			periods = []outbox.PostLatencyPeriod{{
				Label:   communityShortsLatencyCauseObservationPeriodLabel,
				StartAt: state.Window.ObservationStartedAt,
				EndAt:   state.EffectiveWindowEnd,
			}}
		} else {
			periods = nil
		}

		if state.Finalized {
			sendCountRows, err = telemetryRepo.ListPostSendCountsByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, state.Window.BigBangCutoverAt)
			if err != nil {
				return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: list finalized observation-window send counts: %w", err)
			}
			timelineRows, err = telemetryRepo.ListPostDeliveryTimelinesByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, state.Window.BigBangCutoverAt)
			if err != nil {
				return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: list finalized observation-window delivery timelines: %w", err)
			}
			break
		}

		if state.EffectiveWindowEnd.After(state.Window.ObservationStartedAt) {
			sendCountRows, err = telemetryRepo.ListPostSendCountsWithinObservationWindow(ctx, state.Window.ObservationStartedAt, state.EffectiveWindowEnd, state.EffectiveWindowEnd)
			if err != nil {
				return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: list active observation-window send counts: %w", err)
			}
			timelineRows, err = telemetryRepo.ListPostDeliveryTimelinesWithinObservationWindow(ctx, state.Window.ObservationStartedAt, state.EffectiveWindowEnd, state.EffectiveWindowEnd)
			if err != nil {
				return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: list active observation-window delivery timelines: %w", err)
			}
		}
	default:
		since := earliestCommunityShortsLatencyCausePeriodStart(periods)
		sendCountRows, err = telemetryRepo.ListPostSendCountsSince(ctx, since)
		if err != nil {
			return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: list post send counts: %w", err)
		}
		timelineRows, err = telemetryRepo.ListPostDeliveryTimelinesSince(ctx, since)
		if err != nil {
			return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: list post delivery timelines: %w", err)
		}
	}

	report, err := BuildCommunityShortsLatencyCauseReportWithQuery(sendCountRows, timelineRows, query, now, periods)
	if err != nil {
		return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: %w", err)
	}
	return report, nil
}

func BuildCommunityShortsLatencyCauseReport(
	sendCountRows []outbox.PostSendCount,
	timelineRows []outbox.PostDeliveryTimeline,
	generatedAt time.Time,
	periods []outbox.PostLatencyPeriod,
) (CommunityShortsLatencyCauseReport, error) {
	return BuildCommunityShortsLatencyCauseReportWithQuery(
		sendCountRows,
		timelineRows,
		CommunityShortsLatencyCauseQuery{Mode: communityShortsLatencyCauseQueryModeRecent},
		generatedAt,
		periods,
	)
}

func BuildCommunityShortsLatencyCauseReportWithQuery(
	sendCountRows []outbox.PostSendCount,
	timelineRows []outbox.PostDeliveryTimeline,
	query CommunityShortsLatencyCauseQuery,
	generatedAt time.Time,
	periods []outbox.PostLatencyPeriod,
) (CommunityShortsLatencyCauseReport, error) {
	generatedAt = normalizeCommunityShortsSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	normalizedPeriods, requestedPeriods, err := normalizeCommunityShortsLatencyCausePeriods(periods)
	if err != nil {
		return CommunityShortsLatencyCauseReport{}, fmt.Errorf("build community shorts latency cause report: %w", err)
	}
	query = withCommunityShortsLatencyCauseQueryWindow(normalizeCommunityShortsLatencyCauseQuery(query), normalizedPeriods)
	if query.Mode == "" {
		query.Mode = communityShortsLatencyCauseQueryModeRecent
	}

	summaries, err := outbox.BuildPostLatencyPeriodSummaries(sendCountRows, normalizedPeriods)
	if err != nil {
		return CommunityShortsLatencyCauseReport{}, fmt.Errorf("build community shorts latency cause report: build post latency period summaries: %w", err)
	}

	periodRows := make([][]CommunityShortsLatencyCauseRow, len(normalizedPeriods))
	timelineIndex := make(map[communityShortsSendCountKey]outbox.PostDeliveryTimeline, len(timelineRows))
	for i := range timelineRows {
		timeline := normalizeCommunityShortsDeliveryTimeline(timelineRows[i])
		key := buildCommunityShortsSendCountKey(timeline.ChannelID, timeline.AlarmType, timeline.ContentID)
		if key.contentID == "" {
			continue
		}
		timelineIndex[key] = timeline
	}

	for i := range sendCountRows {
		sendCount := normalizeCommunityShortsPostSendCount(sendCountRows[i])
		if !isCommunityShortsLatencyCauseExceeded(sendCount) {
			continue
		}

		observedAt, err := resolveCommunityShortsLatencyCauseObservedAt(sendCount)
		if err != nil {
			return CommunityShortsLatencyCauseReport{}, fmt.Errorf("build community shorts latency cause report: post[%d] %s: %w", i, strings.TrimSpace(sendCount.ContentID), err)
		}

		key := buildCommunityShortsSendCountKey(sendCount.ChannelID, sendCount.AlarmType, sendCount.ContentID)
		timeline, hasTimeline := timelineIndex[key]
		row := buildCommunityShortsLatencyCauseRow(sendCount, observedAt, timeline, hasTimeline)
		for periodIndex := range normalizedPeriods {
			if observedAt.Before(normalizedPeriods[periodIndex].StartAt) || !observedAt.Before(normalizedPeriods[periodIndex].EndAt) {
				continue
			}
			periodRows[periodIndex] = append(periodRows[periodIndex], row)
		}
	}

	periodViews := make([]CommunityShortsLatencyCausePeriodView, 0, len(summaries))
	for i := range summaries {
		rows := append([]CommunityShortsLatencyCauseRow(nil), periodRows[i]...)
		sort.SliceStable(rows, func(left, right int) bool {
			leftTime := communityShortsLatencyCauseSortTime(rows[left])
			rightTime := communityShortsLatencyCauseSortTime(rows[right])
			if !leftTime.Equal(rightTime) {
				return leftTime.After(rightTime)
			}
			if rows[left].AlarmType != rows[right].AlarmType {
				return rows[left].AlarmType < rows[right].AlarmType
			}
			if rows[left].ChannelID != rows[right].ChannelID {
				return rows[left].ChannelID < rows[right].ChannelID
			}
			if rows[left].PostID != rows[right].PostID {
				return rows[left].PostID < rows[right].PostID
			}
			return rows[left].ContentID < rows[right].ContentID
		})
		periodViews = append(periodViews, CommunityShortsLatencyCausePeriodView{
			Summary:      cloneCommunityShortsLatencyPeriodSummary(summaries[i]),
			CauseSummary: buildCommunityShortsLatencyCauseSummary(rows),
			Rows:         rows,
		})
	}

	return CommunityShortsLatencyCauseReport{
		GeneratedAt:      generatedAt,
		Query:            query,
		ObservedAtBasis:  communityShortsLatencyCauseObservedAtBasis,
		ThresholdMillis:  int64((2 * time.Minute) / time.Millisecond),
		Verification:     buildCommunityShortsLatencyCauseVerification(),
		RequestedPeriods: requestedPeriods,
		Periods:          periodViews,
	}, nil
}

func RenderCommunityShortsLatencyCauseMarkdown(report CommunityShortsLatencyCauseReport) string {
	var builder strings.Builder

	builder.WriteString("# YouTube Community/Shorts Latency Cause Report\n\n")
	builder.WriteString("- generated at: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.GeneratedAt))
	builder.WriteString("`\n")
	builder.WriteString("- mode: `")
	builder.WriteString(string(report.Query.Mode))
	builder.WriteString("`\n")
	if report.Query.WindowStart != nil || report.Query.WindowEnd != nil {
		builder.WriteString("- window: `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.WindowStart))
		builder.WriteString("` -> `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.WindowEnd))
		builder.WriteString("`\n")
	}
	if report.Query.Mode == communityShortsLatencyCauseQueryModeObservation {
		builder.WriteString("- observation runtime: `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(report.Query.ObservationRuntimeName))
		builder.WriteString("`, cutover: `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.ObservationBigBangCutoverAt))
		builder.WriteString("`\n")
	}
	builder.WriteString("- observed at basis: `")
	builder.WriteString(fallbackCommunityShortsSendCountValue(report.ObservedAtBasis))
	builder.WriteString("`\n")
	builder.WriteString("- threshold millis: `")
	builder.WriteString(fmt.Sprintf("%d", report.ThresholdMillis))
	builder.WriteString("`\n")
	builder.WriteString("- internal cause rule: `")
	builder.WriteString(report.Verification.InternalCauseRule)
	builder.WriteString("`\n")
	builder.WriteString("- non internal rule: `")
	builder.WriteString(report.Verification.NonInternalCauseRule)
	builder.WriteString("`\n")
	builder.WriteString("- excluded external rule: `")
	builder.WriteString(report.Verification.ExcludedExternalRule)
	builder.WriteString("`\n")
	builder.WriteString("- insufficient evidence rule: `")
	builder.WriteString(report.Verification.InsufficientEvidenceRule)
	builder.WriteString("`\n")
	builder.WriteString("- cause evidence fields: `")
	builder.WriteString(strings.Join(report.Verification.EvidenceFieldCatalog, ", "))
	builder.WriteString("`\n")
	builder.WriteString("- periods: `")
	builder.WriteString(fmt.Sprintf("%d", len(report.Periods)))
	builder.WriteString("`\n")

	if len(report.Periods) == 0 {
		builder.WriteString("\n조회된 community/shorts 지연 원인 리포트가 없습니다.\n")
		return builder.String()
	}

	for i := range report.Periods {
		period := report.Periods[i]
		builder.WriteString("\n## `")
		builder.WriteString(strings.TrimSpace(period.Summary.Label))
		builder.WriteString("`\n\n")
		builder.WriteString("- window: `")
		builder.WriteString(formatCommunityShortsSendCountTime(period.Summary.StartAt))
		builder.WriteString("` -> `")
		builder.WriteString(formatCommunityShortsSendCountTime(period.Summary.EndAt))
		builder.WriteString("`\n")
		builder.WriteString("- latency summary: total_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.Summary.TotalPostCount))
		builder.WriteString("`, alarm_sent_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.Summary.AlarmSentPostCount))
		builder.WriteString("`, pending_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.Summary.PendingPostCount))
		builder.WriteString("`, measured_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.Summary.LatencyMeasuredPostCount))
		builder.WriteString("`, avg_latency_ms=`")
		builder.WriteString(formatCommunityShortsSendCountInt64Ptr(period.Summary.AverageLatencyMillis))
		builder.WriteString("`, p95_latency_ms=`")
		builder.WriteString(formatCommunityShortsSendCountInt64Ptr(period.Summary.P95LatencyMillis))
		builder.WriteString("`, max_latency_ms=`")
		builder.WriteString(formatCommunityShortsSendCountInt64Ptr(period.Summary.MaxLatencyMillis))
		builder.WriteString("`, over_2m_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.Summary.ExceededPostCount))
		builder.WriteString("`\n")
		builder.WriteString("- cause summary: exceeded_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.ExceededPostCount))
		builder.WriteString("`, internal_system_cause_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.InternalSystemCausePostCount))
		builder.WriteString("`, non_internal_system_cause_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.NonInternalSystemCausePostCount))
		builder.WriteString("`, excluded_external_delay_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.ExcludedExternalDelayPostCount))
		builder.WriteString("`, community_exceeded_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.CommunityExceededPostCount))
		builder.WriteString("`, shorts_exceeded_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.ShortsExceededPostCount))
		builder.WriteString("`, external_collection_source_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.ExternalCollectionSourcePostCount))
		builder.WriteString("`, internal_delivery_source_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.InternalDeliverySourcePostCount))
		builder.WriteString("`, mixed_delay_source_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.MixedDelaySourcePostCount))
		builder.WriteString("`, no_dominant_source_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.NoDominantSourcePostCount))
		builder.WriteString("`, internal_cause_candidate_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.InternalCauseCandidatePostCount))
		builder.WriteString("`, queue_wait_cause_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.QueueWaitCausePostCount))
		builder.WriteString("`, retry_accumulation_cause_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.RetryAccumulationCausePostCount))
		builder.WriteString("`, job_failure_cause_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.JobFailureCausePostCount))
		builder.WriteString("`, unclassified_internal_cause_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.UnclassifiedInternalCausePostCount))
		builder.WriteString("`, insufficient_evidence_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.InsufficientEvidencePostCount))
		builder.WriteString("`\n")
		builder.WriteString("- excluded external reference: excluded_external_delay_posts=`")
		builder.WriteString(fmt.Sprintf("%d", period.CauseSummary.ExcludedExternalDelayPostCount))
		builder.WriteString("`, rule=`")
		builder.WriteString(report.Verification.ExcludedExternalRule)
		builder.WriteString("`\n")

		if len(period.Rows) == 0 {
			builder.WriteString("\n2분 초과 community/shorts 게시물이 없습니다.\n")
			continue
		}

		builder.WriteString("\n| alarm_type | channel_id | post_id | observed_at | actual_published_at | detected_at | alarm_sent_at | alarm_latency_ms | internal_cause_judgment | internal_cause_basis | cause_evidence_fields | delay_source | internal_delay_cause | publish_to_detect_ms | internal_latency_ms | queue_wait_ms | retry_accumulation_ms | job_failure_detected | cause_classification_status | cause_classification_evidence |\n")
		builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | ---: | --- | --- | --- | --- | --- | ---: | ---: | ---: | ---: | --- | --- | --- |\n")
		for rowIndex := range period.Rows {
			row := period.Rows[rowIndex]
			builder.WriteString("| `")
			builder.WriteString(string(row.AlarmType))
			builder.WriteString("` | `")
			builder.WriteString(fallbackCommunityShortsSendCountValue(row.ChannelID))
			builder.WriteString("` | `")
			builder.WriteString(fallbackCommunityShortsSendCountValue(row.PostID))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ObservedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.DetectedAt))
			builder.WriteString("` | `")
			builder.WriteString(formatCommunityShortsSendCountTimePtr(row.AlarmSentAt))
			builder.WriteString("` | ")
			builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.AlarmLatencyMillis))
			builder.WriteString(" | `")
			builder.WriteString(string(row.InternalCauseJudgment))
			builder.WriteString("` | `")
			builder.WriteString(fallbackCommunityShortsSendCountValue(row.InternalCauseBasis))
			builder.WriteString("` | `")
			builder.WriteString(renderCommunityShortsLatencyCauseEvidenceFields(row.CauseEvidence))
			builder.WriteString("` | `")
			builder.WriteString(string(row.DelaySource))
			builder.WriteString("` | `")
			builder.WriteString(string(row.InternalDelayCause))
			builder.WriteString("` | ")
			builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.PublishToDetectMillis))
			builder.WriteString(" | ")
			builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.InternalLatencyMillis))
			builder.WriteString(" | ")
			builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.QueueWaitMillis))
			builder.WriteString(" | ")
			builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.RetryAccumulationMillis))
			builder.WriteString(" | `")
			builder.WriteString(formatCommunityShortsSendCountBool(row.JobFailureDetected))
			builder.WriteString("` | `")
			builder.WriteString(string(row.LatencyClassification.Status))
			builder.WriteString("` | `")
			builder.WriteString(renderCommunityShortsLatencyClassificationEvidence(row.LatencyClassification))
			builder.WriteString("` |\n")
		}
	}

	return builder.String()
}

func normalizeCommunityShortsLatencyCauseCollectOptions(
	options CommunityShortsLatencyCauseCollectOptions,
	now time.Time,
) (CommunityShortsLatencyCauseQuery, []outbox.PostLatencyPeriod, error) {
	observationRuntimeName := strings.TrimSpace(options.ObservationRuntimeName)
	hasObservationCutover := options.ObservationBigBangCutoverAt != nil && !options.ObservationBigBangCutoverAt.IsZero()
	hasObservationQuery := observationRuntimeName != "" || hasObservationCutover

	if hasObservationQuery {
		if len(options.PeriodSpecs) > 0 {
			return CommunityShortsLatencyCauseQuery{}, nil, fmt.Errorf("period specs and observation window are mutually exclusive")
		}
		if observationRuntimeName == "" || !hasObservationCutover {
			return CommunityShortsLatencyCauseQuery{}, nil, fmt.Errorf("observation runtime name and cutover must both be set")
		}
		return CommunityShortsLatencyCauseQuery{
			Mode:                        communityShortsLatencyCauseQueryModeObservation,
			ObservationRuntimeName:      observationRuntimeName,
			ObservationBigBangCutoverAt: cloneCommunityShortsSendCountTime(options.ObservationBigBangCutoverAt),
		}, nil, nil
	}

	periods, err := buildCommunityShortsLatencyCausePeriods(now, options.PeriodSpecs)
	if err != nil {
		return CommunityShortsLatencyCauseQuery{}, nil, err
	}

	return withCommunityShortsLatencyCauseQueryWindow(CommunityShortsLatencyCauseQuery{Mode: communityShortsLatencyCauseQueryModeRecent}, periods), periods, nil
}

func normalizeCommunityShortsLatencyCauseQuery(query CommunityShortsLatencyCauseQuery) CommunityShortsLatencyCauseQuery {
	query.Mode = CommunityShortsLatencyCauseQueryMode(strings.TrimSpace(string(query.Mode)))
	query.WindowStart = cloneCommunityShortsSendCountTime(query.WindowStart)
	query.WindowEnd = cloneCommunityShortsSendCountTime(query.WindowEnd)
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = cloneCommunityShortsSendCountTime(query.ObservationBigBangCutoverAt)
	return query
}

func withCommunityShortsLatencyCauseQueryWindow(
	query CommunityShortsLatencyCauseQuery,
	periods []outbox.PostLatencyPeriod,
) CommunityShortsLatencyCauseQuery {
	if query.WindowStart == nil {
		if startAt := earliestCommunityShortsLatencyCausePeriodStart(periods); !startAt.IsZero() {
			query.WindowStart = cloneCommunityShortsSendCountTime(&startAt)
		}
	}
	if query.WindowEnd == nil {
		if endAt := latestCommunityShortsLatencyCausePeriodEnd(periods); !endAt.IsZero() {
			query.WindowEnd = cloneCommunityShortsSendCountTime(&endAt)
		}
	}
	return query
}

func buildCommunityShortsLatencyCausePeriods(
	now time.Time,
	specs []CommunityShortsLatencyPeriodSpec,
) ([]outbox.PostLatencyPeriod, error) {
	periods, err := buildCommunityShortsLatencyPeriods(now, specs)
	if err != nil {
		return nil, err
	}
	return periods, nil
}

func normalizeCommunityShortsLatencyCausePeriods(
	periods []outbox.PostLatencyPeriod,
) ([]outbox.PostLatencyPeriod, []CommunityShortsLatencyPeriodSpec, error) {
	if len(periods) == 0 {
		return []outbox.PostLatencyPeriod{}, []CommunityShortsLatencyPeriodSpec{}, nil
	}

	normalized := make([]outbox.PostLatencyPeriod, 0, len(periods))
	requestedPeriods := make([]CommunityShortsLatencyPeriodSpec, 0, len(periods))
	seenLabels := make(map[string]struct{}, len(periods))
	for i := range periods {
		label := strings.TrimSpace(periods[i].Label)
		if label == "" {
			return nil, nil, fmt.Errorf("period at index %d: label is empty", i)
		}
		if periods[i].StartAt.IsZero() {
			return nil, nil, fmt.Errorf("period %q: start at is empty", label)
		}
		if periods[i].EndAt.IsZero() {
			return nil, nil, fmt.Errorf("period %q: end at is empty", label)
		}
		startAt := periods[i].StartAt.UTC()
		endAt := periods[i].EndAt.UTC()
		if !endAt.After(startAt) {
			return nil, nil, fmt.Errorf("period %q: end at must be after start at", label)
		}
		if _, exists := seenLabels[label]; exists {
			return nil, nil, fmt.Errorf("period %q: duplicate label", label)
		}
		seenLabels[label] = struct{}{}
		normalized = append(normalized, outbox.PostLatencyPeriod{Label: label, StartAt: startAt, EndAt: endAt})
		requestedPeriods = append(requestedPeriods, CommunityShortsLatencyPeriodSpec{Label: label, Window: endAt.Sub(startAt)})
	}
	return normalized, requestedPeriods, nil
}

func earliestCommunityShortsLatencyCausePeriodStart(periods []outbox.PostLatencyPeriod) time.Time {
	if len(periods) == 0 {
		return time.Time{}
	}
	startAt := periods[0].StartAt
	for i := 1; i < len(periods); i++ {
		if periods[i].StartAt.Before(startAt) {
			startAt = periods[i].StartAt
		}
	}
	return startAt.UTC()
}

func latestCommunityShortsLatencyCausePeriodEnd(periods []outbox.PostLatencyPeriod) time.Time {
	if len(periods) == 0 {
		return time.Time{}
	}
	endAt := periods[0].EndAt
	for i := 1; i < len(periods); i++ {
		if periods[i].EndAt.After(endAt) {
			endAt = periods[i].EndAt
		}
	}
	return endAt.UTC()
}

func isCommunityShortsLatencyCauseExceeded(row outbox.PostSendCount) bool {
	return row.AlarmLatencyExceeded != nil && *row.AlarmLatencyExceeded
}

func resolveCommunityShortsLatencyCauseObservedAt(row outbox.PostSendCount) (time.Time, error) {
	if row.ActualPublishedAt != nil {
		return row.ActualPublishedAt.UTC(), nil
	}
	if row.DetectedAt != nil {
		return row.DetectedAt.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("observed at is empty")
}

func buildCommunityShortsLatencyCauseRow(
	sendCount outbox.PostSendCount,
	observedAt time.Time,
	timeline outbox.PostDeliveryTimeline,
	hasTimeline bool,
) CommunityShortsLatencyCauseRow {
	classification := outbox.PostLatencyClassificationResult{
		Status:             outbox.PostLatencyClassificationStatusInsufficientEvidence,
		ThresholdMillis:    int64((2 * time.Minute) / time.Millisecond),
		DelaySource:        outbox.PostDelaySourceNone,
		InternalDelayCause: outbox.PostInternalDelayCauseNone,
	}

	row := CommunityShortsLatencyCauseRow{
		AlarmType:             sendCount.AlarmType,
		ChannelID:             strings.TrimSpace(sendCount.ChannelID),
		PostID:                resolveCommunityShortsSendCountPostID(CommunityShortsSendCountRow{PostSendCount: sendCount}),
		ContentID:             strings.TrimSpace(sendCount.ContentID),
		ObservedAt:            cloneCommunityShortsSendCountTime(&observedAt),
		ActualPublishedAt:     cloneCommunityShortsSendCountTime(sendCount.ActualPublishedAt),
		DetectedAt:            cloneCommunityShortsSendCountTime(sendCount.DetectedAt),
		AlarmSentAt:           cloneCommunityShortsSendCountTime(sendCount.AlarmSentAt),
		AlarmLatencyMillis:    cloneCommunityShortsSendCountInt64(sendCount.AlarmLatencyMillis),
		DelaySource:           outbox.PostDelaySourceNone,
		InternalDelayCause:    outbox.PostInternalDelayCauseNone,
		InternalCauseJudgment: CommunityShortsInternalCauseJudgmentNonInternal,
		LatencyClassification: classification,
	}

	if !hasTimeline {
		row.InternalCauseJudgment, row.InternalCauseBasis = classifyCommunityShortsLatencyCauseInternalJudgment(row)
		row.CauseEvidence = buildCommunityShortsLatencyCauseEvidence(row)
		return row
	}

	classification = cloneCommunityShortsLatencyClassification(timeline.LatencyClassification)
	if classification.Status == "" {
		classification.Status = outbox.PostLatencyClassificationStatusInsufficientEvidence
	}
	if classification.ThresholdMillis <= 0 {
		classification.ThresholdMillis = int64((2 * time.Minute) / time.Millisecond)
	}
	if classification.DelaySource == "" {
		classification.DelaySource = outbox.PostDelaySourceNone
	}
	if classification.InternalDelayCause == "" {
		classification.InternalDelayCause = outbox.PostInternalDelayCauseNone
	}

	row.PublishToDetectMillis = cloneCommunityShortsSendCountInt64(timeline.PublishToDetectMillis)
	row.InternalLatencyMillis = cloneCommunityShortsSendCountInt64(timeline.InternalLatencyMillis)
	row.QueueWaitMillis = cloneCommunityShortsSendCountInt64(timeline.QueueWaitMillis)
	row.RetryAccumulationMillis = cloneCommunityShortsSendCountInt64(timeline.RetryAccumulationMillis)
	row.JobFailureDetected = timeline.JobFailureDetected
	row.DelaySource = timeline.DelaySource
	if row.DelaySource == "" {
		row.DelaySource = classification.DelaySource
	}
	if row.DelaySource == "" {
		row.DelaySource = outbox.PostDelaySourceNone
	}
	row.InternalDelayCause = timeline.InternalDelayCause
	if row.InternalDelayCause == "" {
		row.InternalDelayCause = classification.InternalDelayCause
	}
	if row.InternalDelayCause == "" {
		row.InternalDelayCause = outbox.PostInternalDelayCauseNone
	}
	classification.DelaySource = row.DelaySource
	classification.InternalDelayCause = row.InternalDelayCause
	row.LatencyClassification = classification
	row.InternalCauseJudgment, row.InternalCauseBasis = classifyCommunityShortsLatencyCauseInternalJudgment(row)
	row.CauseEvidence = buildCommunityShortsLatencyCauseEvidence(row)
	return row
}

func buildCommunityShortsLatencyCauseSummary(rows []CommunityShortsLatencyCauseRow) CommunityShortsLatencyCauseSummary {
	summary := CommunityShortsLatencyCauseSummary{}
	for i := range rows {
		row := rows[i]
		summary.ExceededPostCount++
		switch row.InternalCauseJudgment {
		case CommunityShortsInternalCauseJudgmentInternalSystem:
			summary.InternalSystemCausePostCount++
			summary.InternalCauseCandidatePostCount++
			incrementCommunityShortsLatencyCauseInternalCause(&summary, row.InternalDelayCause)
		default:
			summary.NonInternalSystemCausePostCount++
		}
		switch row.AlarmType {
		case domain.AlarmTypeCommunity:
			summary.CommunityExceededPostCount++
		case domain.AlarmTypeShorts:
			summary.ShortsExceededPostCount++
		}
		switch row.DelaySource {
		case outbox.PostDelaySourceExternalCollection:
			summary.ExcludedExternalDelayPostCount++
			summary.ExternalCollectionSourcePostCount++
		case outbox.PostDelaySourceInternalDelivery:
			summary.InternalDeliverySourcePostCount++
		case outbox.PostDelaySourceMixed:
			summary.MixedDelaySourcePostCount++
		default:
			summary.NoDominantSourcePostCount++
		}
		if row.LatencyClassification.Status == outbox.PostLatencyClassificationStatusInsufficientEvidence {
			summary.InsufficientEvidencePostCount++
		}
	}
	return summary
}

func classifyCommunityShortsLatencyCauseInternalJudgment(
	row CommunityShortsLatencyCauseRow,
) (CommunityShortsInternalCauseJudgment, string) {
	if row.DelaySource == outbox.PostDelaySourceExternalCollection {
		return CommunityShortsInternalCauseJudgmentNonInternal, "delay_source=external_collection"
	}

	if row.InternalDelayCause != "" && row.InternalDelayCause != outbox.PostInternalDelayCauseNone {
		return CommunityShortsInternalCauseJudgmentInternalSystem, buildCommunityShortsInternalCauseBasis(row.DelaySource, row.InternalDelayCause)
	}

	switch row.DelaySource {
	case outbox.PostDelaySourceInternalDelivery:
		return CommunityShortsInternalCauseJudgmentInternalSystem, "delay_source=internal_delivery"
	case outbox.PostDelaySourceMixed:
		return CommunityShortsInternalCauseJudgmentInternalSystem, "delay_source=mixed"
	}

	if row.LatencyClassification.Status == outbox.PostLatencyClassificationStatusInsufficientEvidence {
		return CommunityShortsInternalCauseJudgmentNonInternal, "latency_classification=insufficient_evidence"
	}

	return CommunityShortsInternalCauseJudgmentNonInternal, "delay_source=none"
}

func buildCommunityShortsInternalCauseBasis(
	delaySource outbox.PostDelaySource,
	cause outbox.PostInternalDelayCause,
) string {
	parts := make([]string, 0, 2)
	if delaySource != "" && delaySource != outbox.PostDelaySourceNone {
		parts = append(parts, "delay_source="+string(delaySource))
	}
	if cause != "" && cause != outbox.PostInternalDelayCauseNone {
		parts = append(parts, "internal_delay_cause="+string(cause))
	}
	if len(parts) == 0 {
		return "internal_delay_cause=none"
	}
	return strings.Join(parts, ",")
}

func buildCommunityShortsLatencyCauseVerification() CommunityShortsLatencyCauseVerification {
	return CommunityShortsLatencyCauseVerification{
		InternalCauseRule:        communityShortsLatencyCauseInternalCauseRule,
		NonInternalCauseRule:     communityShortsLatencyCauseNonInternalCauseRule,
		ExcludedExternalRule:     communityShortsLatencyCauseExcludedExternalRule,
		InsufficientEvidenceRule: communityShortsLatencyCauseInsufficientEvidence,
		EvidenceFieldCatalog:     append([]string(nil), communityShortsLatencyCauseEvidenceFieldCatalog...),
	}
}

func buildCommunityShortsLatencyCauseEvidence(row CommunityShortsLatencyCauseRow) CommunityShortsLatencyCauseEvidence {
	evidence := CommunityShortsLatencyCauseEvidence{}
	seenFields := make(map[string]struct{}, 6)
	addField := func(field string) {
		if field == "" {
			return
		}
		if _, exists := seenFields[field]; exists {
			return
		}
		seenFields[field] = struct{}{}
		evidence.Fields = append(evidence.Fields, field)
	}

	addField("delay_source")
	addField("internal_delay_cause")

	switch row.DelaySource {
	case outbox.PostDelaySourceExternalCollection:
		addField("publish_to_detect_millis")
	case outbox.PostDelaySourceInternalDelivery:
		addField("internal_latency_millis")
	case outbox.PostDelaySourceMixed:
		addField("publish_to_detect_millis")
		addField("internal_latency_millis")
	}

	switch row.InternalDelayCause {
	case outbox.PostInternalDelayCauseQueueWait:
		addField("queue_wait_millis")
	case outbox.PostInternalDelayCauseRetryAccumulation:
		addField("retry_accumulation_millis")
	case outbox.PostInternalDelayCauseJobFailure:
		addField("job_failure_detected")
	}

	if row.LatencyClassification.Status == outbox.PostLatencyClassificationStatusInsufficientEvidence {
		addField("latency_classification.status")
	}

	for i := range row.LatencyClassification.Evidence {
		item := row.LatencyClassification.Evidence[i]
		if !item.Selected {
			continue
		}
		evidence.SelectedClassificationKeys = append(evidence.SelectedClassificationKeys, item.Key)
		addField(communityShortsLatencyCauseEvidenceFieldForKey(item.Key))
	}
	if len(evidence.SelectedClassificationKeys) > 0 {
		addField("latency_classification.evidence")
	}

	return evidence
}

func communityShortsLatencyCauseEvidenceFieldForKey(key outbox.PostLatencyClassificationEvidenceKey) string {
	switch key {
	case outbox.PostLatencyClassificationEvidenceKeyAlarmLatency:
		return "alarm_latency_millis"
	case outbox.PostLatencyClassificationEvidenceKeyPublishToDetect:
		return "publish_to_detect_millis"
	case outbox.PostLatencyClassificationEvidenceKeyInternalLatency:
		return "internal_latency_millis"
	case outbox.PostLatencyClassificationEvidenceKeyQueueWait:
		return "queue_wait_millis"
	case outbox.PostLatencyClassificationEvidenceKeyRetryAccumulation:
		return "retry_accumulation_millis"
	case outbox.PostLatencyClassificationEvidenceKeyJobFailure:
		return "job_failure_detected"
	default:
		return ""
	}
}

func renderCommunityShortsLatencyCauseEvidenceFields(evidence CommunityShortsLatencyCauseEvidence) string {
	if len(evidence.Fields) == 0 {
		return "(none)"
	}
	return strings.Join(evidence.Fields, ", ")
}

func incrementCommunityShortsLatencyCauseInternalCause(
	summary *CommunityShortsLatencyCauseSummary,
	cause outbox.PostInternalDelayCause,
) {
	if summary == nil {
		return
	}
	switch cause {
	case outbox.PostInternalDelayCauseQueueWait:
		summary.QueueWaitCausePostCount++
	case outbox.PostInternalDelayCauseRetryAccumulation:
		summary.RetryAccumulationCausePostCount++
	case outbox.PostInternalDelayCauseJobFailure:
		summary.JobFailureCausePostCount++
	default:
		summary.UnclassifiedInternalCausePostCount++
	}
}

func communityShortsLatencyCauseSortTime(row CommunityShortsLatencyCauseRow) time.Time {
	for _, candidate := range []*time.Time{row.ObservedAt, row.AlarmSentAt, row.DetectedAt, row.ActualPublishedAt} {
		if candidate != nil {
			return candidate.UTC()
		}
	}
	return time.Time{}
}
