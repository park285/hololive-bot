package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/content"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func lockContentSubject(ctx context.Context, tx dbx.Tx, kind contract.ObservationKind, subjectKey string) error {
	if _, err := tx.Exec(ctx, mustSQL("repository_content_subject_lock_0034_34.sql"), kind, subjectKey); err != nil {
		return fmt.Errorf("lock content subject: %w", err)
	}

	return nil
}

// loadContentState는 reducer가 이번 관측에 읽는 범위만 적재하고 잠근다. 관측된 영상과 clock 보유 영상,
// 현재 slot과 이번 관측보다 늦은 slot이 그 범위이며, 나머지 이력은 결정에 영향을 주지 않는다.
func loadContentState(
	ctx context.Context,
	tx dbx.Tx,
	kind contract.ObservationKind,
	channelID string,
	evidence *content.Evidence,
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

	if err := loadContentVideos(ctx, tx, &state, evidenceVideoIDs(evidence)); err != nil {
		return content.State{}, fmt.Errorf("load content videos: %w", err)
	}

	if err := loadContentAbsenceSlots(ctx, tx, &state, evidence); err != nil {
		return content.State{}, fmt.Errorf("load content absence slots: %w", err)
	}

	return state, nil
}

func loadContentHead(ctx context.Context, tx dbx.Tx, state *content.State) error {
	var earliestComplete, earliestBaseline *time.Time

	err := tx.QueryRow(ctx, mustSQL("repository_content_channel_head_0040_40.sql"), state.ChannelID, state.Kind).Scan(&earliestComplete, &earliestBaseline)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("load content channel head: %w", err)
	}

	state.EarliestCompleteAt = earliestComplete
	state.EarliestBaselineAt = earliestBaseline

	return nil
}

func evidenceVideoIDs(evidence *content.Evidence) []string {
	ids := make([]string, 0, len(evidence.Videos))
	for i := range evidence.Videos {
		ids = append(ids, evidence.Videos[i].VideoID)
	}

	return ids
}

func loadContentVideos(ctx context.Context, tx dbx.Tx, state *content.State, evidenceIDs []string) error {
	isShort := state.Kind == contract.KindShortsList

	rows, err := tx.Query(ctx, mustSQL("repository_content_videos_0035_35.sql"), state.ChannelID, isShort, evidenceIDs)
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

	if state.Kind == contract.KindVideoList {
		if err := loadKnownElsewhere(ctx, tx, state, evidenceIDs); err != nil {
			return fmt.Errorf("load known elsewhere: %w", err)
		}
	}

	return nil
}

// loadKnownElsewhere는 이번 video_list 영상 중 다른 채널이나 Shorts로 이미 저장된 ID를 읽습니다. 이런 영상은 이 채널 목록에서
// 처음 보여도 새 업로드 근거가 아니므로 NEW_VIDEO 후보에서 뺍니다. 행을 잠그지 않으며 결과는 이번 관측 영상 수로 제한됩니다.
func loadKnownElsewhere(ctx context.Context, tx dbx.Tx, state *content.State, evidenceIDs []string) error {
	if len(evidenceIDs) == 0 {
		return nil
	}

	rows, err := tx.Query(ctx, mustSQL("repository_content_known_elsewhere_0087_87.sql"), state.ChannelID, evidenceIDs)
	if err != nil {
		return fmt.Errorf("query known elsewhere: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var videoID string

		if err := rows.Scan(&videoID); err != nil {
			return fmt.Errorf("scan known elsewhere: %w", err)
		}

		if state.KnownElsewhere == nil {
			state.KnownElsewhere = map[string]struct{}{}
		}

		state.KnownElsewhere[videoID] = struct{}{}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("query known elsewhere: %w", err)
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

func loadContentAbsenceSlots(ctx context.Context, tx dbx.Tx, state *content.State, evidence *content.Evidence) error {
	rows, err := tx.Query(
		ctx,
		mustSQL("repository_content_absence_slots_0038_38.sql"),
		state.ChannelID,
		state.Kind,
		evidence.ScheduledFor,
		evidence.EffectiveAt,
	)
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
