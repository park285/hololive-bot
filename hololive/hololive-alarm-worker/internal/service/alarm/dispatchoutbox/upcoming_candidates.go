package dispatchoutbox

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/pgxutil"
)

// UpcomingCandidateLimit은 기존 publisher의 한 batch 상한을 복구에도 적용한다.
const UpcomingCandidateLimit = 1000

// UpcomingCandidate는 최초 선정한 category와 방별 payload를 보존한다.
type UpcomingCandidate struct {
	DedupeKey    string
	ChannelID    string
	Notification domain.AlarmNotification
	Outcome      string
}

// UpcomingCandidateStore는 발행 전 후보와 평가 완료를 같은 저장 경계에서 관리한다.
type UpcomingCandidateStore interface {
	Stage(context.Context, string, time.Time, []*domain.AlarmNotification) error
	Pending(context.Context, time.Time) ([]UpcomingCandidate, error)
	Finish(context.Context, string, string, time.Time) error
}

// UpcomingCandidates는 dispatch ledger와 같은 PostgreSQL의 선정 후보 저장소다.
type UpcomingCandidates struct{ db upcomingCandidateDB }

type upcomingCandidateDB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func NewUpcomingCandidates(db upcomingCandidateDB) *UpcomingCandidates {
	return &UpcomingCandidates{db: db}
}

type upcomingCandidateInput struct {
	DedupeKey    string                    `json:"dedupe_key"`
	EventKey     string                    `json:"event_key"`
	PayloadHash  string                    `json:"payload_hash"`
	StreamID     string                    `json:"stream_id"`
	RoomID       string                    `json:"room_id"`
	ScheduledAt  time.Time                 `json:"scheduled_at"`
	Notification *domain.AlarmNotification `json:"notification"`
}

// Stage는 저장 응답이 불명확하면 checkpoint 성공을 반환하지 않는다.
// Commit 뒤 응답 소실은 동일 key 재선정 또는 Pending의 ledger 조회로 복구한다.
func (s *UpcomingCandidates) Stage(ctx context.Context, channelID string, now time.Time, notifications []*domain.AlarmNotification) error {
	if s.db == nil {
		return errors.New("stage upcoming candidates: database is nil")
	}

	inputs := make([]upcomingCandidateInput, 0, len(notifications))
	for _, n := range notifications {
		if n == nil || n.Stream == nil || !n.Stream.IsUpcoming() || n.Stream.StartScheduled == nil {
			continue
		}

		envelope := domain.AlarmQueueEnvelope{Notification: *n, Version: 1}

		event, delivery, err := buildLedgerRows(&envelope)
		if err != nil {
			return fmt.Errorf("stage upcoming candidates: build ledger identity: %w", err)
		}

		if event.ChannelID != channelID {
			return errors.New("stage upcoming candidates: channel identity mismatch")
		}

		inputs = append(inputs, upcomingCandidateInput{DedupeKey: delivery.DedupeKey, EventKey: event.EventKey, PayloadHash: event.PayloadHash, StreamID: event.StreamID, RoomID: n.RoomID, ScheduledAt: *n.Stream.StartScheduled, Notification: n})
	}

	if len(inputs) > UpcomingCandidateLimit {
		return s.stageChunks(ctx, channelID, now, inputs)
	}

	return stageUpcomingInputs(ctx, s.db, channelID, now, inputs)
}

func stageUpcomingInputs(ctx context.Context, db upcomingCandidateDB, channelID string, now time.Time, inputs []upcomingCandidateInput) error {
	raw, err := jsonv2.Marshal(inputs)
	if err != nil {
		return fmt.Errorf("stage upcoming candidates: encode: %w", err)
	}

	if _, err := db.Exec(ctx, mustSQL("upcoming_candidates_stage.sql"), string(raw), channelID, now); err != nil {
		return fmt.Errorf("stage upcoming candidates: commit candidates and checkpoint: %w", err)
	}

	return nil
}

// 큰 fanout도 기존 publish batch 크기로 나누되 checkpoint는 모든 chunk와 함께 commit한다.
func (s *UpcomingCandidates) stageChunks(ctx context.Context, channelID string, now time.Time, inputs []upcomingCandidateInput) (resultErr error) {
	beginner, ok := s.db.(interface {
		Begin(context.Context) (pgx.Tx, error)
	})
	if !ok {
		return errors.New("stage upcoming candidate chunks: database does not support transactions")
	}

	tx, err := beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("stage upcoming candidate chunks: begin: %w", err)
	}

	defer func() {
		if err := pgxutil.Rollback(ctx, tx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			resultErr = errors.Join(resultErr, fmt.Errorf("stage upcoming candidate chunks: rollback: %w", err))
		}
	}()

	for start := 0; start < len(inputs); start += UpcomingCandidateLimit {
		if err := stageUpcomingInputs(ctx, tx, channelID, now, inputs[start:min(start+UpcomingCandidateLimit, len(inputs))]); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("stage upcoming candidate chunks: commit: %w", err)
	}

	return nil
}

// Pending은 commit된 delivery 상태·hash를 대조하고 현재 미해결 후보만 반환한다.
// Accepted는 발행 수용을 뜻하며 provider 성공이나 재발송 허가를 뜻하지 않는다.
func (s *UpcomingCandidates) Pending(ctx context.Context, now time.Time) ([]UpcomingCandidate, error) {
	rows, err := s.db.Query(ctx, mustSQL("upcoming_candidates_pending.sql"), now, UpcomingCandidateLimit)
	if err != nil {
		return nil, fmt.Errorf("load upcoming candidates: %w", err)
	}
	defer rows.Close()

	candidates := make([]UpcomingCandidate, 0)

	for rows.Next() {
		var c UpcomingCandidate

		var raw []byte

		if err := rows.Scan(&c.DedupeKey, &c.ChannelID, &raw, &c.Outcome); err != nil {
			return nil, fmt.Errorf("scan upcoming candidate: %w", err)
		}

		if err := jsonv2.Unmarshal(raw, &c.Notification); err != nil {
			return nil, fmt.Errorf("decode upcoming candidate: %w", err)
		}

		candidates = append(candidates, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upcoming candidates: %w", err)
	}

	return candidates, nil
}

// Finish는 pending만 종료하며 기존 delivery·claim·불명 전송 증거를 수정하지 않는다.
func (s *UpcomingCandidates) Finish(ctx context.Context, key, outcome string, now time.Time) error {
	if _, err := s.db.Exec(ctx, mustSQL("upcoming_candidates_finish.sql"), key, outcome, now); err != nil {
		return fmt.Errorf("finish upcoming candidate: %w", err)
	}

	return nil
}

// Cleanup은 기존 dispatch event 보존 기간·배치 상한으로 종료 후보만 제거한다.
func (s *UpcomingCandidates) Cleanup(ctx context.Context, retentionDays, limit int) (int64, error) {
	if retentionDays <= 0 || limit <= 0 {
		return 0, errors.New("cleanup upcoming candidates: retention and limit must be positive")
	}

	tag, err := s.db.Exec(ctx, mustSQL("upcoming_candidates_cleanup.sql"), retentionDays, limit)
	if err != nil {
		return 0, fmt.Errorf("cleanup upcoming candidates: %w", err)
	}

	return tag.RowsAffected(), nil
}
