package sourceobservation

import (
	"context"
	"fmt"

	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	"github.com/kapu/hololive-api/internal/youtube/reconcile/schedule"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const scheduleStatementBatchSize = 128

func lockScheduleSubject(ctx context.Context, tx dbx.Tx, groupKey string) error {
	return lockLiveSubject(ctx, tx, "schedule:"+groupKey)
}

func loadScheduleState(ctx context.Context, tx dbx.Tx, items []schedule.Item) (schedule.State, map[string]live.SessionState, error) {
	state := schedule.State{Sessions: map[string]schedule.Session{}}
	stored := map[string]live.SessionState{}

	if err := loadScheduleSessions(ctx, tx, &state, stored, items); err != nil {
		return schedule.State{}, nil, fmt.Errorf("load schedule sessions: %w", err)
	}

	return state, stored, nil
}

func loadScheduleSessions(
	ctx context.Context,
	tx dbx.Tx,
	state *schedule.State,
	stored map[string]live.SessionState,
	items []schedule.Item,
) error {
	videoIDs := scheduleVideoIDs(items)
	if len(videoIDs) == 0 {
		return nil
	}

	rows, err := pgxdb.Query(ctx, tx, repositoryScheduleSessionsSQL, videoIDs)
	if err != nil {
		return fmt.Errorf("load schedule sessions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		session, err := scanLiveSession(rows)
		if err != nil {
			return fmt.Errorf("scan schedule session: %w", err)
		}

		stored[session.VideoID] = session
		state.Sessions[session.VideoID] = schedule.Session{
			VideoID: session.VideoID, ChannelID: session.ChannelID, Status: session.Status, Title: session.Title,
			ScheduledStartTime: session.ScheduledStartTime, LastSeenAt: session.LastSeenAt,
			ScheduleObservedAt: session.ScheduleObservedAt, TitleObservedAt: session.TitleObservedAt,
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("load schedule session rows: %w", err)
	}

	missing := make([]string, 0)

	for _, videoID := range videoIDs {
		if _, present := state.Sessions[videoID]; !present {
			missing = append(missing, videoID)
		}
	}

	if err := loadScheduleHeads(ctx, tx, state, missing); err != nil {
		return fmt.Errorf("load schedule heads: %w", err)
	}

	return nil
}

func loadScheduleHeads(ctx context.Context, tx dbx.Tx, state *schedule.State, videoIDs []string) error {
	if len(videoIDs) == 0 {
		return nil
	}

	rows, err := pgxdb.Query(ctx, tx, repositoryScheduleHeadsSQL, videoIDs)
	if err != nil {
		return fmt.Errorf("load schedule head statuses: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var videoID, status string

		if err := rows.Scan(&videoID, &status); err != nil {
			return fmt.Errorf("scan schedule head status: %w", err)
		}

		state.Sessions[videoID] = schedule.Session{VideoID: videoID, Status: domain.LiveStatus(status)}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("load schedule head rows: %w", err)
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

func persistScheduleDecision(
	ctx context.Context,
	tx dbx.Tx,
	observation *Observation,
	stored map[string]live.SessionState,
	decision *schedule.Decision,
) error {
	total := len(decision.Items) + len(decision.Sessions)
	for start := 0; start < total; start += scheduleStatementBatchSize {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("persist schedule decision at %d: %w", start, err)
		}

		end := min(start+scheduleStatementBatchSize, total)
		statements := make([]dbx.Statement, 0, end-start)

		for i := start; i < end; i++ {
			if i < len(decision.Items) {
				statements = append(statements, scheduleItemStatement(observation, &decision.Items[i]))
				continue
			}

			session := &decision.Sessions[i-len(decision.Items)]
			if session.ChannelID == "" {
				continue
			}

			next := scheduleSessionState(stored, session)
			if statement, write := liveSessionWrite(stored, &next); write {
				statements = append(statements, statement)
			}
		}

		if err := execLiveSessionStatements(ctx, tx, statements); err != nil {
			return fmt.Errorf("persist schedule decision batch at %d: %w", start, err)
		}
	}

	return nil
}

func scheduleDecisionRuntimeVideoIDs(decision *schedule.Decision) []string {
	videoIDs := make([]string, len(decision.Sessions))
	for i := range decision.Sessions {
		videoIDs[i] = decision.Sessions[i].VideoID
	}

	return videoIDs
}

func scheduleItemStatement(observation *Observation, item *schedule.Item) dbx.Statement {
	return dbx.Statement{
		Operation: "upsert schedule item",
		SQL:       scheduleItemStatementSQL,
		Args: []any{
			item.GroupKey, observation.Provider, item.ExternalID, item.VideoID,
			item.ChannelID, item.Title, item.ScheduledAt, item.EndedAt, item.IsLive,
			persistedCollaboTalentNames(item.CollaboTalentNames), observation.EffectiveAt,
		},
	}
}

func scheduleSessionState(stored map[string]live.SessionState, session *schedule.Session) live.SessionState {
	next, ok := stored[session.VideoID]
	if !ok || !next.Present {
		next = live.SessionState{VideoID: session.VideoID, LifecycleOrigin: live.OriginMetadataOnly}
	}

	next.ChannelID = session.ChannelID
	next.Status = session.Status
	next.Title = session.Title
	next.ScheduledStartTime = session.ScheduledStartTime
	next.LastSeenAt = session.LastSeenAt
	next.ScheduleObservedAt = session.ScheduleObservedAt
	next.TitleObservedAt = session.TitleObservedAt

	return next
}

func persistedCollaboTalentNames(names []string) []string {
	if names == nil {
		return []string{}
	}

	cloned := make([]string, len(names))
	copy(cloned, names)

	return cloned
}
