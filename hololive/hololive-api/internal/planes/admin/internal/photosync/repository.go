package photosync

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// repository는 members 테이블의 photo·photo_updated_at만 읽고 쓴다.
type repository struct {
	pool *pgxpool.Pool
}

func (r repository) membersNeedingSync(ctx context.Context, staleThreshold time.Duration) ([]string, error) {
	staleTime := time.Now().Add(-staleThreshold)

	rows, err := r.pool.Query(ctx, membersNeedingSyncSQL, staleTime)
	if err != nil {
		return nil, fmt.Errorf("get members needing photo sync: %w", err)
	}
	defer rows.Close()

	var channelIDs []string

	for rows.Next() {
		var channelID string

		if err := rows.Scan(&channelID); err != nil {
			return nil, fmt.Errorf("scan channel id needing photo sync: %w", err)
		}

		channelIDs = append(channelIDs, channelID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photo sync rows iteration error: %w", err)
	}

	return channelIDs, nil
}

func (r repository) updatePhoto(ctx context.Context, channelID, photoURL string) error {
	if _, err := r.pool.Exec(ctx, updatePhotoSQL, channelID, photoURL, time.Now()); err != nil {
		return fmt.Errorf("update photo: %w", err)
	}

	return nil
}
