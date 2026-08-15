package targetprojection

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

func LiveHeadViewerVideoIDs(ctx context.Context, tx dbx.Tx) ([]string, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: transaction is not configured", ErrInputRead)
	}
	rows, err := tx.Query(ctx, mustSQL("live_head_viewer_video_ids.sql"), MaxInputChannelCount+1)
	if err != nil {
		return nil, fmt.Errorf("%w: load live head videos: %w", ErrInputRead, err)
	}
	defer rows.Close()
	videos := make([]string, 0)
	for rows.Next() {
		var videoID string
		if err := rows.Scan(&videoID); err != nil {
			return nil, fmt.Errorf("%w: scan live head video: %w", ErrInputRead, err)
		}
		videos = append(videos, videoID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: read live head videos: %w", ErrInputRead, err)
	}
	if len(videos) > MaxInputChannelCount {
		return nil, fmt.Errorf("%w: viewer video count exceeds %d", ErrInvalidProjection, MaxInputChannelCount)
	}
	return videos, nil
}
