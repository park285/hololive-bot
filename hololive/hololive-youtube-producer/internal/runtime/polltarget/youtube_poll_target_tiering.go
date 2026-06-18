package polltarget

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type youtubePollTier string

const (
	youtubePollTierActive youtubePollTier = "active"
	youtubePollTierWarm   youtubePollTier = "warm"
	youtubePollTierCold   youtubePollTier = "cold"
)

type youtubeTieredPollTargets struct {
	NotificationChannelIDs       []string
	ActiveNotificationChannelIDs []string
	WarmNotificationChannelIDs   []string
	ColdNotificationChannelIDs   []string
	StatsChannelIDs              []string
}

func classifyYouTubePollTargetsByActivity(ctx context.Context, pool *pgxpool.Pool, targets youtubePollTargets, now time.Time) (youtubeTieredPollTargets, error) {
	if err := ctx.Err(); err != nil {
		return youtubeTieredPollTargets{}, err
	}

	out := youtubeTieredPollTargets{
		NotificationChannelIDs: targets.NotificationChannelIDs,
		StatsChannelIDs:        targets.StatsChannelIDs,
	}
	if pool == nil || len(targets.NotificationChannelIDs) == 0 {
		out.ActiveNotificationChannelIDs = targets.NotificationChannelIDs
		return out, nil
	}

	lastActivity, err := loadYouTubeChannelLastActivity(ctx, pool, targets.NotificationChannelIDs)
	if err != nil {
		return youtubeTieredPollTargets{}, err
	}
	activeCutoff := now.Add(-24 * time.Hour)
	warmCutoff := now.Add(-7 * 24 * time.Hour)
	for _, channelID := range targets.NotificationChannelIDs {
		appendClassifiedYouTubePollTarget(&out, channelID, lastActivity, activeCutoff, warmCutoff)
	}
	return out, nil
}

func appendClassifiedYouTubePollTarget(out *youtubeTieredPollTargets, channelID string, lastActivity map[string]time.Time, activeCutoff, warmCutoff time.Time) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return
	}
	switch classifyYouTubePollTier(channelID, lastActivity, activeCutoff, warmCutoff) {
	case youtubePollTierActive:
		out.ActiveNotificationChannelIDs = append(out.ActiveNotificationChannelIDs, channelID)
	case youtubePollTierWarm:
		out.WarmNotificationChannelIDs = append(out.WarmNotificationChannelIDs, channelID)
	case youtubePollTierCold:
		out.ColdNotificationChannelIDs = append(out.ColdNotificationChannelIDs, channelID)
	}
}

func classifyYouTubePollTier(channelID string, lastActivity map[string]time.Time, activeCutoff, warmCutoff time.Time) youtubePollTier {
	activityAt, ok := lastActivity[channelID]
	switch {
	case ok && !activityAt.Before(activeCutoff):
		return youtubePollTierActive
	case ok && !activityAt.Before(warmCutoff):
		return youtubePollTierWarm
	default:
		return youtubePollTierCold
	}
}

func loadYouTubeChannelLastActivity(ctx context.Context, pool *pgxpool.Pool, channelIDs []string) (map[string]time.Time, error) {
	lastActivity := make(map[string]time.Time, len(channelIDs))
	loaders := []func(context.Context, *pgxpool.Pool, []string, map[string]time.Time) error{
		loadYouTubeContentAlarmTrackingActivity,
		loadYouTubeLiveSessionActivity,
		loadYouTubeVideoActivity,
		loadYouTubeCommunityPostActivity,
	}
	for _, load := range loaders {
		if err := load(ctx, pool, channelIDs, lastActivity); err != nil {
			return nil, err
		}
	}
	return lastActivity, nil
}

func loadYouTubeContentAlarmTrackingActivity(ctx context.Context, pool *pgxpool.Pool, channelIDs []string, lastActivity map[string]time.Time) error {
	return loadYouTubeActivityRows(ctx, pool, "youtube_content_alarm_tracking", channelIDs, lastActivity, `
		SELECT channel_id,
		       MAX(GREATEST(COALESCE(actual_published_at, '-infinity'::timestamptz), detected_at, created_at)) AS activity_at
		FROM youtube_content_alarm_tracking
		WHERE channel_id = ANY($1)
		GROUP BY channel_id
	`)
}

func loadYouTubeLiveSessionActivity(ctx context.Context, pool *pgxpool.Pool, channelIDs []string, lastActivity map[string]time.Time) error {
	return loadYouTubeActivityRows(ctx, pool, "youtube_live_sessions", channelIDs, lastActivity, `
		SELECT channel_id,
		       MAX(GREATEST(
		           last_seen_at,
		           COALESCE(scheduled_start_time, '-infinity'::timestamptz),
		           COALESCE(started_at, '-infinity'::timestamptz),
		           COALESCE(ended_at, '-infinity'::timestamptz)
		       )) AS activity_at
		FROM youtube_live_sessions
		WHERE channel_id = ANY($1)
		GROUP BY channel_id
	`)
}

func loadYouTubeVideoActivity(ctx context.Context, pool *pgxpool.Pool, channelIDs []string, lastActivity map[string]time.Time) error {
	return loadYouTubeActivityRows(ctx, pool, "youtube_videos", channelIDs, lastActivity, `
		SELECT channel_id,
		       MAX(GREATEST(COALESCE(published_at, '-infinity'::timestamptz), first_seen_at)) AS activity_at
		FROM youtube_videos
		WHERE channel_id = ANY($1)
		GROUP BY channel_id
	`)
}

func loadYouTubeCommunityPostActivity(ctx context.Context, pool *pgxpool.Pool, channelIDs []string, lastActivity map[string]time.Time) error {
	return loadYouTubeActivityRows(ctx, pool, "youtube_community_posts", channelIDs, lastActivity, `
		SELECT channel_id,
		       MAX(GREATEST(COALESCE(published_at, '-infinity'::timestamptz), first_seen_at)) AS activity_at
		FROM youtube_community_posts
		WHERE channel_id = ANY($1)
		GROUP BY channel_id
	`)
}

func loadYouTubeActivityRows(ctx context.Context, pool *pgxpool.Pool, tableName string, channelIDs []string, lastActivity map[string]time.Time, query string) error {
	exists, err := tableExists(ctx, pool, tableName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	rows, err := pool.Query(ctx, query, channelIDs)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var channelID string
		var activityAt time.Time
		if err := rows.Scan(&channelID, &activityAt); err != nil {
			return err
		}
		mergeChannelActivity(lastActivity, channelID, activityAt)
	}
	return rows.Err()
}

func tableExists(ctx context.Context, pool *pgxpool.Pool, tableName string) (bool, error) {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, tableName).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func mergeChannelActivity(lastActivity map[string]time.Time, channelID string, activityAt time.Time) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" || activityAt.IsZero() {
		return
	}
	if current, ok := lastActivity[channelID]; !ok || activityAt.After(current) {
		lastActivity[channelID] = activityAt
	}
}
