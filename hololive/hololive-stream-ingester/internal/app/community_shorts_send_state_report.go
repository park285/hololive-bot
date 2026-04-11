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

type CommunityShortsPerPostSendState string

const (
	CommunityShortsPerPostSendStateSent                    CommunityShortsPerPostSendState = "sent"
	CommunityShortsPerPostSendStateAttemptedWithoutSuccess CommunityShortsPerPostSendState = "attempted_without_success"
	CommunityShortsPerPostSendStateNotSent                 CommunityShortsPerPostSendState = "not_sent"
)

type CommunityShortsSendStateCollectOptions struct {
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time
}

type CommunityShortsSendStateQuery struct {
	ObservationRuntimeName      string     `json:"observation_runtime_name"`
	ObservationBigBangCutoverAt *time.Time `json:"observation_bigbang_cutover_at,omitempty"`
	WindowStart                 *time.Time `json:"window_start,omitempty"`
	WindowEnd                   *time.Time `json:"window_end,omitempty"`
	Finalized                   bool       `json:"finalized"`
}

type CommunityShortsSendStateSummary struct {
	PostStateCount                   int        `json:"post_state_count"`
	SentPostCount                    int        `json:"sent_post_count"`
	AttemptedWithoutSuccessPostCount int        `json:"attempted_without_success_post_count"`
	NotSentPostCount                 int        `json:"not_sent_post_count"`
	DuplicateSuccessPostCount        int        `json:"duplicate_success_post_count"`
	FailedAttemptPostCount           int        `json:"failed_attempt_post_count"`
	CommunityPostCount               int        `json:"community_post_count"`
	ShortsPostCount                  int        `json:"shorts_post_count"`
	EarliestObservedAt               *time.Time `json:"earliest_observed_at,omitempty"`
	LatestObservedAt                 *time.Time `json:"latest_observed_at,omitempty"`
	EarliestAlarmSentAt              *time.Time `json:"earliest_alarm_sent_at,omitempty"`
	LatestAlarmSentAt                *time.Time `json:"latest_alarm_sent_at,omitempty"`
}

type CommunityShortsSendStateRow struct {
	outbox.PostSendCount
	SendState               CommunityShortsPerPostSendState `json:"send_state"`
	PostKey                 string                          `json:"post_key,omitempty"`
	ReportAlarmType         domain.AlarmType                `json:"alarm_type"`
	ReportChannelID         string                          `json:"channel_id"`
	ReportPostID            string                          `json:"post_id"`
	ReportActualPublishedAt *time.Time                      `json:"actual_published_at,omitempty"`
	ReportDetectedAt        *time.Time                      `json:"detected_at,omitempty"`
	ReportAlarmSentAt       *time.Time                      `json:"alarm_sent_at,omitempty"`
}

type CommunityShortsSendStateReport struct {
	GeneratedAt time.Time                       `json:"generated_at"`
	Query       CommunityShortsSendStateQuery   `json:"query"`
	Summary     CommunityShortsSendStateSummary `json:"summary"`
	Rows        []CommunityShortsSendStateRow   `json:"rows"`
}

