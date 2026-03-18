package settlement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository: 정산 관련 PostgreSQL 저장소.
type Repository struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewRepository: Repository 생성자.
func NewRepository(pool *pgxpool.Pool, logger *slog.Logger) *Repository {
	return &Repository{pool: pool, logger: logger}
}

// WithTx: 트랜잭션 범위를 실행합니다.
func (r *Repository) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("tx begin failed: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("tx commit failed: %w", err)
	}
	return nil
}

// LockRoomTx: 같은 방에서 동시에 회차/납부 처리가 중복 생성되지 않도록 advisory lock을 획득합니다.
func (r *Repository) LockRoomTx(ctx context.Context, tx pgx.Tx, roomID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, roomID)
	if err != nil {
		return fmt.Errorf("room advisory lock failed (room=%s): %w", roomID, err)
	}
	return nil
}

// GetRoomConfigTx: 방 설정을 조회합니다. 없으면 기본 설정으로 생성합니다.
func (r *Repository) GetRoomConfigTx(ctx context.Context, tx pgx.Tx, roomID string) (RoomConfig, error) {
	_, err := tx.Exec(ctx,
		`INSERT INTO settlement_room_configs (room_id) VALUES ($1) ON CONFLICT (room_id) DO NOTHING`,
		roomID,
	)
	if err != nil {
		return RoomConfig{}, fmt.Errorf("room config ensure failed (room=%s): %w", roomID, err)
	}

	var cfg RoomConfig
	err = tx.QueryRow(ctx, `
		SELECT room_id, billing_anchor_day, billing_tz, total_amount, per_person, require_explicit_for_multiple
		FROM settlement_room_configs
		WHERE room_id = $1
	`, roomID).Scan(
		&cfg.RoomID,
		&cfg.BillingAnchorDay,
		&cfg.BillingTZ,
		&cfg.TotalAmount,
		&cfg.PerPerson,
		&cfg.RequireExplicitForMultiple,
	)
	if err != nil {
		return RoomConfig{}, fmt.Errorf("room config load failed (room=%s): %w", roomID, err)
	}
	return cfg, nil
}

