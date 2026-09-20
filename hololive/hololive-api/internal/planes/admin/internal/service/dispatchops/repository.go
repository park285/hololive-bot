package dispatchops

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed queries/*.sql
var queries embed.FS

// Repository는 기존 dispatch 테이블을 사용하며 스키마 변경이나 외부 발송을 수행하지 않습니다.
type Repository struct{ pool *pgxpool.Pool }

// NewRepository는 관리자 저장소를 만듭니다. Nil pool에서는 모든 작업이 명시적으로 실패합니다.
func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) available() error {
	if r == nil || r.pool == nil {
		return ErrUnavailable
	}

	return nil
}

func querySQL(name string) string {
	content, err := queries.ReadFile("queries/" + name + ".sql")
	if err != nil {
		panic(fmt.Sprintf("dispatch operation SQL missing: %s", name))
	}

	return string(content)
}

// Summary는 보존 중인 모든 발송 상태를 단일 PostgreSQL 스냅샷으로 집계합니다.
func (r *Repository) Summary(ctx context.Context) (Summary, error) {
	result := Summary{Counts: []StatusCount{}}

	if availabilityErr := r.available(); availabilityErr != nil {
		return result, availabilityErr
	}

	rows, err := r.pool.Query(ctx, querySQL("summary"))
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

// List는 최대 PageSize개를 반환하며 커서는 상태 변화와 무관한 ID 내림차순입니다.
func (r *Repository) List(ctx context.Context, filter Filter) (Page, error) {
	result := Page{Items: []Delivery{}}

	query, args, err := buildListQuery(querySQL("list"), filter)
	if err != nil {
		return result, err
	}

	if availabilityErr := r.available(); availabilityErr != nil {
		return result, availabilityErr
	}

	rows, err := r.pool.Query(ctx, query, args...)
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

// Detail은 같은 스냅샷에서 대상과 묶음을 조회합니다. 본문이나 오류 원문은 공개하지 않습니다.
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

	item, err := scanDelivery(tx.QueryRow(ctx, querySQL("detail"), numericID))
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

// Actions는 해당 발송 항목의 영속 감사 이력만 조회합니다. 상태나 로그를 생성하지 않습니다.
// 보존 기한이 지나 삭제된 ID에는 빈 목록을 반환합니다.
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

	rows, err := r.pool.Query(ctx, querySQL("actions"), numericID, before, PageSize+1)
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
	queryName := "group"

	if lock {
		queryName = "group_locked"
	}

	rows, err := tx.Query(ctx, querySQL(queryName), id, MaxReplaySize+1)
	if err != nil {
		return nil, fmt.Errorf("query dispatch replay group: %w", err)
	}

	return readDeliveries(rows)
}

func rollback(ctx context.Context, tx pgx.Tx) {
	// 취소된 HTTP 요청에도 연결을 정리하되 무제한 대기를 만들지 않습니다.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()

	if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		slog.Error("dispatch operation rollback failed")
	}
}

func conflictError(err error) error {
	if postgres, ok := errors.AsType[*pgconn.PgError](err); ok &&
		(postgres.Code == "40001" || postgres.Code == "40P01") {
		return fmt.Errorf("concurrent dispatch operation: %w", ErrConflict)
	}

	return err
}