func CollectCommunityShortsSendStateReport(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CommunityShortsSendStateCollectOptions,
) (CommunityShortsSendStateReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return CommunityShortsSendStateReport{}, fmt.Errorf("collect community shorts send state report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = normalizeCommunityShortsSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}

	query, err := normalizeCommunityShortsSendStateCollectOptions(options)
	if err != nil {
		return CommunityShortsSendStateReport{}, fmt.Errorf("collect community shorts send state report: %w", err)
	}

	databaseResources, cleanupDB, err := sharedproviders.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
	if err != nil {
		return CommunityShortsSendStateReport{}, fmt.Errorf("collect community shorts send state report: provide database resources: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	db := databaseResources.Service.GetGormDB()
	observationRepository := trackingrepo.NewRepository(db)
	state, err := resolveCommunityShortsObservationQueryState(
		ctx,
		observationRepository,
		query.ObservationRuntimeName,
		*query.ObservationBigBangCutoverAt,
		now,
	)
	if err != nil {
		return CommunityShortsSendStateReport{}, fmt.Errorf("collect community shorts send state report: find observation window: %w", err)
	}
	if state.Window == nil {
		return CommunityShortsSendStateReport{}, fmt.Errorf(
			"collect community shorts send state report: observation window not found: runtime=%s cutover=%s",
			query.ObservationRuntimeName,
			formatCommunityShortsSendCountTime(*query.ObservationBigBangCutoverAt),
		)
	}

	query.WindowStart = cloneCommunityShortsSendCountTime(&state.Window.ObservationStartedAt)
	query.WindowEnd = cloneCommunityShortsSendCountTime(&state.EffectiveWindowEnd)
	query.Finalized = state.Finalized

	telemetryRepo := outbox.NewDeliveryTelemetryRepository(db)
	var rows []outbox.PostSendCount
	if state.Finalized {
		rows, err = telemetryRepo.ListPostSendCountsByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, state.Window.BigBangCutoverAt)
		if err != nil {
			return CommunityShortsSendStateReport{}, fmt.Errorf("collect community shorts send state report: list finalized observation-window send states: %w", err)
		}
	} else {
		rows, err = telemetryRepo.ListPostSendCountsWithinObservationWindow(ctx, state.Window.ObservationStartedAt, state.EffectiveWindowEnd, state.EffectiveWindowEnd)
		if err != nil {
			return CommunityShortsSendStateReport{}, fmt.Errorf("collect community shorts send state report: list active observation-window send states: %w", err)
		}
	}

	return BuildCommunityShortsSendStateReport(rows, query, now), nil
}

func BuildCommunityShortsSendStateReport(
	sendStateRows []outbox.PostSendCount,
	query CommunityShortsSendStateQuery,
	generatedAt time.Time,
) CommunityShortsSendStateReport {
	generatedAt = normalizeCommunityShortsSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	query = normalizeCommunityShortsSendStateQuery(query)

	normalizedRows := make([]CommunityShortsSendStateRow, 0, len(sendStateRows))
	summary := CommunityShortsSendStateSummary{}
	for i := range sendStateRows {
		normalized := normalizeCommunityShortsPostSendCount(sendStateRows[i])
		alarmSentAt := resolveCommunityShortsSendStateAlarmSentAt(normalized)
		postID := resolveCommunityShortsSendStatePostID(normalized)
		row := CommunityShortsSendStateRow{
			PostSendCount:           normalized,
			SendState:               resolveCommunityShortsPerPostSendState(normalized),
			PostKey:                 buildCommunityShortsObservationPostKey(normalized.AlarmType, normalized.ChannelID, postID),
			ReportAlarmType:         normalized.AlarmType,
			ReportChannelID:         normalized.ChannelID,
			ReportPostID:            postID,
			ReportActualPublishedAt: cloneCommunityShortsSendCountTime(normalized.ActualPublishedAt),
			ReportDetectedAt:        cloneCommunityShortsSendCountTime(normalized.DetectedAt),
			ReportAlarmSentAt:       alarmSentAt,
		}
		normalizedRows = append(normalizedRows, row)

		summary.PostStateCount++
		switch row.SendState {
		case CommunityShortsPerPostSendStateSent:
			summary.SentPostCount++
		case CommunityShortsPerPostSendStateAttemptedWithoutSuccess:
			summary.AttemptedWithoutSuccessPostCount++
		default:
			summary.NotSentPostCount++
		}
		if row.DuplicateSuccessCount > 0 {
			summary.DuplicateSuccessPostCount++
		}
		if row.FailedAttemptCount > 0 {
			summary.FailedAttemptPostCount++
		}
		switch row.ReportAlarmType {
		case domain.AlarmTypeCommunity:
			summary.CommunityPostCount++
		case domain.AlarmTypeShorts:
			summary.ShortsPostCount++
		}
		if observedAt := resolveCommunityShortsSendStateObservedAt(row); observedAt != nil {
			if summary.EarliestObservedAt == nil || observedAt.Before(summary.EarliestObservedAt.UTC()) {
				summary.EarliestObservedAt = cloneCommunityShortsSendCountTime(observedAt)
			}
			if summary.LatestObservedAt == nil || observedAt.After(summary.LatestObservedAt.UTC()) {
				summary.LatestObservedAt = cloneCommunityShortsSendCountTime(observedAt)
			}
		}
		if alarmSentAt != nil {
			if summary.EarliestAlarmSentAt == nil || alarmSentAt.Before(summary.EarliestAlarmSentAt.UTC()) {
				summary.EarliestAlarmSentAt = cloneCommunityShortsSendCountTime(alarmSentAt)
			}
			if summary.LatestAlarmSentAt == nil || alarmSentAt.After(summary.LatestAlarmSentAt.UTC()) {
				summary.LatestAlarmSentAt = cloneCommunityShortsSendCountTime(alarmSentAt)
			}
		}
	}

	sort.SliceStable(normalizedRows, func(i, j int) bool {
		left := communityShortsSendStateSortTime(normalizedRows[i])
		right := communityShortsSendStateSortTime(normalizedRows[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		if normalizedRows[i].ReportAlarmType != normalizedRows[j].ReportAlarmType {
			return normalizedRows[i].ReportAlarmType < normalizedRows[j].ReportAlarmType
		}
		if normalizedRows[i].ReportChannelID != normalizedRows[j].ReportChannelID {
			return normalizedRows[i].ReportChannelID < normalizedRows[j].ReportChannelID
		}
		if normalizedRows[i].ReportPostID != normalizedRows[j].ReportPostID {
			return normalizedRows[i].ReportPostID < normalizedRows[j].ReportPostID
		}
		return normalizedRows[i].ContentID < normalizedRows[j].ContentID
	})

	return CommunityShortsSendStateReport{
		GeneratedAt: generatedAt,
		Query:       query,
		Summary:     summary,
		Rows:        normalizedRows,
	}
}

func RenderCommunityShortsSendStateMarkdown(report CommunityShortsSendStateReport) string {
	var builder strings.Builder

	builder.WriteString("# YouTube Community/Shorts Send State Report\n\n")
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
	builder.WriteString("- finalized: `")
	builder.WriteString(fmt.Sprintf("%t", report.Query.Finalized))
	builder.WriteString("`\n")
	builder.WriteString("- summary: post_states=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.PostStateCount))
	builder.WriteString("`, sent_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.SentPostCount))
	builder.WriteString("`, attempted_without_success_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.AttemptedWithoutSuccessPostCount))
	builder.WriteString("`, not_sent_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.NotSentPostCount))
	builder.WriteString("`, duplicate_success_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.DuplicateSuccessPostCount))
	builder.WriteString("`, failed_attempt_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.FailedAttemptPostCount))
	builder.WriteString("`, community_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.CommunityPostCount))
	builder.WriteString("`, shorts_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.ShortsPostCount))
	builder.WriteString("`, earliest_observed_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.EarliestObservedAt))
	builder.WriteString("`, latest_observed_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.LatestObservedAt))
	builder.WriteString("`, earliest_alarm_sent_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.EarliestAlarmSentAt))
	builder.WriteString("`, latest_alarm_sent_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.LatestAlarmSentAt))
	builder.WriteString("`\n")

	if len(report.Rows) == 0 {
		builder.WriteString("\n조회된 community/shorts per-post send state row가 없습니다.\n")
		return builder.String()
	}

	builder.WriteString("\n| send_state | alarm_type | channel_id | post_key | post_id | content_id | actual_published_at | detected_at | alarm_sent_at | outbox_count | success_send_count | success_room_count | duplicate_success_count | failed_attempt_count | first_event_at | last_event_at |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | --- | --- |\n")
	for i := range report.Rows {
		row := report.Rows[i]
		builder.WriteString("| `")
		builder.WriteString(string(row.SendState))
		builder.WriteString("` | `")
		builder.WriteString(string(row.ReportAlarmType))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.ReportChannelID))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.PostKey))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.ReportPostID))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.ContentID))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ReportActualPublishedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ReportDetectedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ReportAlarmSentAt))
		builder.WriteString("` | ")
		builder.WriteString(fmt.Sprintf("%d", row.OutboxCount))
		builder.WriteString(" | ")
		builder.WriteString(fmt.Sprintf("%d", row.SuccessSendCount))
		builder.WriteString(" | ")
		builder.WriteString(fmt.Sprintf("%d", row.SuccessRoomCount))
		builder.WriteString(" | ")
		builder.WriteString(fmt.Sprintf("%d", row.DuplicateSuccessCount))
		builder.WriteString(" | ")
		builder.WriteString(fmt.Sprintf("%d", row.FailedAttemptCount))
		builder.WriteString(" | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.FirstEventAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.LastEventAt))
		builder.WriteString("` |\n")
	}

	return builder.String()
}

func normalizeCommunityShortsSendStateCollectOptions(
	options CommunityShortsSendStateCollectOptions,
) (CommunityShortsSendStateQuery, error) {
	runtimeName := strings.TrimSpace(options.ObservationRuntimeName)
	cutoverAt := cloneCommunityShortsSendCountTime(options.ObservationBigBangCutoverAt)
	if runtimeName == "" || cutoverAt == nil || cutoverAt.IsZero() {
		return CommunityShortsSendStateQuery{}, fmt.Errorf("observation runtime name and big-bang cutover at are required")
	}

	return CommunityShortsSendStateQuery{
		ObservationRuntimeName:      runtimeName,
		ObservationBigBangCutoverAt: cutoverAt,
	}, nil
}

func normalizeCommunityShortsSendStateQuery(query CommunityShortsSendStateQuery) CommunityShortsSendStateQuery {
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = cloneCommunityShortsSendCountTime(query.ObservationBigBangCutoverAt)
	query.WindowStart = cloneCommunityShortsSendCountTime(query.WindowStart)
	query.WindowEnd = cloneCommunityShortsSendCountTime(query.WindowEnd)
	return query
}

func resolveCommunityShortsPerPostSendState(row outbox.PostSendCount) CommunityShortsPerPostSendState {
	if hasCommunityShortsSendStateSuccess(row) {
		return CommunityShortsPerPostSendStateSent
	}
	if hasCommunityShortsSendStateAttempt(row) {
		return CommunityShortsPerPostSendStateAttemptedWithoutSuccess
	}
	return CommunityShortsPerPostSendStateNotSent
}

func hasCommunityShortsSendStateSuccess(row outbox.PostSendCount) bool {
	return row.SuccessSendCount > 0 || row.AlarmSentAt != nil || row.FirstSuccessAt != nil || row.LastSuccessAt != nil
}

func hasCommunityShortsSendStateAttempt(row outbox.PostSendCount) bool {
	return row.OutboxCount > 0 || row.FailedAttemptCount > 0 || row.FirstEventAt != nil || row.LastEventAt != nil
}

func resolveCommunityShortsSendStatePostID(row outbox.PostSendCount) string {
	if strings.TrimSpace(row.PostID) != "" {
		return strings.TrimSpace(row.PostID)
	}
	return strings.TrimSpace(row.ContentID)
}

func resolveCommunityShortsSendStateAlarmSentAt(row outbox.PostSendCount) *time.Time {
	for _, candidate := range []*time.Time{row.AlarmSentAt, row.FirstSuccessAt, row.LastSuccessAt} {
		if candidate != nil {
			return cloneCommunityShortsSendCountTime(candidate)
		}
	}
	return nil
}

func resolveCommunityShortsSendStateObservedAt(row CommunityShortsSendStateRow) *time.Time {
	for _, candidate := range []*time.Time{row.ReportActualPublishedAt, row.ReportDetectedAt, row.LastEventAt, row.ReportAlarmSentAt} {
		if candidate != nil {
			return cloneCommunityShortsSendCountTime(candidate)
		}
	}
	return nil
}

func communityShortsSendStateSortTime(row CommunityShortsSendStateRow) time.Time {
	if observedAt := resolveCommunityShortsSendStateObservedAt(row); observedAt != nil {
		return observedAt.UTC()
	}
	return time.Time{}
}
