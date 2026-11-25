package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"
)

func (s *TransitionStore) PausedRooms(ctx context.Context, roomIDs []string) (map[string]bool, error) {
	paused := make(map[string]bool)

	if len(roomIDs) == 0 {
		return paused, nil
	}

	rows, err := pgxdb.Query(ctx, s.db, pausedRoomsSQL, roomIDs)
	if err != nil {
		return nil, fmt.Errorf("load paused kakao rooms: %w", err)
	}

	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("load paused kakao rooms: %w", err)
	}

	for _, id := range ids {
		paused[id] = true
	}

	return paused, nil
}
