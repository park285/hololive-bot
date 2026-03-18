package settlement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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

// FindMemberByUserID: 방 + kakao_user_id로 멤버 조회. 미존재 시 nil, nil 반환.
func (r *Repository) FindMemberByUserID(ctx context.Context, roomID, kakaoUserID string) (*Member, error) {
	var m Member
	err := r.pool.QueryRow(ctx,
		"SELECT id, room_id, kakao_user_id, member_name FROM settlement_members WHERE room_id = $1 AND kakao_user_id = $2",
		roomID, kakaoUserID,
	).Scan(&m.ID, &m.RoomID, &m.KakaoUserID, &m.MemberName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("멤버 조회 실패 (room=%s, kakaoUserID=%s): %w", roomID, kakaoUserID, err)
	}
	return &m, nil
}

// EnsureCycle: 방별 월별 정산 주기 생성 또는 기존 조회. 없으면 생성 후 반환.
func (r *Repository) EnsureCycle(ctx context.Context, roomID string, year, month int) (*Cycle, error) {
	_, err := r.pool.Exec(ctx,
		"INSERT INTO settlement_cycles (room_id, year, month) VALUES ($1, $2, $3) ON CONFLICT (room_id, year, month) DO NOTHING",
		roomID, year, month,
	)
	if err != nil {
		return nil, fmt.Errorf("정산 주기 생성 실패 (room=%s, year=%d, month=%d): %w", roomID, year, month, err)
	}

	var c Cycle
	err = r.pool.QueryRow(ctx,
		"SELECT id, room_id, year, month, total_amount, per_person, due_day FROM settlement_cycles WHERE room_id = $1 AND year = $2 AND month = $3",
		roomID, year, month,
	).Scan(&c.ID, &c.RoomID, &c.Year, &c.Month, &c.TotalAmount, &c.PerPerson, &c.DueDay)
	if err != nil {
		return nil, fmt.Errorf("정산 주기 조회 실패 (room=%s, year=%d, month=%d): %w", roomID, year, month, err)
	}
	return &c, nil
}

// EnsurePaymentRows: 해당 주기에 해당 방의 멤버에 대한 납부 행 생성. 이미 존재하면 무시.
func (r *Repository) EnsurePaymentRows(ctx context.Context, roomID string, cycleID int) error {
	_, err := r.pool.Exec(ctx,
		"INSERT INTO settlement_payments (cycle_id, member_id) SELECT $1, id FROM settlement_members WHERE room_id = $2 ON CONFLICT DO NOTHING",
		cycleID, roomID,
	)
	if err != nil {
		return fmt.Errorf("납부 행 생성 실패 (room=%s, cycleID=%d): %w", roomID, cycleID, err)
	}
	return nil
}

// MarkPaid: 납부 완료 처리. 이미 납부된 경우 무시 (멱등성 보장).
func (r *Repository) MarkPaid(ctx context.Context, cycleID, memberID int) error {
	_, err := r.pool.Exec(ctx,
		"UPDATE settlement_payments SET paid_at = NOW() WHERE cycle_id = $1 AND member_id = $2 AND paid_at IS NULL",
		cycleID, memberID,
	)
	if err != nil {
		return fmt.Errorf("납부 처리 실패 (cycleID=%d, memberID=%d): %w", cycleID, memberID, err)
	}
	return nil
}

// GetPaymentStatuses: 해당 주기의 전체 멤버 납부 상태 조회.
func (r *Repository) GetPaymentStatuses(ctx context.Context, cycleID int) ([]PaymentStatus, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT sm.member_name, sp.paid_at FROM settlement_payments sp JOIN settlement_members sm ON sm.id = sp.member_id WHERE sp.cycle_id = $1 ORDER BY sm.id",
		cycleID,
	)
	if err != nil {
		return nil, fmt.Errorf("납부 상태 조회 실패 (cycleID=%d): %w", cycleID, err)
	}
	defer rows.Close()

	var statuses []PaymentStatus
	for rows.Next() {
		var ps PaymentStatus
		if err := rows.Scan(&ps.MemberName, &ps.PaidAt); err != nil {
			return nil, fmt.Errorf("납부 상태 행 스캔 실패: %w", err)
		}
		statuses = append(statuses, ps)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("납부 상태 순회 실패: %w", err)
	}
	return statuses, nil
}

// GetUnpaidMembers: 해당 주기의 미납 멤버 이름 목록 조회.
func (r *Repository) GetUnpaidMembers(ctx context.Context, cycleID int) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT sm.member_name FROM settlement_payments sp JOIN settlement_members sm ON sm.id = sp.member_id WHERE sp.cycle_id = $1 AND sp.paid_at IS NULL ORDER BY sm.id",
		cycleID,
	)
	if err != nil {
		return nil, fmt.Errorf("미납 멤버 조회 실패 (cycleID=%d): %w", cycleID, err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("미납 멤버 행 스캔 실패: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("미납 멤버 순회 실패: %w", err)
	}
	return names, nil
}
