package alarmstore

import (
	"context"
	"fmt"

	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *Repository) CountAlarmEntries(ctx context.Context) (int, error) {
	var count int

	if err := pgxdb.QueryRow(ctx, r.pool, countAlarmEntriesSQL).Scan(&count); err != nil {
		return 0, fmt.Errorf("count alarm entries: %w", err)
	}

	return count, nil
}

func (r *Repository) GetAllDistinctRoomIDs(ctx context.Context) ([]string, error) {
	rows, err := pgxdb.Query(ctx, r.pool, getAllDistinctRoomIDsSQL)
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

func (r *Repository) ListAlarmEntries(ctx context.Context) ([]*domain.AlarmEntry, error) {
	rows, err := pgxdb.Query(ctx, r.pool, listAlarmEntriesSQL)
	if err != nil {
		return nil, fmt.Errorf("list alarm entries: %w", err)
	}
	defer rows.Close()

	var entries []*domain.AlarmEntry

	for rows.Next() {
		var entry domain.AlarmEntry

		if err := rows.Scan(&entry.RoomID, &entry.ChannelID, &entry.RoomName); err != nil {
			return nil, fmt.Errorf("scan alarm entry: %w", err)
		}

		entries = append(entries, &entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("alarm entries rows iteration: %w", err)
	}

	return entries, nil
}

func (r *Repository) SetRoomDisplayName(ctx context.Context, roomID, displayName string) error {
	if _, err := pgxdb.Exec(ctx, r.pool, setRoomDisplayNameSQL, roomID, displayName); err != nil {
		return fmt.Errorf("upsert room display name: %w", err)
	}

	return nil
}

func (r *Repository) DeleteRoomDisplayName(ctx context.Context, roomID string) error {
	if _, err := pgxdb.Exec(ctx, r.pool, deleteRoomDisplayNameSQL, roomID); err != nil {
		return fmt.Errorf("delete room display name: %w", err)
	}

	return nil
}
