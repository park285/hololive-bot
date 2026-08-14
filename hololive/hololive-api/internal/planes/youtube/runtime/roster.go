package runtime

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type rosterReader struct{}

func (rosterReader) NotificationChannelIDs(ctx context.Context, tx dbx.Tx) ([]string, error) {
	return loadChannelIDs(ctx, tx, "notification_channel_ids.sql", "notification")
}

func (rosterReader) OperationalChannelIDs(ctx context.Context, tx dbx.Tx) ([]string, error) {
	return loadChannelIDs(ctx, tx, "operational_channel_ids.sql", "operational")
}

func (rosterReader) ViewerVideoIDs(ctx context.Context, tx dbx.Tx) ([]string, error) {
	return targetprojection.LiveHeadViewerVideoIDs(ctx, tx)
}

func loadChannelIDs(ctx context.Context, tx dbx.Tx, queryName, label string) ([]string, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: transaction is not configured", targetprojection.ErrInputRead)
	}
	rows, err := tx.Query(ctx, mustSQL(queryName), targetprojection.MaxInputChannelCount+1)
	if err != nil {
		return nil, fmt.Errorf("%w: load %s channels: %w", targetprojection.ErrInputRead, label, err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var channelID string
		if err := rows.Scan(&channelID); err != nil {
			return nil, fmt.Errorf("%w: scan %s channel: %w", targetprojection.ErrInputRead, label, err)
		}
		ids = append(ids, channelID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: read %s channels: %w", targetprojection.ErrInputRead, label, err)
	}
	if len(ids) > targetprojection.MaxInputChannelCount {
		return nil, fmt.Errorf("%w: %s channel count exceeds %d", targetprojection.ErrInvalidProjection, label, targetprojection.MaxInputChannelCount)
	}
	return ids, nil
}
