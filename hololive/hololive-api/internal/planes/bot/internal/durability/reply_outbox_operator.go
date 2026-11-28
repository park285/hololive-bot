package durability

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const ManualReplayCutoff = 144 * time.Hour

type ManualReviewOutcome string

const (
	ManualReviewReplayed                ManualReviewOutcome = "replayed"
	ManualReviewDiscarded               ManualReviewOutcome = "discarded"
	ManualReviewCutoffExpired           ManualReviewOutcome = "cutoff_expired"
	ManualReviewInvalidOperatorMetadata ManualReviewOutcome = "invalid_operator_metadata"
	ManualReviewInvalidIrisState        ManualReviewOutcome = "invalid_iris_state"
	ManualReviewNotManualReview         ManualReviewOutcome = "not_manual_review"
	ManualReviewNotFound                ManualReviewOutcome = "not_found"
)

const operatorReasonByteLimit = 256

var (
	operatorActorPattern  = regexp.MustCompile(`^[A-Za-z0-9._:@-]{1,64}$`)
	discardObservedStates = map[string]struct{}{
		"queued": {}, "preparing": {}, "prepared": {}, "sending": {},
		"handoff_completed": {}, "failed": {}, "outcome_unknown": {}, "not_found": {},
	}
)

type manualReviewTarget struct {
	status         string
	createdAtMS    int64
	replayGrants   int32
	found          bool
	inManualReview bool
}

func (s *Store) GrantManualReplay(ctx context.Context, outboxID int64, actor, reason string) (ManualReviewOutcome, error) {
	if err := ensureStore(s); err != nil {
		return "", err
	}

	actor, reason = strings.Trim(actor, " "), strings.Trim(reason, " ")

	var outcome ManualReviewOutcome

	err := s.write(ctx, func(tx *sql.Tx, now int64) error {
		target, err := loadManualReviewTarget(ctx, tx, outboxID)
		if err != nil {
			return err
		}

		switch {
		case !target.found:
			outcome = ManualReviewNotFound
		case !target.inManualReview:
			outcome = ManualReviewNotManualReview
		case now >= target.createdAtMS+ManualReplayCutoff.Milliseconds():
			outcome = ManualReviewCutoffExpired
		case !validOperatorMetadata(actor, reason):
			outcome = ManualReviewInvalidOperatorMetadata
		default:
			outcome = ManualReviewReplayed

			return grantManualReplayTx(ctx, tx, outboxID, target.replayGrants+1, actor, reason, now)
		}

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("grant manual replay for reply outbox row %d: %w", outboxID, err)
	}

	return outcome, nil
}

func grantManualReplayTx(ctx context.Context, tx *sql.Tx, outboxID int64, grant int32, actor, reason string, now int64) error {
	if _, err := tx.ExecContext(ctx, replyOutboxReplayGrantAuditSQL, outboxID, grant, actor, reason, now); err != nil {
		return fmt.Errorf("append replay grant audit: %w", err)
	}

	result, err := tx.ExecContext(ctx, replyOutboxReplayGrantSQL, outboxID, grant, now)
	if err != nil {
		return fmt.Errorf("requeue granted reply: %w", err)
	}

	if affected, err := rowsAffected(result); err != nil || affected != 1 {
		return errors.Join(errors.New("granted reply left manual review concurrently"), err)
	}

	return nil
}

func (s *Store) DiscardManualReview(ctx context.Context, outboxID int64, actor, reason, observedIrisState string) (ManualReviewOutcome, error) {
	if err := ensureStore(s); err != nil {
		return "", err
	}

	actor, reason, observedIrisState = strings.Trim(actor, " "), strings.Trim(reason, " "), strings.Trim(observedIrisState, " ")

	var outcome ManualReviewOutcome

	err := s.write(ctx, func(tx *sql.Tx, now int64) error {
		target, err := loadManualReviewTarget(ctx, tx, outboxID)
		if err != nil {
			return err
		}

		_, knownState := discardObservedStates[observedIrisState]

		switch {
		case !target.found:
			outcome = ManualReviewNotFound
		case !target.inManualReview:
			outcome = ManualReviewNotManualReview
		case !validOperatorMetadata(actor, reason):
			outcome = ManualReviewInvalidOperatorMetadata
		case !knownState:
			outcome = ManualReviewInvalidIrisState
		default:
			outcome = ManualReviewDiscarded

			return discardManualReviewTx(ctx, tx, outboxID, observedIrisState, actor, reason, now)
		}

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("discard manual review reply outbox row %d: %w", outboxID, err)
	}

	return outcome, nil
}

func discardManualReviewTx(ctx context.Context, tx *sql.Tx, outboxID int64, observedIrisState, actor, reason string, now int64) error {
	if _, err := tx.ExecContext(ctx, replyOutboxDiscardAuditSQL, outboxID, observedIrisState, actor, reason, now); err != nil {
		return fmt.Errorf("append discard audit: %w", err)
	}

	result, err := tx.ExecContext(ctx, replyOutboxDiscardSQL, outboxID, now)
	if err != nil {
		return fmt.Errorf("discard reply: %w", err)
	}

	if affected, err := rowsAffected(result); err != nil || affected != 1 {
		return errors.Join(errors.New("discarded reply left manual review concurrently"), err)
	}

	return nil
}

func loadManualReviewTarget(ctx context.Context, tx *sql.Tx, outboxID int64) (manualReviewTarget, error) {
	var target manualReviewTarget

	err := tx.QueryRowContext(ctx, replyOutboxOperatorTargetSQL, outboxID).Scan(&target.status, &target.createdAtMS, &target.replayGrants)
	if errors.Is(err, sql.ErrNoRows) {
		return target, nil
	}

	if err != nil {
		return target, fmt.Errorf("load reply outbox row: %w", err)
	}

	target.found = true
	target.inManualReview = target.status == ReplyOutboxManualReview

	return target, nil
}

func validOperatorMetadata(actor, reason string) bool {
	if !operatorActorPattern.MatchString(actor) || reason == "" || len(reason) > operatorReasonByteLimit {
		return false
	}

	return !strings.ContainsFunc(reason, unicode.IsControl)
}

type ManualReviewRow struct {
	ID                   int64
	ClientRequestID      string
	IrisRequestID        string
	Attempts             int32
	OperatorReplayGrants int32
	CreatedAt            time.Time
	UpdatedAt            time.Time
	LastError            string
}

func (s *Store) ManualReviewRows(ctx context.Context, limit int) ([]ManualReviewRow, error) {
	if err := ensureStore(s); err != nil {
		return nil, err
	}

	if limit < 1 {
		return nil, errors.Join(ErrInvalidArgument, errors.New("manual review list limit must be positive"))
	}

	rows, err := s.read().QueryContext(ctx, replyOutboxManualReviewListSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("select manual review reply outbox rows: %w", err)
	}
	defer rows.Close()

	var listed []ManualReviewRow

	for rows.Next() {
		var (
			row                  ManualReviewRow
			createdMS, updatedMS int64
		)

		if err := rows.Scan(&row.ID, &row.ClientRequestID, &row.IrisRequestID, &row.Attempts, &row.OperatorReplayGrants,
			&createdMS, &updatedMS, &row.LastError); err != nil {
			return nil, fmt.Errorf("scan manual review reply outbox row: %w", err)
		}

		row.CreatedAt, row.UpdatedAt = time.UnixMilli(createdMS), time.UnixMilli(updatedMS)
		listed = append(listed, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read manual review reply outbox rows: %w", err)
	}

	return listed, nil
}
