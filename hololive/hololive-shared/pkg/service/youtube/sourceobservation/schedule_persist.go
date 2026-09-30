package sourceobservation

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/live"
	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/schedule"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func lockScheduleSubject(ctx context.Context, tx dbx.Tx, groupKey string) error {
	if err := lockLiveSubject(ctx, tx, "schedule:"+groupKey); err != nil {
		return fmt.Errorf("lock live subject: %w", err)
	}

	return nil
}

// loadScheduleState는 reducer가 읽는 live session만 적재한다. 누적 schedule item은 reducer가 읽지 않고,
// youtube_schedule_items의 유일한 writer인 schedule consumer는 lockScheduleSubject advisory lock으로
// 이미 직렬화되므로 item 행을 읽거나 잠그지 않는다.
func loadScheduleState(ctx context.Context, tx dbx.Tx, items []schedule.Item) (schedule.State, error) {
	state := schedule.State{Sessions: map[string]schedule.Session{}}

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
			ScheduleObservedAt: session.ScheduleObservedAt,
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
			LifecycleOrigin:    live.OriginMetadataOnly,
			Title:              session.Title,
			ScheduledStartTime: session.ScheduledStartTime,
			LastSeenAt:         session.LastSeenAt,
			ScheduleObservedAt: session.ScheduleObservedAt,
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
