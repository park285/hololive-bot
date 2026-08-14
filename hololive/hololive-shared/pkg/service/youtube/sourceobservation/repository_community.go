package sourceobservation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func loadCommunityWatermark(
	ctx context.Context,
	q dbx.Querier,
	channelID string,
) (*domain.YouTubeContentWatermark, bool, error) {
	var watermark domain.YouTubeContentWatermark
	var lastContentID *string
	err := q.QueryRow(
		ctx,
		mustSQL("repository_community_watermark_0014_14.sql"),
		channelID,
		domain.WatermarkTypeCommunityPost,
	).Scan(
		&watermark.ChannelID,
		&watermark.WatermarkType,
		&watermark.Initialized,
		&lastContentID,
		&watermark.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load community watermark: %w", err)
	}
	if lastContentID != nil {
		watermark.LastContentID = *lastContentID
	}
	return &watermark, watermark.Initialized, nil
}
