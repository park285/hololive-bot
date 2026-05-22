package reports

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
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
	ctx, logger, now, query, err := prepareCommunityShortsSendStateCollectInputs(ctx, cfg, logger, now, options)
	if err != nil {
		return CommunityShortsSendStateReport{}, err
	}

	session, cleanupDB, err := openCommunityShortsSendStateSession(ctx, cfg, logger)
	if err != nil {
		return CommunityShortsSendStateReport{}, err
	}
	defer cleanupCommunityShortsSendStateSession(cleanupDB)

	state, err := resolveCommunityShortsSendStateObservationState(ctx, session, query, now)
	if err != nil {
		return CommunityShortsSendStateReport{}, err
	}

	query.WindowStart = cloneCommunityShortsSendCountTime(&state.Window.ObservationStartedAt)
	query.WindowEnd = cloneCommunityShortsSendCountTime(&state.EffectiveWindowEnd)
	query.Finalized = state.Finalized

	rows, err := listCommunityShortsSendStateRows(ctx, session, query, state)
	if err != nil {
		return CommunityShortsSendStateReport{}, err
	}

	return BuildCommunityShortsSendStateReport(rows, query, now), nil
}

func prepareCommunityShortsSendStateCollectInputs(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CommunityShortsSendStateCollectOptions,
) (context.Context, *slog.Logger, time.Time, CommunityShortsSendStateQuery, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return nil, nil, time.Time{}, CommunityShortsSendStateQuery{}, fmt.Errorf("collect community shorts send state report: config is nil")
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
		return nil, nil, time.Time{}, CommunityShortsSendStateQuery{}, fmt.Errorf("collect community shorts send state report: %w", err)
	}

	return ctx, logger, now, query, nil
}

func cleanupCommunityShortsSendStateSession(cleanupDB func()) {
	if cleanupDB != nil {
		cleanupDB()
	}
}

func openCommunityShortsSendStateSession(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
) (*communityShortsOpsSession, func(), error) {
	session, cleanupDB, err := openCommunityShortsOpsSession(ctx, cfg, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("collect community shorts send state report: %w", err)
	}
	if session == nil {
		if cleanupDB != nil {
			cleanupDB()
		}
		return nil, nil, fmt.Errorf("collect community shorts send state report: session is nil")
	}
	return session, cleanupDB, nil
}

func resolveCommunityShortsSendStateObservationState(
	ctx context.Context,
	session *communityShortsOpsSession,
	query CommunityShortsSendStateQuery,
	now time.Time,
) (communityShortsObservationQueryState, error) {
	state, err := resolveCommunityShortsObservationQueryState(
		ctx,
		session.trackingRepository,
		query.ObservationRuntimeName,
		*query.ObservationBigBangCutoverAt,
		now,
	)
	if err != nil {
		return communityShortsObservationQueryState{}, fmt.Errorf("collect community shorts send state report: find observation window: %w", err)
	}
	if state.Window == nil {
		return communityShortsObservationQueryState{}, fmt.Errorf(
			"collect community shorts send state report: observation window not found: runtime=%s cutover=%s",
			query.ObservationRuntimeName,
			formatCommunityShortsSendCountTime(*query.ObservationBigBangCutoverAt),
		)
	}
	return state, nil
}

func listCommunityShortsSendStateRows(
	ctx context.Context,
	session *communityShortsOpsSession,
	query CommunityShortsSendStateQuery,
	state communityShortsObservationQueryState,
) ([]outbox.PostSendCount, error) {
	if state.Finalized {
		rows, err := session.telemetryRepository.ListPostSendCountsByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, state.Window.BigBangCutoverAt)
		if err != nil {
			return nil, fmt.Errorf("collect community shorts send state report: list finalized observation-window send states: %w", err)
		}
		return rows, nil
	}

	rows, err := session.telemetryRepository.ListPostSendCountsWithinObservationWindow(ctx, state.Window.ObservationStartedAt, state.EffectiveWindowEnd, state.EffectiveWindowEnd)
	if err != nil {
		return nil, fmt.Errorf("collect community shorts send state report: list active observation-window send states: %w", err)
	}
	return rows, nil
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