// EnsureMemberTermsSeededTx: 기존 settlement_members를 기준으로 기본 membership term을 보장합니다.
func (r *Repository) EnsureMemberTermsSeededTx(ctx context.Context, tx pgx.Tx, roomID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO settlement_member_terms (room_id, member_id, effective_from_at)
		SELECT sm.room_id, sm.id, sm.registered_at
		FROM settlement_members sm
		WHERE sm.room_id = $1
		  AND NOT EXISTS (
			  SELECT 1
			  FROM settlement_member_terms smt
			  WHERE smt.room_id = sm.room_id
			    AND smt.member_id = sm.id
		  )
	`, roomID)
	if err != nil {
		return fmt.Errorf("member term seed failed (room=%s): %w", roomID, err)
	}
	return nil
}

// FindMemberByUserIDTx: 방 + kakao_user_id로 멤버 조회. 미존재 시 nil, nil 반환.
func (r *Repository) FindMemberByUserIDTx(ctx context.Context, tx pgx.Tx, roomID, kakaoUserID string) (*Member, error) {
	var m Member
	err := tx.QueryRow(ctx, `
		SELECT id, room_id, kakao_user_id, member_name
		FROM settlement_members
		WHERE room_id = $1 AND kakao_user_id = $2
	`, roomID, kakaoUserID).Scan(&m.ID, &m.RoomID, &m.KakaoUserID, &m.MemberName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("member load failed (room=%s, kakaoUserID=%s): %w", roomID, kakaoUserID, err)
	}
	return &m, nil
}

// FindCycleByKeyTx: cycle_key로 회차 조회. 없으면 nil, nil 반환.
func (r *Repository) FindCycleByKeyTx(ctx context.Context, tx pgx.Tx, roomID, cycleKey string) (*Cycle, error) {
	var c Cycle
	err := tx.QueryRow(ctx, `
		SELECT id, room_id, cycle_key::text, period_start_at, period_end_at,
		       total_amount, per_person, billing_anchor_day, member_count_snapshot, created_at
		FROM settlement_cycles_v2
		WHERE room_id = $1 AND cycle_key = $2::date
	`, roomID, cycleKey).Scan(
		&c.ID,
		&c.RoomID,
		&c.CycleKey,
		&c.PeriodStartAt,
		&c.PeriodEndAt,
		&c.TotalAmount,
		&c.PerPerson,
		&c.BillingAnchorDay,
		&c.MemberCountSnapshot,
		&c.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("cycle load by key failed (room=%s, cycleKey=%s): %w", roomID, cycleKey, err)
	}
	return &c, nil
}

// GetCycleByIDTx: ID로 회차 조회.
func (r *Repository) GetCycleByIDTx(ctx context.Context, tx pgx.Tx, cycleID int) (*Cycle, error) {
	var c Cycle
	err := tx.QueryRow(ctx, `
		SELECT id, room_id, cycle_key::text, period_start_at, period_end_at,
		       total_amount, per_person, billing_anchor_day, member_count_snapshot, created_at
		FROM settlement_cycles_v2
		WHERE id = $1
	`, cycleID).Scan(
		&c.ID,
		&c.RoomID,
		&c.CycleKey,
		&c.PeriodStartAt,
		&c.PeriodEndAt,
		&c.TotalAmount,
		&c.PerPerson,
		&c.BillingAnchorDay,
		&c.MemberCountSnapshot,
		&c.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("cycle load by id failed (cycleID=%d): %w", cycleID, err)
	}
	return &c, nil
}

// GetLatestCycleTx: 방의 가장 최근 회차 조회. 없으면 nil, nil 반환.
func (r *Repository) GetLatestCycleTx(ctx context.Context, tx pgx.Tx, roomID string) (*Cycle, error) {
	var c Cycle
	err := tx.QueryRow(ctx, `
		SELECT id, room_id, cycle_key::text, period_start_at, period_end_at,
		       total_amount, per_person, billing_anchor_day, member_count_snapshot, created_at
		FROM settlement_cycles_v2
		WHERE room_id = $1
		ORDER BY period_start_at DESC
		LIMIT 1
	`, roomID).Scan(
		&c.ID,
		&c.RoomID,
		&c.CycleKey,
		&c.PeriodStartAt,
		&c.PeriodEndAt,
		&c.TotalAmount,
		&c.PerPerson,
		&c.BillingAnchorDay,
		&c.MemberCountSnapshot,
		&c.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("latest cycle load failed (room=%s): %w", roomID, err)
	}
	return &c, nil
}

// InsertCycleTx: 회차 스냅샷을 생성합니다.
func (r *Repository) InsertCycleTx(ctx context.Context, tx pgx.Tx, c Cycle) (*Cycle, error) {
	var out Cycle
	err := tx.QueryRow(ctx, `
		INSERT INTO settlement_cycles_v2 (
			room_id,
			cycle_key,
			period_start_at,
			period_end_at,
			total_amount,
			per_person,
			billing_anchor_day,
			member_count_snapshot
		) VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8)
		RETURNING id, room_id, cycle_key::text, period_start_at, period_end_at,
		          total_amount, per_person, billing_anchor_day, member_count_snapshot, created_at
	`,
		c.RoomID,
		c.CycleKey,
		c.PeriodStartAt,
		c.PeriodEndAt,
		c.TotalAmount,
		c.PerPerson,
		c.BillingAnchorDay,
		c.MemberCountSnapshot,
	).Scan(
		&out.ID,
		&out.RoomID,
		&out.CycleKey,
		&out.PeriodStartAt,
		&out.PeriodEndAt,
		&out.TotalAmount,
		&out.PerPerson,
		&out.BillingAnchorDay,
		&out.MemberCountSnapshot,
		&out.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("cycle insert failed (room=%s, cycleKey=%s): %w", c.RoomID, c.CycleKey, err)
	}
	return &out, nil
}

// ListActiveMembersAtTx: 특정 시점에 활성인 멤버를 membership term 기준으로 조회합니다.
func (r *Repository) ListActiveMembersAtTx(ctx context.Context, tx pgx.Tx, roomID string, at time.Time) ([]MemberSnapshot, error) {
	if err := r.EnsureMemberTermsSeededTx(ctx, tx, roomID); err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
		SELECT sm.id, sm.member_name
		FROM settlement_member_terms smt
		JOIN settlement_members sm ON sm.id = smt.member_id
		WHERE smt.room_id = $1
		  AND smt.effective_from_at <= $2
		  AND (smt.effective_to_at IS NULL OR smt.effective_to_at > $2)
		ORDER BY sm.id
	`, roomID, at)
	if err != nil {
		return nil, fmt.Errorf("active members load failed (room=%s): %w", roomID, err)
	}
	defer rows.Close()

	var members []MemberSnapshot
	for rows.Next() {
		var m MemberSnapshot
		if err := rows.Scan(&m.MemberID, &m.MemberName); err != nil {
			return nil, fmt.Errorf("active member row scan failed: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("active member rows failed: %w", err)
	}
	return members, nil
}

// InsertCyclePaymentRowTx: 회차별 멤버 납부 스냅샷 row 생성.
func (r *Repository) InsertCyclePaymentRowTx(ctx context.Context, tx pgx.Tx, cycleID, memberID int, memberName string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO settlement_payments_v2 (cycle_id, member_id, member_name_snapshot)
		VALUES ($1, $2, $3)
		ON CONFLICT (cycle_id, member_id) DO NOTHING
	`, cycleID, memberID, memberName)
	if err != nil {
		return fmt.Errorf("cycle payment row insert failed (cycleID=%d, memberID=%d): %w", cycleID, memberID, err)
	}
	return nil
}

// GetPaymentStatusesTx: 해당 회차의 납부 상태 조회.
func (r *Repository) GetPaymentStatusesTx(ctx context.Context, tx pgx.Tx, cycleID int) ([]PaymentStatus, error) {
	rows, err := tx.Query(ctx, `
		SELECT member_id, member_name_snapshot, paid_at
		FROM settlement_payments_v2
		WHERE cycle_id = $1
		ORDER BY id
	`, cycleID)
	if err != nil {
		return nil, fmt.Errorf("payment statuses load failed (cycleID=%d): %w", cycleID, err)
	}
	defer rows.Close()

	var statuses []PaymentStatus
	for rows.Next() {
		var ps PaymentStatus
		if err := rows.Scan(&ps.MemberID, &ps.MemberNameSnapshot, &ps.PaidAt); err != nil {
			return nil, fmt.Errorf("payment status row scan failed: %w", err)
		}
		statuses = append(statuses, ps)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("payment status rows failed: %w", err)
	}
	return statuses, nil
}

// GetUnpaidMemberNamesTx: 해당 회차의 미납 멤버 이름 목록 조회.
func (r *Repository) GetUnpaidMemberNamesTx(ctx context.Context, tx pgx.Tx, cycleID int) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT member_name_snapshot
		FROM settlement_payments_v2
		WHERE cycle_id = $1 AND paid_at IS NULL
		ORDER BY id
	`, cycleID)
	if err != nil {
		return nil, fmt.Errorf("unpaid names load failed (cycleID=%d): %w", cycleID, err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("unpaid name row scan failed: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("unpaid name rows failed: %w", err)
	}
	return names, nil
}

// FindPaymentTargetByCycleKeyTx: 특정 회차에 대한 멤버의 납부 row를 조회합니다. 이미 납부된 row도 반환합니다.
func (r *Repository) FindPaymentTargetByCycleKeyTx(ctx context.Context, tx pgx.Tx, roomID string, memberID int, cycleKey string) (*PaymentTarget, error) {
	var t PaymentTarget
	err := tx.QueryRow(ctx, `
		SELECT sc.id, sc.cycle_key::text, sc.period_start_at, sc.period_end_at, sp.member_id, sp.paid_at
		FROM settlement_payments_v2 sp
		JOIN settlement_cycles_v2 sc ON sc.id = sp.cycle_id
		WHERE sc.room_id = $1
		  AND sp.member_id = $2
		  AND sc.cycle_key = $3::date
		FOR UPDATE OF sp
	`, roomID, memberID, cycleKey).Scan(
		&t.CycleID,
		&t.CycleKey,
		&t.PeriodStartAt,
		&t.PeriodEndAt,
		&t.MemberID,
		&t.PaidAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("payment target load failed (room=%s, memberID=%d, cycleKey=%s): %w", roomID, memberID, cycleKey, err)
	}
	return &t, nil
}

// ListPendingPaymentTargetsUpToTx: 현재 회차 시작 이전까지의 미납 row를 오래된 순으로 조회합니다.
func (r *Repository) ListPendingPaymentTargetsUpToTx(ctx context.Context, tx pgx.Tx, roomID string, memberID int, upToStartAt time.Time) ([]PaymentTarget, error) {
	rows, err := tx.Query(ctx, `
		SELECT sc.id, sc.cycle_key::text, sc.period_start_at, sc.period_end_at, sp.member_id, sp.paid_at
		FROM settlement_payments_v2 sp
		JOIN settlement_cycles_v2 sc ON sc.id = sp.cycle_id
		WHERE sc.room_id = $1
		  AND sp.member_id = $2
		  AND sp.paid_at IS NULL
		  AND sc.period_start_at <= $3
		ORDER BY sc.period_start_at ASC
		FOR UPDATE OF sp
	`, roomID, memberID, upToStartAt)
	if err != nil {
		return nil, fmt.Errorf("pending payment targets load failed (room=%s, memberID=%d): %w", roomID, memberID, err)
	}
	defer rows.Close()

	var targets []PaymentTarget
	for rows.Next() {
		var t PaymentTarget
		if err := rows.Scan(&t.CycleID, &t.CycleKey, &t.PeriodStartAt, &t.PeriodEndAt, &t.MemberID, &t.PaidAt); err != nil {
			return nil, fmt.Errorf("pending target row scan failed: %w", err)
		}
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pending target rows failed: %w", err)
	}
	return targets, nil
}

// MarkPaidTx: 납부 완료 처리. 이미 납부된 경우에도 멱등적으로 성공합니다.
func (r *Repository) MarkPaidTx(ctx context.Context, tx pgx.Tx, cycleID, memberID int, paidAt time.Time) error {
	tag, err := tx.Exec(ctx, `
		UPDATE settlement_payments_v2
		SET paid_at = COALESCE(paid_at, $3)
		WHERE cycle_id = $1 AND member_id = $2
	`, cycleID, memberID, paidAt)
	if err != nil {
		return fmt.Errorf("mark paid failed (cycleID=%d, memberID=%d): %w", cycleID, memberID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("mark paid affected 0 rows (cycleID=%d, memberID=%d)", cycleID, memberID)
	}
	return nil
}

// FindPaymentEventBySourceTx: 외부 이벤트 dedup 조회.
func (r *Repository) FindPaymentEventBySourceTx(ctx context.Context, tx pgx.Tx, sourceType, sourceEventID string) (*PaymentEventRef, error) {
	if sourceType == "" || sourceEventID == "" {
		return nil, nil
	}

	var ref PaymentEventRef
	err := tx.QueryRow(ctx, `
		SELECT cycle_id
		FROM settlement_payment_events_v2
		WHERE source_type = $1 AND source_event_id = $2
	`, sourceType, sourceEventID).Scan(&ref.CycleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("payment event lookup failed (sourceType=%s, sourceEventID=%s): %w", sourceType, sourceEventID, err)
	}
	return &ref, nil
}

// InsertPaymentEventTx: 외부 이벤트 dedup 이력을 저장합니다.
func (r *Repository) InsertPaymentEventTx(ctx context.Context, tx pgx.Tx, cycleID, memberID int, sourceType, sourceEventID string, paidAt time.Time) error {
	if sourceType == "" || sourceEventID == "" {
		return nil
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO settlement_payment_events_v2 (cycle_id, member_id, source_type, source_event_id, paid_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (source_type, source_event_id) DO NOTHING
	`, cycleID, memberID, sourceType, sourceEventID, paidAt)
	if err != nil {
		return fmt.Errorf("payment event insert failed (cycleID=%d, memberID=%d): %w", cycleID, memberID, err)
	}
	return nil
}
