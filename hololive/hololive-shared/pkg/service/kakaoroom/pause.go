package kakaoroom

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"
)

type DeliveryPause struct {
	RoomID   string
	Reason   string
	PausedAt time.Time
}

type DeliveryPauses struct {
	pool *pgxpool.Pool
}

func NewDeliveryPauses(pool *pgxpool.Pool) *DeliveryPauses {
	return &DeliveryPauses{pool: pool}
}

func (p *DeliveryPauses) List(ctx context.Context) ([]DeliveryPause, error) {
	if p == nil || p.pool == nil {
		return nil, errors.New("list kakao room delivery pauses: database is not configured")
	}

	rows, err := pgxdb.Query(ctx, p.pool, listPausesSQL)
	if err != nil {
		return nil, fmt.Errorf("list kakao room delivery pauses: %w", err)
	}

	pauses, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (DeliveryPause, error) {
		var pause DeliveryPause

		scanErr := row.Scan(&pause.RoomID, &pause.Reason, &pause.PausedAt)

		return pause, scanErr
	})
	if err != nil {
		return nil, fmt.Errorf("list kakao room delivery pauses: %w", err)
	}

	return pauses, nil
}

func (p *DeliveryPauses) Resume(ctx context.Context, roomID string) (bool, error) {
	if p == nil || p.pool == nil {
		return false, errors.New("resume kakao room delivery: database is not configured")
	}

	return resumeDelivery(ctx, p.pool, roomID)
}

func resumeDelivery(ctx context.Context, db pgxdb.Querier, roomID string) (bool, error) {
	var resumed string

	err := pgxdb.QueryRow(ctx, db, resumeSQL, roomID).Scan(&resumed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("resume kakao room delivery: %w", err)
	}

	return true, nil
}
