package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/content"
)

func lockContentSubject(ctx context.Context, tx dbx.Tx, kind contract.ObservationKind, subjectKey string) error {
	if _, err := tx.Exec(ctx, mustSQL("repository_content_subject_lock_0034_34.sql"), kind, subjectKey); err != nil {
		return fmt.Errorf("lock content subject: %w", err)
	}

	return nil
}

func loadContentState(
	ctx context.Context,
	tx dbx.Tx,
	kind contract.ObservationKind,
	channelID string,
) (content.State, error) {
	state := content.State{ChannelID: channelID, Kind: kind, Videos: map[string]content.EntityState{}}

	watermark, err := loadTypedWatermark(ctx, tx, channelID, watermarkTypeFor(kind))
	if err != nil {
		return content.State{}, fmt.Errorf("load typed watermark: %w", err)
	}

	state.Initialized = watermark.Initialized
	state.LastContentID = watermark.LastContentID

	if err := loadContentHead(ctx, tx, &state); err != nil {
		return content.State{}, fmt.Errorf("load content head: %w", err)
	}

	if err := loadContentVideos(ctx, tx, &state); err != nil {
		return content.State{}, fmt.Errorf("load content videos: %w", err)
	}

	if err := loadContentAbsenceSlots(ctx, tx, &state); err != nil {
		return content.State{}, fmt.Errorf("load content absence slots: %w", err)
	}

	return state, nil
}

func loadContentHead(ctx context.Context, tx dbx.Tx, state *content.State) error {
	var earliest *time.Time

	err := tx.QueryRow(ctx, mustSQL("repository_content_channel_head_0040_40.sql"), state.ChannelID, state.Kind).Scan(&earliest)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("load content channel head: %w", err)
	}

	state.EarliestCompleteAt = earliest

	return nil
}

func loadContentVideos(ctx context.Context, tx dbx.Tx, state *content.State) error {
	rows, err := tx.Query(ctx, mustSQL("repository_content_videos_0035_35.sql"), state.ChannelID, state.Kind == contract.KindShortsList)
	if err != nil {
		return fmt.Errorf("load content videos: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0, 16)

	for rows.Next() {
		entity, err := scanContentVideo(rows)
		if err != nil {
			return fmt.Errorf("scan content video: %w", err)
		}

		state.Videos[entity.VideoID] = content.EntityState{Entity: entity}
		ids = append(ids, entity.VideoID)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("load content videos: %w", err)
	}

	if err := loadContentClocks(ctx, tx, state, ids); err != nil {
		return fmt.Errorf("load content clocks: %w", err)
	}

	return nil
}

func scanContentVideo(rows pgx.Rows) (content.Entity, error) {
	var entity content.Entity

	if err := rows.Scan(&entity.VideoID, &entity.ChannelID, &entity.Title, &entity.PublishedAt, &entity.IsShort); err != nil {
		return content.Entity{}, fmt.Errorf("scan content video: %w", err)
	}

	return entity, nil
}

func loadContentClocks(ctx context.Context, tx dbx.Tx, state *content.State, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	rows, err := tx.Query(ctx, mustSQL("repository_content_clocks_0036_36.sql"), ids)
	if err != nil {
		return fmt.Errorf("load content clocks: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		clock, err := scanContentClock(rows, state.Kind)
		if err != nil {
			return fmt.Errorf("scan content clock: %w", err)
		}

		existing := state.Videos[clock.VideoID]

		clock.Entity = existing.Entity
		state.Videos[clock.VideoID] = clock
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("load content clocks: %w", err)
	}

	return nil
}

func loadContentAbsenceSlots(ctx context.Context, tx dbx.Tx, state *content.State) error {
	rows, err := tx.Query(ctx, mustSQL("repository_content_absence_slots_0038_38.sql"), state.ChannelID, state.Kind)
	if err != nil {
		return fmt.Errorf("load content absence slots: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		slot, err := scanAbsenceSlot(rows, state.Kind)
		if err != nil {
			return fmt.Errorf("scan absence slot: %w", err)
		}

		state.AbsenceSlots = append(state.AbsenceSlots, slot)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("load content absence slots: %w", err)
	}

	return nil
}

func watermarkTypeFor(kind contract.ObservationKind) domain.WatermarkType {
	if kind == contract.KindShortsList {
		return domain.WatermarkTypeShort
	}

	return domain.WatermarkTypeVideo
}
