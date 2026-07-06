package alarm

import (
	"context"
	"fmt"
)

func (r *Repository) GetAllDistinctRoomIDs(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, mustSQL("repository_rooms_0009_01.sql"))
	if err != nil {
		return nil, fmt.Errorf("get all distinct room ids: %w", err)
	}
	defer rows.Close()

	var roomIDs []string
	for rows.Next() {
		var roomID string
		if err := rows.Scan(&roomID); err != nil {
			return nil, fmt.Errorf("scan room id: %w", err)
		}
		roomIDs = append(roomIDs, roomID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("room ids rows iteration: %w", err)
	}
	return roomIDs, nil
}
