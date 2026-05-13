package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"gorm.io/gorm"
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

func classifyYouTubePollTargetsByActivity(ctx context.Context, db *gorm.DB, targets youtubePollTargets, now time.Time) (youtubeTieredPollTargets, error) {
	out := youtubeTieredPollTargets{
		NotificationChannelIDs: targets.NotificationChannelIDs,
		StatsChannelIDs:        targets.StatsChannelIDs,
	}
	if db == nil || len(targets.NotificationChannelIDs) == 0 {
		out.ActiveNotificationChannelIDs = targets.NotificationChannelIDs
		return out, nil
	}

	lastActivity, err := loadYouTubeChannelLastActivity(ctx, db, targets.NotificationChannelIDs)
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

func appendClassifiedYouTubePollTarget(out *youtubeTieredPollTargets, channelID string, lastActivity map[string]time.Time, activeCutoff time.Time, warmCutoff time.Time) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return
	}
	switch classifyYouTubePollTier(channelID, lastActivity, activeCutoff, warmCutoff) {
	case youtubePollTierActive:
		out.ActiveNotificationChannelIDs = append(out.ActiveNotificationChannelIDs, channelID)
	case youtubePollTierWarm:
		out.WarmNotificationChannelIDs = append(out.WarmNotificationChannelIDs, channelID)
	default:
		out.ColdNotificationChannelIDs = append(out.ColdNotificationChannelIDs, channelID)
	}
}

func classifyYouTubePollTier(channelID string, lastActivity map[string]time.Time, activeCutoff time.Time, warmCutoff time.Time) youtubePollTier {
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

func loadYouTubeChannelLastActivity(ctx context.Context, db *gorm.DB, channelIDs []string) (map[string]time.Time, error) {
	lastActivity := make(map[string]time.Time, len(channelIDs))
	loaders := []func(context.Context, *gorm.DB, []string, map[string]time.Time) error{
		loadYouTubeContentAlarmTrackingActivity,
		loadYouTubeLiveSessionActivity,
		loadYouTubeVideoActivity,
		loadYouTubeCommunityPostActivity,
	}
	for _, load := range loaders {
		if err := load(ctx, db, channelIDs, lastActivity); err != nil {
			return nil, err
		}
	}
	return lastActivity, nil
}

func loadYouTubeContentAlarmTrackingActivity(ctx context.Context, db *gorm.DB, channelIDs []string, lastActivity map[string]time.Time) error {
	if !db.Migrator().HasTable(&domain.YouTubeContentAlarmTracking{}) {
		return nil
	}
	var rows []domain.YouTubeContentAlarmTracking
	err := db.WithContext(ctx).
		Where("channel_id IN ?", channelIDs).
		Find(&rows).Error
	if err != nil {
		return err
	}
	for _, row := range rows {
		mergeChannelActivity(lastActivity, row.ChannelID, latestTimePtr(row.ActualPublishedAt, row.DetectedAt, row.CreatedAt))
	}
	return nil
}

func loadYouTubeLiveSessionActivity(ctx context.Context, db *gorm.DB, channelIDs []string, lastActivity map[string]time.Time) error {
	if !db.Migrator().HasTable(&domain.YouTubeLiveSession{}) {
		return nil
	}
	var rows []domain.YouTubeLiveSession
	err := db.WithContext(ctx).
		Where("channel_id IN ?", channelIDs).
		Find(&rows).Error
	if err != nil {
		return err
	}
	for _, row := range rows {
		mergeChannelActivity(lastActivity, row.ChannelID, latestTimePtr(row.LastSeenAt, row.ScheduledStartTime, row.StartedAt, row.EndedAt))
	}
	return nil
}

func loadYouTubeVideoActivity(ctx context.Context, db *gorm.DB, channelIDs []string, lastActivity map[string]time.Time) error {
	if !db.Migrator().HasTable(&domain.YouTubeVideo{}) {
		return nil
	}
	var rows []domain.YouTubeVideo
	err := db.WithContext(ctx).
		Where("channel_id IN ?", channelIDs).
		Find(&rows).Error
	if err != nil {
		return err
	}
	for _, row := range rows {
		mergeChannelActivity(lastActivity, row.ChannelID, latestTimePtr(row.PublishedAt, row.FirstSeenAt))
	}
	return nil
}

func loadYouTubeCommunityPostActivity(ctx context.Context, db *gorm.DB, channelIDs []string, lastActivity map[string]time.Time) error {
	if !db.Migrator().HasTable(&domain.YouTubeCommunityPost{}) {
		return nil
	}
	var rows []domain.YouTubeCommunityPost
	err := db.WithContext(ctx).
		Where("channel_id IN ?", channelIDs).
		Find(&rows).Error
	if err != nil {
		return err
	}
	for _, row := range rows {
		mergeChannelActivity(lastActivity, row.ChannelID, latestTimePtr(row.PublishedAt, row.FirstSeenAt))
	}
	return nil
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

func latestTimePtr(values ...any) time.Time {
	var latest time.Time
	for _, value := range values {
		candidate := timeCandidate(value)
		if candidate.After(latest) {
			latest = candidate
		}
	}
	return latest
}

func timeCandidate(value any) time.Time {
	switch typed := value.(type) {
	case *time.Time:
		if typed != nil {
			return *typed
		}
	case time.Time:
		return typed
	}
	return time.Time{}
}
