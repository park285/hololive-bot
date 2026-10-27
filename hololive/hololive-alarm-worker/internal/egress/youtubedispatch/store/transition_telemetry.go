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
	"github.com/kapu/hololive-shared/pkg/domain"
)

// DeliveryMode는 발송 방식(per_room/grouped)이다. TransitionStore가 lifecycle 전이와 함께 시도를 기록한다.
// 기록 실패는 전이도 rollback한다. 이미 발송한 CompleteSent가 실패하면 SENDING을 유지하고
// stale sweep이 QUARANTINED로 격리해 재발송 대신 결과 불명을 드러낸다.
type DeliveryMode string

const (
	DeliveryModePerRoom DeliveryMode = "per_room"
	DeliveryModeGrouped DeliveryMode = "grouped"
	// 이 값은 stale SENDING을 격리하는 sweep이 결과 불명 시도를 기록할 때만 쓴다. SENDING 행에는 발송
	// 방식이 남지 않아 per_room·grouped를 복원할 수 없다.
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
	attemptResultSuccess = "success"
	attemptResultFailure = "failure"
	// 결과 불명(outcome_unknown)은 성공이 아닌 시도로 기록한다.
	attemptResultOutcomeUnknown = "outcome_unknown"
)

// staleSendingOutcomeUnknownReason은 stale sweep이 격리한 시도의 failure_reason이다(lifecycle Reason 어휘).
const staleSendingOutcomeUnknownReason lifecycle.Reason = "stale_sending_outcome_unknown"

// 시도 하나는 logical group owner의 provider 시도다. 따라서 follower·fulfilled·전파 전이는 기록하지 않는다.
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

// recordAttemptTelemetry는 전이 트랜잭션 tx 안에서 owner 시도를 telemetry 버퍼에 한 행씩 넣는다.
// 순번(attempt_ordinal)은 두 값 중 큰 쪽이다. 하나는 버퍼에 남은 delivery별 최대 순번 다음 값이라 revive가 attempt_count를 0으로
// 되돌려도 버퍼에 남은 행과 겹치지 않는다. 다른 하나는 claim 시점 attempt_count + 1(attempt started 로그와 같은 값)이라 telemetry
// processor가 retention(기본 24h)이 지난 방출 행을 지운 뒤에도 순번이 되돌아가지 않는다. 버퍼 행이 지워지고 revive로
// attempt_count도 0이 된 delivery만 1부터 다시 세며, 이 한계는 runbook YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md에 적었다.
// 같은 트랜잭션이 delivery 행을 CAS로 갱신해 잠그므로 같은 delivery의 다른 전이와 순번을 다투지 않는다.
func recordAttemptTelemetry(
	ctx context.Context,
	tx dbx.Querier,
	mode DeliveryMode,
	attempts []deliveryAttempt,
	at time.Time,
) error {
	rows := make([]domain.YouTubeNotificationDeliveryTelemetry, 0, len(attempts))
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
) (domain.YouTubeNotificationDeliveryTelemetry, bool) {
	owner := attempt.group.ownerAfter
	if !telemetry.IsCommunityShortsDeliveryAuditKind(owner.Kind) {
		return domain.YouTubeNotificationDeliveryTelemetry{}, false
	}

	outbox := owner.domainOutbox()
	finishedAt := at.UTC()

	failureReason := ""

	if attempt.result != attemptResultSuccess {
		failureReason = deliverysql.TruncateString(string(attempt.reason), 100)
	}

	return domain.YouTubeNotificationDeliveryTelemetry{
		DeliveryID: owner.ID,
		OutboxID:   owner.OutboxID,
		ChannelID:  owner.ChannelID,
		ContentID:  strings.TrimSpace(owner.ContentID),
		// logical key는 content_id와 payload canonical_post_id가 일치함을 검증한 정본 식별자다.
		PostID:        attempt.group.key.LogicalID,
		RoomID:        owner.RoomID,
		AlarmType:     owner.Kind.ToAlarmType(),
		DedupeKey:     telemetry.DedupeKeyLogValue(&outbox),
		DeliveryPath:  telemetry.CommunityShortsDeliveryPath,
		DeliveryMode:  string(mode),
		SendResult:    attempt.result,
		FailureReason: failureReason,
		// claim 시점 attempt_count + 1은 순번의 하한이다. assignAttemptOrdinals가 버퍼의 최대 순번과 비교해 올린다.
		AttemptOrdinal:    max(attempt.group.ownerBefore.AttemptCount, 0) + 1,
		AttemptStartedAt:  cloneTimePtr(owner.LockedAt),
		AttemptFinishedAt: &finishedAt,
		EventAt:           finishedAt,
		NextAttemptAt:     finishedAt,
	}, true
}

func assignAttemptOrdinals(ctx context.Context, tx dbx.Querier, rows []domain.YouTubeNotificationDeliveryTelemetry) error {
	deliveryIDs := make([]int64, 0, len(rows))
	for i := range rows {
		deliveryIDs = append(deliveryIDs, rows[i].DeliveryID)
	}

	deliveryIDs = uniqueSortedInt64s(deliveryIDs)
	if len(deliveryIDs) != len(rows) {
		return fmt.Errorf("assign attempt ordinals: %d attempts share %d deliveries", len(rows), len(deliveryIDs))
	}

	queryRows, err := tx.Query(ctx, mustSQL("transition_telemetry_last_attempt.sql"), deliveryIDs)
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
