package kakaoroom

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

	if _, err := s.pool.Exec(ctx, mustSQL("upsert.sql"), facts.RoomID, facts.RoomType, facts.RoomLinkID); err != nil {
		return fmt.Errorf("upsert kakao room %s: %w", facts.RoomID, err)
	}

	return nil
}

func (s *store) get(ctx context.Context, roomID string) (Facts, bool, error) {
	if s == nil || s.pool == nil {
		return Facts{}, false, nil
	}

	var facts Facts

	err := s.pool.QueryRow(ctx, mustSQL("get.sql"), roomID).Scan(&facts.RoomID, &facts.RoomType, &facts.RoomLinkID)

	if errors.Is(err, pgx.ErrNoRows) {
		return Facts{}, false, nil
	}

	if err != nil {
		return Facts{}, false, fmt.Errorf("get kakao room %s: %w", roomID, err)
	}

	return facts, true, nil
}
