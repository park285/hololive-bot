package eventgrade

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type dispatchPublisher interface {
	PublishDispatchBatch(context.Context, []dispatchoutbox.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error)
}

type pending struct {
	room    string
	payload domain.EventGradeDispatchPayload
}

func (r *Repository) Finalize(ctx context.Context, publisher dispatchPublisher) error {
	return r.FinalizeRegions(ctx, publisher, []string{"jp", "us", "as"})
}

func (r *Repository) FinalizeRegions(ctx context.Context, publisher dispatchPublisher, regions []string) error {
	var failures []error

	for _, region := range regions {
		if _, err := r.decideRegion(ctx, region); err != nil {
			failures = append(failures, err)
		}
	}

	return errors.Join(append(failures, r.publishDecisions(ctx, publisher))...)
}

func (r *Repository) decideRegion(ctx context.Context, region string) (bool, error) {
	decided := false

	err := dbx.InPgxTxOptions(ctx, r.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := pgxdb.Exec(ctx, tx, regionFenceSQL, region); err != nil {
			return fmt.Errorf("wait for event grade publications: %w", err)
		}

		var (
			published, applied int64
			inFlight           bool
		)

		if err := pgxdb.QueryRow(ctx, tx, regionStateSQL, region).Scan(&published, &applied, &inFlight); err != nil {
			return fmt.Errorf("read event grade region state: %w", err)
		}

		if applied < published || inFlight {
			return nil
		}

		if _, err := pgxdb.Exec(ctx, tx, expireSQL, region); err != nil {
			return fmt.Errorf("expire event grade decisions: %w", err)
		}

		if _, err := pgxdb.Exec(ctx, tx, decideSQL, []string{region}); err != nil {
			return fmt.Errorf("freeze event grade decisions: %w", err)
		}

		decided = true

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("decide event grade region %s: %w", region, err)
	}

	return decided, nil
}

func (r *Repository) publishDecisions(ctx context.Context, publisher dispatchPublisher) error {
	rows, err := pgxdb.Query(ctx, r.pool, pendingSQL, 100)
	if err != nil {
		return fmt.Errorf("load event grade decisions: %w", err)
	}

	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pending, error) {
		var p pending

		scanErr := row.Scan(&p.payload.RequestID, &p.room, &p.payload.Region, &p.payload.Score, &p.payload.Outcome)

		return p, scanErr
	})
	if err != nil {
		return fmt.Errorf("decode event grade decisions: %w", err)
	}

	if len(items) == 0 {
		return nil
	}

	entries := make([]dispatchoutbox.AlarmQueueEnvelope, len(items))
	for i := range items {
		p := &items[i]

		entries[i] = dispatchoutbox.AlarmQueueEnvelope{Notification: domain.AlarmNotification{RoomID: p.room, AlarmType: domain.AlarmTypeEventGrade}, SourceKind: domain.AlarmDispatchSourceKindEventGrade, EventGrade: &p.payload, Version: contract.QueueEnvelopeVersionV1}
	}

	result, err := publisher.PublishDispatchBatch(ctx, entries)
	if err != nil {
		err = fmt.Errorf("publish event grade: %w", err)

		if len(result.Receipts) == 0 {
			return err
		}
	}

	return errors.Join(err, r.settleReceipts(ctx, items, result.Receipts))
}

func (r *Repository) settleReceipts(ctx context.Context, items []pending, receipts []dispatchoutbox.PublishReceipt) error {
	positions := make([]int, len(items))

	var failures []error

	for j, receipt := range receipts {
		i := receipt.Ordinal
		switch {
		case i < 0 || i >= len(items):
			failures = append(failures, fmt.Errorf("invalid event grade receipt ordinal %d", i))
		case positions[i] != 0:
			positions[i] = -1
			failures = append(failures, fmt.Errorf("duplicate event grade receipt ordinal %d", i))
		default:
			positions[i] = j + 1
		}
	}

	for i, position := range positions {
		if position == 0 {
			failures = append(failures, fmt.Errorf("missing event grade receipt ordinal %d", i))
			continue
		}

		if position < 0 {
			continue
		}

		receipt := &receipts[position-1]

		var rejection *dispatchoutbox.PublishOutcome

		switch {
		case receipt.Accepted():
		case receipt.Outcome == dispatchoutbox.PublishRejectedCollision || receipt.Outcome == dispatchoutbox.PublishRejectedTerminal:
			rejection = &receipt.Outcome
			failures = append(failures, fmt.Errorf("event grade dispatch ordinal %d: %s", i, receipt.Outcome))
		default:
			failures = append(failures, fmt.Errorf("unknown event grade receipt outcome for ordinal %d", i))
			continue
		}

		p := &items[i].payload
		if _, err := pgxdb.Exec(ctx, r.pool, settleDispatchSQL, p.RequestID, p.Outcome, rejection); err != nil {
			failures = append(failures, fmt.Errorf("settle event grade receipt ordinal %d: %w", i, err))
		}
	}

	return errors.Join(failures...)
}
