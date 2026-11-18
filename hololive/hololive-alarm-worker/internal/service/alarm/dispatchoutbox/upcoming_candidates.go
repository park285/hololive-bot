package dispatchoutbox

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const UpcomingCandidateLimit = 1000

type UpcomingCandidate struct {
	DedupeKey    string
	ChannelID    string
	Notification domain.AlarmNotification
	Outcome      string
}

type UpcomingCandidateStore interface {
	Stage(context.Context, string, time.Time, []*domain.AlarmNotification) error
	Pending(context.Context, time.Time) ([]UpcomingCandidate, error)
	Finish(context.Context, map[string][]string, time.Time) error
}

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

func (s *UpcomingCandidates) Stage(ctx context.Context, channelID string, now time.Time, notifications []*domain.AlarmNotification) error {
	if s.db == nil {
		return errors.New("stage upcoming candidates: database is nil")
	}

	inputs := make([]upcomingCandidateInput, 0, len(notifications))
	for _, n := range notifications {
		if n == nil || n.Stream == nil || !n.Stream.IsUpcoming() || n.Stream.StartScheduled == nil {
			continue
		}

		envelope := AlarmQueueEnvelope{Notification: *n, Version: 1}

		event, delivery, err := buildLedgerRows(&envelope)
		if err != nil {
			return fmt.Errorf("stage upcoming candidates: build ledger identity: %w", err)
		}

		if event.ChannelID != channelID {
			return errors.New("stage upcoming candidates: channel identity mismatch")
		}

		inputs = append(inputs, upcomingCandidateInput{DedupeKey: delivery.DedupeKey, EventKey: event.EventKey, PayloadHash: event.PayloadHash, StreamID: event.StreamID, RoomID: n.RoomID, ScheduledAt: *n.Stream.StartScheduled, Notification: n})
	}

	if len(inputs) == 0 {
		return nil
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

	if _, err := db.Exec(ctx, upcomingCandidatesStageSQL, string(raw), channelID, now); err != nil {
		return fmt.Errorf("stage upcoming candidates: insert candidates: %w", err)
	}

	return nil
}

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
		if err := pgxdb.Rollback(ctx, tx); err != nil {
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

func (s *UpcomingCandidates) Finish(ctx context.Context, keysByOutcome map[string][]string, now time.Time) error {
	for outcome, keys := range keysByOutcome {
		if len(keys) == 0 {
			continue
		}

		if _, err := s.db.Exec(ctx, upcomingCandidatesApplySQL, keys, outcome, &now, now); err != nil {
			return fmt.Errorf("finish upcoming candidates: outcome %s: %w", outcome, err)
		}
	}

	return nil
}

func (s *UpcomingCandidates) Cleanup(ctx context.Context, retentionDays, limit int) (int64, error) {
	if retentionDays <= 0 || limit <= 0 {
		return 0, errors.New("cleanup upcoming candidates: retention and limit must be positive")
	}

	tag, err := s.db.Exec(ctx, upcomingCandidatesCleanupSQL, retentionDays, limit)
	if err != nil {
		return 0, fmt.Errorf("cleanup upcoming candidates: %w", err)
	}

	return tag.RowsAffected(), nil
}
