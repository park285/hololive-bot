package sourceobservation

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/live"
	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/schedule"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func lockScheduleSubject(ctx context.Context, tx dbx.Tx, groupKey string) error {
	if err := lockLiveSubject(ctx, tx, "schedule:"+groupKey); err != nil {
		return fmt.Errorf("lock live subject: %w", err)
	}

	return nil
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
			return schedule.State{}, fmt.Errorf("scan schedule item: %w", err)
		}

		state.Items[schedule.ItemIdentity(item.Provider, &item)] = item
	}

	if err := rows.Err(); err != nil {
		return schedule.State{}, fmt.Errorf("load schedule items: %w", err)
	}

	if err := loadScheduleSessions(ctx, tx, &state, items); err != nil {
		return schedule.State{}, fmt.Errorf("load schedule sessions: %w", err)
	}

	return state, nil
}

func loadScheduleSessions(ctx context.Context, tx dbx.Tx, state *schedule.State, items []schedule.Item) error {
	videoIDs := scheduleVideoIDs(items)
	if len(videoIDs) == 0 {
		return nil
	}

	liveState, err := loadLiveState(ctx, tx, nil, videoIDs)
	if err != nil {
		return fmt.Errorf("load live state: %w", err)
	}

	for videoID := range liveState.Sessions {
		session := liveState.Sessions[videoID]

		state.Sessions[videoID] = schedule.Session{
			VideoID:            session.VideoID,
			ChannelID:          session.ChannelID,
			Status:             session.Status,
			Title:              session.Title,
			ScheduledStartTime: session.ScheduledStartTime,
			LastSeenAt:         session.LastSeenAt,
		}
	}

	return nil
}

func scheduleVideoIDs(items []schedule.Item) []string {
	videoIDs := make([]string, 0, len(items))
	for i := range items {
		if items[i].VideoID != "" {
			videoIDs = append(videoIDs, items[i].VideoID)
		}
	}

	return videoIDs
}

func scanScheduleItem(rows pgx.Rows) (schedule.Item, error) {
	var (
		item     schedule.Item
		provider string
	)

	if err := rows.Scan(
		&item.GroupKey, &provider, &item.ExternalID, &item.VideoID, &item.ChannelID,
		&item.Title, &item.ScheduledAt, &item.EndedAt, &item.IsLive, &item.CollaboTalentNames,
	); err != nil {
		return schedule.Item{}, fmt.Errorf("scan schedule item: %w", err)
	}

	item.Provider = contract.Provider(provider)
	item.CollaboTalentNames = persistedCollaboTalentNames(item.CollaboTalentNames)

	return item, nil
}

func persistScheduleDecision(ctx context.Context, tx dbx.Tx, observation *Observation, decision *schedule.Decision) error {
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
			persistedCollaboTalentNames(item.CollaboTalentNames),
		); err != nil {
			return fmt.Errorf("upsert schedule item: %w", err)
		}
	}

	for i := range decision.Sessions {
		session := decision.Sessions[i]
		if err := upsertLiveSession(ctx, tx, &live.SessionState{
			VideoID:            session.VideoID,
			ChannelID:          session.ChannelID,
			Status:             session.Status,
			Title:              session.Title,
			ScheduledStartTime: session.ScheduledStartTime,
			LastSeenAt:         session.LastSeenAt,
		}); err != nil {
			return fmt.Errorf("upsert live session: %w", err)
		}
	}

	return nil
}

func persistedCollaboTalentNames(names []string) []string {
	if names == nil {
		return []string{}
	}

	cloned := make([]string, len(names))
	copy(cloned, names)

	return cloned
}
