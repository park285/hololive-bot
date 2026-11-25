package kakaoroom

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"
)

type store struct {
	pool *pgxpool.Pool
}

func (s *store) upsert(ctx context.Context, facts Facts) error {
	if s == nil || s.pool == nil {
		return nil
	}

	if facts.RoomID == "" {
		return errors.New("upsert kakao room: room id is required")
	}

	if _, err := pgxdb.Exec(ctx, s.pool, upsertSQL, facts.RoomID, facts.RoomType, facts.RoomLinkID); err != nil {
		return fmt.Errorf("upsert kakao room %s: %w", facts.RoomID, err)
	}

	return nil
}

func (s *store) resume(ctx context.Context, roomID string) (bool, error) {
	if s == nil || s.pool == nil {
		return false, nil
	}

	return resumeDelivery(ctx, s.pool, roomID)
}

func (s *store) get(ctx context.Context, roomID string) (Facts, bool, error) {
	if s == nil || s.pool == nil {
		return Facts{}, false, nil
	}

	var facts Facts

	err := pgxdb.QueryRow(ctx, s.pool, getSQL, roomID).Scan(&facts.RoomID, &facts.RoomType, &facts.RoomLinkID)

	if errors.Is(err, pgx.ErrNoRows) {
		return Facts{}, false, nil
	}

	if err != nil {
		return Facts{}, false, fmt.Errorf("get kakao room %s: %w", roomID, err)
	}

	return facts, true, nil
}
