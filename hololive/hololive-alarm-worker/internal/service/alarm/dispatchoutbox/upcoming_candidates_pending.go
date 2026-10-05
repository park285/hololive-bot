package dispatchoutbox

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/pgxutil"
)

type upcomingCandidateFacts struct {
	PayloadHash        string
	EventPayloadHash   *string
	DeliveryStatus     *Status
	ScheduledAt        time.Time
	SelectedAt         time.Time
	LiveStatus         *string
	IsPremiere         *bool
	StatusObservedAt   *time.Time
	ScheduleObservedAt *time.Time
	LiveScheduledAt    *time.Time
}

// 원장 증거가 만료·방송 관측보다 우선한다. 불명 전송을 재발행 가능한 후보로 되돌리지 않는다.
func (f upcomingCandidateFacts) outcome(now time.Time) string {
	if f.EventPayloadHash != nil && *f.EventPayloadHash != f.PayloadHash {
		return "rejected_collision"
	}

	if f.DeliveryStatus != nil {
		return upcomingDeliveryOutcome(*f.DeliveryStatus)
	}

	if !f.ScheduledAt.After(now) {
		return "expired"
	}

	if f.IsPremiere != nil && *f.IsPremiere {
		return "stream_ended"
	}

	if f.StatusObservedAt != nil && f.StatusObservedAt.After(f.SelectedAt) && f.LiveStatus != nil &&
		(*f.LiveStatus == "LIVE" || *f.LiveStatus == "ENDED") {
		return "stream_ended"
	}

	if f.ScheduleObservedAt != nil && f.ScheduleObservedAt.After(f.SelectedAt) && f.LiveScheduledAt != nil &&
		!f.LiveScheduledAt.Equal(f.ScheduledAt) {
		return "schedule_changed"
	}

	return "pending"
}

func upcomingDeliveryOutcome(status Status) string {
	switch status {
	case StatusPending, StatusLeased, StatusRetry, StatusSending, StatusSent:
		return "accepted"
	case StatusDLQ, StatusQuarantined, StatusCancelled:
		return "rejected_terminal"
	default:
		return "rejected_terminal"
	}
}

// Pending은 잠근 후보의 원장·관측 snapshot으로 판정하고 같은 트랜잭션에서 일괄 저장한다.
// Accepted는 발행 수용을 뜻하며 provider 성공이나 재발송 허가를 뜻하지 않는다.
func (s *UpcomingCandidates) Pending(ctx context.Context, now time.Time) (_ []UpcomingCandidate, resultErr error) {
	beginner, ok := s.db.(interface {
		Begin(context.Context) (pgx.Tx, error)
	})
	if !ok {
		return nil, errors.New("load upcoming candidates: database does not support transactions")
	}

	tx, err := beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("load upcoming candidates: begin: %w", err)
	}

	defer func() {
		if rollbackErr := pgxutil.Rollback(ctx, tx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			resultErr = errors.Join(resultErr, fmt.Errorf("load upcoming candidates: rollback: %w", rollbackErr))
		}
	}()

	pending, err := evaluateUpcomingCandidates(ctx, tx, now)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("load upcoming candidates: commit: %w", err)
	}

	// 저장 결과는 기존 단문 SQL처럼 해독 실패와 독립적으로 확정한다.
	// JSON 해독 중에는 row lock을 유지하지 않아 다른 evaluator·Finish를 막지 않는다.
	candidates := make([]UpcomingCandidate, 0, len(pending))
	for i := range pending {
		candidate := pending[i].candidate
		if err := jsonv2.Unmarshal(pending[i].payload, &candidate.Notification); err != nil {
			return nil, fmt.Errorf("decode upcoming candidate: %w", err)
		}

		candidates = append(candidates, candidate)
	}

	return candidates, nil
}

type pendingCandidatePayload struct {
	candidate UpcomingCandidate
	payload   []byte
}

func evaluateUpcomingCandidates(ctx context.Context, tx pgx.Tx, now time.Time) ([]pendingCandidatePayload, error) {
	rows, err := tx.Query(ctx, mustSQL("upcoming_candidates_pending.sql"), UpcomingCandidateLimit)
	if err != nil {
		return nil, fmt.Errorf("load upcoming candidate facts: %w", err)
	}
	defer rows.Close()

	candidates := make([]pendingCandidatePayload, 0)
	keysByOutcome := make(map[string][]string)

	for rows.Next() {
		var (
			c   UpcomingCandidate
			f   upcomingCandidateFacts
			raw []byte
		)

		if err := rows.Scan(&c.DedupeKey, &c.ChannelID, &raw,
			&f.PayloadHash, &f.EventPayloadHash, &f.DeliveryStatus, &f.ScheduledAt, &f.SelectedAt,
			&f.LiveStatus, &f.IsPremiere, &f.StatusObservedAt, &f.ScheduleObservedAt, &f.LiveScheduledAt); err != nil {
			return nil, fmt.Errorf("scan upcoming candidate facts: %w", err)
		}

		c.Outcome = f.outcome(now)
		keysByOutcome[c.Outcome] = append(keysByOutcome[c.Outcome], c.DedupeKey)

		if c.Outcome != "pending" {
			continue
		}

		candidates = append(candidates, pendingCandidatePayload{candidate: c, payload: raw})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upcoming candidate facts: %w", err)
	}

	// 같은 outcome의 시각·상태를 한 번만 전송하고 최대 7개 UPDATE를 한 batch로 보낸다.
	statements := make([]dbx.Statement, 0, len(keysByOutcome))
	for outcome, keys := range keysByOutcome {
		var terminalAt *time.Time

		if outcome != "pending" {
			terminalAt = &now
		}

		statements = append(statements, dbx.Statement{
			SQL:       mustSQL("upcoming_candidates_apply.sql"),
			Args:      []any{keys, outcome, terminalAt, now},
			Operation: "apply upcoming candidate outcomes",
		})
	}

	if err := dbx.ExecStatements(ctx, tx, statements); err != nil {
		return nil, fmt.Errorf("apply upcoming candidate outcomes: %w", err)
	}

	return candidates, nil
}
