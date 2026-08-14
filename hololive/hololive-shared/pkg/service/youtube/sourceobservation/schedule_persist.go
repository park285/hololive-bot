package sourceobservation

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/live"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/schedule"
)

func lockScheduleSubject(ctx context.Context, tx dbx.Tx, groupKey string) error {
	return lockLiveSubject(ctx, tx, "schedule:"+groupKey)
}

func loadScheduleState(ctx context.Context, tx dbx.Tx, groupKey string, items []schedule.Item) (schedule.State, error) {
	state := schedule.State{Items: map[string]schedule.Item{}, Sessions: map[string]schedule.Session{}}
	rows, err := tx.Query(ctx, mustSQL("repository_schedule_items_0058_58.sql"), groupKey)
	if err != nil {
		return schedule.State{}, fmt.Errorf("load schedule items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanScheduleItem(rows)
		if err != nil {
			return schedule.State{}, err
		}
		state.Items[schedule.ItemIdentity(item.Provider, item)] = item
	}
	if err := rows.Err(); err != nil {
		return schedule.State{}, err
	}
	videoIDs := make([]string, 0, len(items))
	for i := range items {
		if items[i].VideoID != "" {
			videoIDs = append(videoIDs, items[i].VideoID)
		}
	}
	if len(videoIDs) == 0 {
		return state, nil
	}
	liveState, err := loadLiveState(ctx, tx, nil, videoIDs)
	if err != nil {
		return schedule.State{}, err
	}
	for videoID, session := range liveState.Sessions {
		state.Sessions[videoID] = schedule.Session{
			VideoID:            session.VideoID,
			ChannelID:          session.ChannelID,
			Status:             session.Status,
			Title:              session.Title,
			ScheduledStartTime: session.ScheduledStartTime,
			LastSeenAt:         session.LastSeenAt,
		}
	}
	return state, nil
}

func scanScheduleItem(rows pgx.Rows) (schedule.Item, error) {
	var item schedule.Item
	var provider string
	if err := rows.Scan(
		&item.GroupKey, &provider, &item.ExternalID, &item.VideoID, &item.ChannelID,
		&item.Title, &item.ScheduledAt, &item.EndedAt, &item.IsLive,
	); err != nil {
		return schedule.Item{}, fmt.Errorf("scan schedule item: %w", err)
	}
	item.Provider = contract.Provider(provider)
	return item, nil
}

func persistScheduleDecision(ctx context.Context, tx dbx.Tx, observation Observation, decision schedule.Decision) error {
	for i := range decision.Items {
		item := decision.Items[i]
		if _, err := tx.Exec(
			ctx,
			mustSQL("repository_schedule_item_upsert_0059_59.sql"),
			item.GroupKey,
			observation.Provider,
			item.ExternalID,
			item.VideoID,
			item.ChannelID,
			item.Title,
			item.ScheduledAt,
			item.EndedAt,
			item.IsLive,
		); err != nil {
			return fmt.Errorf("upsert schedule item: %w", err)
		}
	}
	for i := range decision.Sessions {
		session := decision.Sessions[i]
		if err := upsertLiveSession(ctx, tx, live.SessionState{
			VideoID:            session.VideoID,
			ChannelID:          session.ChannelID,
			Status:             session.Status,
			Title:              session.Title,
			ScheduledStartTime: session.ScheduledStartTime,
			LastSeenAt:         session.LastSeenAt,
		}); err != nil {
			return err
		}
	}
	return nil
}
