package runtime

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type rosterReader struct{}

func (rosterReader) NotificationChannelIDs(ctx context.Context, tx dbx.Tx) ([]string, error) {
	out, err := loadChannelIDs(ctx, tx, "notification_channel_ids.sql", "notification")
	if err != nil {
		return out, fmt.Errorf("load channel IDs: %w", err)
	}

	return out, nil
}

func (rosterReader) OperationalChannelIDs(ctx context.Context, tx dbx.Tx) ([]string, error) {
	out, err := loadChannelIDs(ctx, tx, "operational_channel_ids.sql", "operational")
	if err != nil {
		return out, fmt.Errorf("load channel IDs: %w", err)
	}

	return out, nil
}

// StaleLiveVideos는 같은 projection transaction에서 운영 roster의 신선한 positive 없는 LIVE 영상을 조회 시점 DB 시각으로 읽습니다.
func (rosterReader) StaleLiveVideos(
	ctx context.Context,
	tx dbx.Tx,
	query targetprojection.StaleLiveVideoQuery,
) ([]targetprojection.StaleLiveVideo, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: transaction is not configured", targetprojection.ErrInputRead)
	}

	if query.FreshnessBudget <= 0 {
		return nil, fmt.Errorf("%w: stale live video freshness budget is invalid", targetprojection.ErrInvalidProjection)
	}

	if len(query.OperationalChannelIDs) == 0 {
		return nil, nil
	}

	rows, err := tx.Query(ctx, mustSQL("stale_live_videos.sql"),
		query.OperationalChannelIDs, query.FreshnessBudget.Milliseconds(),
		targetprojection.MaxInputStaleLiveVideoCount+1)
	if err != nil {
		return nil, fmt.Errorf("%w: load stale live videos: %w", targetprojection.ErrInputRead, err)
	}
	defer rows.Close()

	videos := make([]targetprojection.StaleLiveVideo, 0)

	for rows.Next() {
		var video targetprojection.StaleLiveVideo

		if err := rows.Scan(&video.VideoID, &video.ChannelID); err != nil {
			return nil, fmt.Errorf("%w: scan stale live video: %w", targetprojection.ErrInputRead, err)
		}

		videos = append(videos, video)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: read stale live videos: %w", targetprojection.ErrInputRead, err)
	}

	if len(videos) > targetprojection.MaxInputStaleLiveVideoCount {
		return nil, fmt.Errorf("%w: stale live video count exceeds %d", targetprojection.ErrInvalidProjection, targetprojection.MaxInputStaleLiveVideoCount)
	}

	return videos, nil
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
