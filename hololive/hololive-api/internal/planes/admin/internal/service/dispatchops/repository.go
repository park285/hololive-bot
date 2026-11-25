package dispatchops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) available() error {
	if r == nil || r.pool == nil {
		return ErrUnavailable
	}

	return nil
}

func (r *Repository) Summary(ctx context.Context) (Summary, error) {
	result := Summary{Counts: []StatusCount{}}

	if availabilityErr := r.available(); availabilityErr != nil {
		return result, availabilityErr
	}

	rows, err := pgxdb.Query(ctx, r.pool, summarySQL)
	if err != nil {
		return result, fmt.Errorf("query dispatch summary: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item StatusCount

		if err := rows.Scan(&item.Status, &item.Count, &item.OldestAt); err != nil {
			return Summary{}, fmt.Errorf("scan dispatch summary: %w", err)
		}

		result.Counts = append(result.Counts, item)
	}

	if err := rows.Err(); err != nil {
		return Summary{}, fmt.Errorf("read dispatch summary: %w", err)
	}

	result.ObservedAt = time.Now().UTC()

	return result, nil
}

func (r *Repository) Failures(ctx context.Context) (FailureBreakdown, error) {
	result := FailureBreakdown{
		Total: "0", ErrorCodes: []FailureCount{}, AlarmTypes: []FailureCount{},
		Channels: []FailureCount{}, Rooms: []FailureCount{},
	}

	if err := r.available(); err != nil {
		return result, err
	}

	rows, err := pgxdb.Query(ctx, r.pool, failuresSQL, FailureBreakdownLimit)
	if err != nil {
		return result, fmt.Errorf("query dispatch failures: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var kind string

		var item FailureCount

		if err := rows.Scan(&kind, &item.Value, &item.Count); err != nil {
			return FailureBreakdown{}, fmt.Errorf("scan dispatch failures: %w", err)
		}

		switch kind {
		case "total":
			result.Total = item.Count
		case "errorCodes":
			result.ErrorCodes = append(result.ErrorCodes, item)
		case "alarmTypes":
			result.AlarmTypes = append(result.AlarmTypes, item)
		case "channels":
			result.Channels = append(result.Channels, item)
		case "rooms":
			result.Rooms = append(result.Rooms, item)
		}
	}

	if err := rows.Err(); err != nil {
		return FailureBreakdown{}, fmt.Errorf("read dispatch failures: %w", err)
	}

	result.ObservedAt = time.Now().UTC()

	return result, nil
}

func (r *Repository) List(ctx context.Context, filter Filter) (Page, error) {
	result := Page{Items: []Delivery{}}

	query, args, err := buildListQuery(listSQL, filter)
	if err != nil {
		return result, err
	}

	if availabilityErr := r.available(); availabilityErr != nil {
		return result, availabilityErr
	}

	rows, err := r.pool.Query(ctx, query, args...) //nolint:forbidigo // 필터에 따라 조건절과 자리표시자를 조립하는 관리자 목록 조회다.
	if err != nil {
		return result, fmt.Errorf("query dispatch deliveries: %w", err)
	}

	items, err := readDeliveries(rows)
	if err != nil {
		return result, err
	}

	if len(items) > PageSize {
		result.NextBeforeID = items[PageSize-1].ID
		items = items[:PageSize]
	}

	result.Items = items

	return result, nil
}

func (r *Repository) Detail(ctx context.Context, id string) (Detail, error) {
	result := Detail{ReplayTargets: []Revision{}, Group: []Delivery{}}

	numericID, err := ParseID(id)
	if err != nil {
		return result, err
	}

	if availabilityErr := r.available(); availabilityErr != nil {
		return result, availabilityErr
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, fmt.Errorf("begin dispatch detail: %w", err)
	}
	defer rollback(ctx, tx)

	item, err := scanDelivery(pgxdb.QueryRow(ctx, tx, detailSQL, numericID))
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrNotFound
	}

	if err != nil {
		return result, fmt.Errorf("read dispatch detail: %w", err)
	}

	group, err := loadGroup(ctx, tx, numericID, false)
	if err != nil {
		return result, err
	}

	result.Delivery = item
	result.ReplayBlocked = replayBlock(group)
	result.Group = group

	if len(group) > MaxReplaySize {
		result.Group = group[:MaxReplaySize]
		result.GroupTruncated = true
	}

	if result.ReplayBlocked == "" {
		for index := range group {
			member := &group[index]

			result.ReplayTargets = append(result.ReplayTargets, Revision{ID: member.ID, UpdatedAt: member.UpdatedAt})
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Detail{}, fmt.Errorf("commit dispatch detail: %w", err)
	}

	return result, nil
}

func (r *Repository) Actions(ctx context.Context, id, beforeID string) (ActionPage, error) {
	result := ActionPage{Items: []Action{}}

	numericID, err := ParseID(id)
	if err != nil {
		return result, err
	}

	before, err := optionalID(beforeID)
	if err != nil {
		return result, err
	}

	if availabilityErr := r.available(); availabilityErr != nil {
		return result, availabilityErr
	}

	rows, err := pgxdb.Query(ctx, r.pool, actionsSQL, numericID, before, PageSize+1)
	if err != nil {
		return result, fmt.Errorf("query dispatch actions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item Action

		if err := rows.Scan(&item.ID, &item.DeliveryID, &item.Action, &item.OperatorID, &item.Reason,
			&item.FromStatus, &item.ToStatus, &item.DuplicateRiskAck, &item.CreatedAt); err != nil {
			return ActionPage{}, fmt.Errorf("scan dispatch action: %w", err)
		}

		result.Items = append(result.Items, item)
	}

	if err := rows.Err(); err != nil {
		return ActionPage{}, fmt.Errorf("read dispatch actions: %w", err)
	}

	if len(result.Items) > PageSize {
		result.NextBeforeID = result.Items[PageSize-1].ID
		result.Items = result.Items[:PageSize]
	}

	return result, nil
}

func optionalID(value string) (any, error) {
	if value == "" {
		return nil, nil //nolint:nilnil // 비어 있는 커서는 SQL NULL 인수로 전달합니다.
	}

	return ParseID(value)
}

func scanDelivery(row pgx.Row) (Delivery, error) {
	var item Delivery

	err := row.Scan(&item.ID, &item.EventID, &item.RoomID, &item.SendUnitID,
		&item.AlarmType, &item.ChannelID, &item.StreamID, &item.Status, &item.AttemptCount,
		&item.ErrorCode, &item.NextAttemptAt, &item.CreatedAt, &item.UpdatedAt,
		&item.LockExpiresAt, &item.SendingAt, &item.SentAt, &item.DLQAt, &item.QuarantinedAt, &item.CancelledAt)
	if err != nil {
		return Delivery{}, fmt.Errorf("scan dispatch delivery: %w", err)
	}

	return item, nil
}

func readDeliveries(rows pgx.Rows) ([]Delivery, error) {
	defer rows.Close()

	items := make([]Delivery, 0, PageSize+1)

	for rows.Next() {
		item, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read dispatch deliveries: %w", err)
	}

	return items, nil
}

func loadGroup(ctx context.Context, tx pgx.Tx, id int64, lock bool) ([]Delivery, error) {
	query := groupSQL

	if lock {
		query = groupLockedSQL
	}

	rows, err := pgxdb.Query(ctx, tx, query, id, MaxReplaySize+1)
	if err != nil {
		return nil, fmt.Errorf("query dispatch replay group: %w", err)
	}

	return readDeliveries(rows)
}

func rollback(ctx context.Context, tx pgx.Tx) {
	if err := pgxdb.Rollback(ctx, tx); err != nil {
		slog.Error("dispatch operation rollback failed", slog.Any("error", err))
	}
}

func conflictError(err error) error {
	if postgres, ok := errors.AsType[*pgconn.PgError](err); ok &&
		(postgres.Code == "40001" || postgres.Code == "40P01") {
		return fmt.Errorf("concurrent dispatch operation: %w", ErrConflict)
	}

	return err
}
