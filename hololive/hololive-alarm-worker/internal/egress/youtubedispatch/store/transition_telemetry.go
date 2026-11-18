package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/deliverysql"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/telemetry"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type DeliveryMode string

const (
	DeliveryModePerRoom    DeliveryMode = "per_room"
	DeliveryModeGrouped    DeliveryMode = "grouped"
	deliveryModeStaleSweep DeliveryMode = "stale_sweep"
)

func (m DeliveryMode) validateCallerMode() error {
	switch m {
	case DeliveryModePerRoom, DeliveryModeGrouped:
		return nil
	case deliveryModeStaleSweep:
		return fmt.Errorf("delivery mode %q is reserved for the stale sending sweep", m)
	default:
		return fmt.Errorf("delivery mode %q is invalid", m)
	}
}

const (
	attemptResultSuccess        = "success"
	attemptResultFailure        = "failure"
	attemptResultOutcomeUnknown = "outcome_unknown"
)

const staleSendingOutcomeUnknownReason lifecycle.Reason = "stale_sending_outcome_unknown"

type deliveryAttempt struct {
	group  startedLogicalGroup
	result string
	reason lifecycle.Reason
}

func ownerAttempts(groups []startedLogicalGroup, result string, reason lifecycle.Reason) []deliveryAttempt {
	attempts := make([]deliveryAttempt, 0, len(groups))
	for i := range groups {
		attempts = append(attempts, deliveryAttempt{group: groups[i], result: result, reason: reason})
	}

	return attempts
}

func recordAttemptTelemetry(
	ctx context.Context,
	tx dbx.Querier,
	mode DeliveryMode,
	attempts []deliveryAttempt,
	at time.Time,
) error {
	rows := make([]telemetry.YouTubeNotificationDeliveryTelemetry, 0, len(attempts))
	for i := range attempts {
		if row, ok := attemptTelemetryRow(mode, attempts[i], at); ok {
			rows = append(rows, row)
		}
	}

	if len(rows) == 0 {
		return nil
	}

	if err := assignAttemptOrdinals(ctx, tx, rows); err != nil {
		return fmt.Errorf("record attempt telemetry: %w", err)
	}

	repository, err := telemetry.NewRepository(tx)
	if err != nil {
		return fmt.Errorf("record attempt telemetry: new telemetry repository: %w", err)
	}

	if err := repository.Enqueue(ctx, rows); err != nil {
		return fmt.Errorf("record attempt telemetry: enqueue: %w", err)
	}

	return nil
}

func attemptTelemetryRow(
	mode DeliveryMode,
	attempt deliveryAttempt,
	at time.Time,
) (telemetry.YouTubeNotificationDeliveryTelemetry, bool) {
	owner := attempt.group.ownerAfter
	if !telemetry.IsCommunityShortsDeliveryAuditKind(owner.Kind) {
		return telemetry.YouTubeNotificationDeliveryTelemetry{}, false
	}

	outbox := owner.domainOutbox()
	finishedAt := at.UTC()

	failureReason := ""

	if attempt.result != attemptResultSuccess {
		failureReason = deliverysql.TruncateString(string(attempt.reason), 100)
	}

	return telemetry.YouTubeNotificationDeliveryTelemetry{
		DeliveryID:        owner.ID,
		OutboxID:          owner.OutboxID,
		ChannelID:         owner.ChannelID,
		ContentID:         strings.TrimSpace(owner.ContentID),
		PostID:            attempt.group.key.LogicalID,
		RoomID:            owner.RoomID,
		AlarmType:         owner.Kind.ToAlarmType(),
		DedupeKey:         telemetry.DedupeKeyLogValue(&outbox),
		DeliveryPath:      telemetry.CommunityShortsDeliveryPath,
		DeliveryMode:      string(mode),
		SendResult:        attempt.result,
		FailureReason:     failureReason,
		AttemptOrdinal:    max(attempt.group.ownerBefore.AttemptCount, 0) + 1,
		AttemptStartedAt:  cloneTimePtr(owner.LockedAt),
		AttemptFinishedAt: &finishedAt,
		EventAt:           finishedAt,
		NextAttemptAt:     finishedAt,
	}, true
}

func assignAttemptOrdinals(ctx context.Context, tx dbx.Querier, rows []telemetry.YouTubeNotificationDeliveryTelemetry) error {
	deliveryIDs := make([]int64, 0, len(rows))
	for i := range rows {
		deliveryIDs = append(deliveryIDs, rows[i].DeliveryID)
	}

	deliveryIDs = uniqueSortedInt64s(deliveryIDs)
	if len(deliveryIDs) != len(rows) {
		return fmt.Errorf("assign attempt ordinals: %d attempts share %d deliveries", len(rows), len(deliveryIDs))
	}

	queryRows, err := tx.Query(ctx, transitionTelemetryLastAttemptSQL, deliveryIDs)
	if err != nil {
		return fmt.Errorf("assign attempt ordinals: load last ordinals: %w", err)
	}
	defer queryRows.Close()

	lastByDelivery := make(map[int64]int, len(deliveryIDs))

	for queryRows.Next() {
		var (
			deliveryID int64
			last       int
		)

		if err := queryRows.Scan(&deliveryID, &last); err != nil {
			return fmt.Errorf("assign attempt ordinals: scan last ordinal: %w", err)
		}

		lastByDelivery[deliveryID] = last
	}

	if err := queryRows.Err(); err != nil {
		return fmt.Errorf("assign attempt ordinals: iterate last ordinals: %w", err)
	}

	for i := range rows {
		rows[i].AttemptOrdinal = max(rows[i].AttemptOrdinal, lastByDelivery[rows[i].DeliveryID]+1)
	}

	return nil
}
