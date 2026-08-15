package settings

import (
	"fmt"
	"time"

	sharedenv "github.com/park285/shared-go/pkg/envutil"
)

const (
	youtubePlaneRetentionMaxBatchSize = 1000
	youtubePlaneRetentionMaxAge       = 3650 * 24 * time.Hour
	youtubePlaneRetentionDay          = 24 * time.Hour
)

func defaultYouTubePlaneRetentionConfig() YouTubePlaneRetentionConfig {
	return YouTubePlaneRetentionConfig{
		Interval:        time.Hour,
		BatchSize:       youtubePlaneRetentionMaxBatchSize,
		ChannelStatsAge: 180 * youtubePlaneRetentionDay,
		LiveSnapshotAge: 365 * youtubePlaneRetentionDay,
		ViewerSampleAge: 30 * youtubePlaneRetentionDay,
	}
}

func defaultYouTubePlaneReplayConfig() YouTubePlaneReplayConfig {
	return YouTubePlaneReplayConfig{
		Interval:  time.Hour,
		BatchSize: youtubePlaneRetentionMaxBatchSize,
	}
}

func loadYouTubePlaneRetention(config *YouTubePlaneConfig) error {
	defaults := defaultYouTubePlaneRetentionConfig()
	var err error
	if config.Retention.Enabled, err = sharedenv.BoolE("YOUTUBE_PLANE_RETENTION_ENABLED", false); err != nil {
		return err
	}
	if config.Retention.PolicyApproved, err = sharedenv.BoolE("YOUTUBE_PLANE_RETENTION_POLICY_APPROVED", false); err != nil {
		return err
	}
	if config.Retention.Interval, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_INTERVAL_SECONDS",
		defaults.Interval,
		time.Second,
	); err != nil {
		return err
	}
	if config.Retention.BatchSize, err = sharedenv.IntE(
		"YOUTUBE_PLANE_RETENTION_BATCH_SIZE",
		defaults.BatchSize,
	); err != nil {
		return err
	}
	return loadYouTubePlaneRetentionAges(config, &defaults)
}

func loadYouTubePlaneRetentionAges(config *YouTubePlaneConfig, defaults *YouTubePlaneRetentionConfig) error {
	var err error
	if config.Retention.QueueProcessedAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_QUEUE_PROCESSED_DAYS",
		defaults.QueueProcessedAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return err
	}
	if config.Retention.QueueDLQAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_QUEUE_DLQ_DAYS",
		defaults.QueueDLQAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return err
	}
	if config.Retention.CollisionAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_COLLISION_DAYS",
		defaults.CollisionAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return err
	}
	if config.Retention.ReplayAuditAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_REPLAY_AUDIT_DAYS",
		defaults.ReplayAuditAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return err
	}
	if config.Retention.ProjectionRetiredAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_PROJECTION_RETIRED_DAYS",
		defaults.ProjectionRetiredAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return err
	}
	return loadYouTubePlaneEvidenceAges(config, defaults)
}

func loadYouTubePlaneEvidenceAges(config *YouTubePlaneConfig, defaults *YouTubePlaneRetentionConfig) error {
	if err := loadYouTubePlaneContentEvidenceAges(config, defaults); err != nil {
		return err
	}
	return loadYouTubePlaneChannelEvidenceAges(config, defaults)
}

func loadYouTubePlaneContentEvidenceAges(config *YouTubePlaneConfig, defaults *YouTubePlaneRetentionConfig) error {
	var err error
	if config.Retention.CommunityPageAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_COMMUNITY_PAGE_DAYS",
		defaults.CommunityPageAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return err
	}
	if config.Retention.VideoListAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_VIDEO_LIST_DAYS",
		defaults.VideoListAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return err
	}
	if config.Retention.ShortsListAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_SHORTS_LIST_DAYS",
		defaults.ShortsListAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return err
	}
	if config.Retention.LiveSnapshotAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_LIVE_SNAPSHOT_DAYS",
		defaults.LiveSnapshotAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return err
	}
	return nil
}

func loadYouTubePlaneChannelEvidenceAges(config *YouTubePlaneConfig, defaults *YouTubePlaneRetentionConfig) error {
	var err error
	if config.Retention.ChannelStatsAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_CHANNEL_STATS_DAYS",
		defaults.ChannelStatsAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return err
	}
	if config.Retention.ViewerSampleAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_VIEWER_SAMPLE_DAYS",
		defaults.ViewerSampleAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return err
	}
	if config.Retention.ChannelProfileAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_CHANNEL_PROFILE_DAYS",
		defaults.ChannelProfileAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return err
	}
	if config.Retention.ChannelPhotoAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_CHANNEL_PHOTO_DAYS",
		defaults.ChannelPhotoAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return err
	}
	if config.Retention.ScheduleSnapshotAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_SCHEDULE_SNAPSHOT_DAYS",
		defaults.ScheduleSnapshotAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return err
	}
	return nil
}

func loadYouTubePlaneReplay(config *YouTubePlaneConfig) error {
	defaults := defaultYouTubePlaneReplayConfig()
	var err error
	if config.Replay.Enabled, err = sharedenv.BoolE("YOUTUBE_PLANE_REPLAY_ENABLED", false); err != nil {
		return err
	}
	if config.Replay.Interval, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_REPLAY_INTERVAL_SECONDS",
		defaults.Interval,
		time.Second,
	); err != nil {
		return err
	}
	if config.Replay.BatchSize, err = sharedenv.IntE(
		"YOUTUBE_PLANE_REPLAY_BATCH_SIZE",
		defaults.BatchSize,
	); err != nil {
		return err
	}
	return nil
}

func (c *YouTubePlaneConfig) validateRetention() error {
	if err := validateRetentionLoop("retention", c.Retention.Enabled, c.Retention.Interval, c.Retention.BatchSize); err != nil {
		return err
	}
	if err := validateRetentionAges(&c.Retention); err != nil {
		return err
	}
	if c.Retention.Enabled && c.Retention.ReplayAuditAge > 0 &&
		c.Retention.ReplayAuditAge < maxEvidenceRetentionAge(&c.Retention) {
		return fmt.Errorf("youtube plane replay audit retention must cover the longest evidence retention")
	}
	return nil
}

func (c *YouTubePlaneConfig) validateReplay() error {
	return validateRetentionLoop("replay", c.Replay.Enabled, c.Replay.Interval, c.Replay.BatchSize)
}

func validateRetentionLoop(name string, enabled bool, interval time.Duration, batchSize int) error {
	if enabled && interval <= 0 {
		return fmt.Errorf("youtube plane %s interval must be positive when enabled", name)
	}
	if batchSize < 1 || batchSize > youtubePlaneRetentionMaxBatchSize {
		return fmt.Errorf("youtube plane %s batch size must be between 1 and %d", name, youtubePlaneRetentionMaxBatchSize)
	}
	return nil
}

func validateRetentionAges(cfg *YouTubePlaneRetentionConfig) error {
	ages := []struct {
		name string
		age  time.Duration
	}{
		{"queue processed", cfg.QueueProcessedAge},
		{"queue dlq", cfg.QueueDLQAge},
		{"collision", cfg.CollisionAge},
		{"replay audit", cfg.ReplayAuditAge},
		{"retired projection", cfg.ProjectionRetiredAge},
		{"community page", cfg.CommunityPageAge},
		{"video list", cfg.VideoListAge},
		{"shorts list", cfg.ShortsListAge},
		{"channel stats", cfg.ChannelStatsAge},
		{"live snapshot", cfg.LiveSnapshotAge},
		{"viewer sample", cfg.ViewerSampleAge},
		{"channel profile", cfg.ChannelProfileAge},
		{"channel photo", cfg.ChannelPhotoAge},
		{"schedule snapshot", cfg.ScheduleSnapshotAge},
	}
	for _, item := range ages {
		if err := validateRetentionAge(item.name, item.age); err != nil {
			return err
		}
	}
	return nil
}

func maxEvidenceRetentionAge(cfg *YouTubePlaneRetentionConfig) time.Duration {
	ages := []time.Duration{
		cfg.CommunityPageAge,
		cfg.VideoListAge,
		cfg.ShortsListAge,
		cfg.LiveSnapshotAge,
		cfg.ViewerSampleAge,
		cfg.ChannelStatsAge,
		cfg.ChannelProfileAge,
		cfg.ChannelPhotoAge,
		cfg.ScheduleSnapshotAge,
	}
	var maxAge time.Duration
	for _, age := range ages {
		if age > maxAge {
			maxAge = age
		}
	}
	return maxAge
}

func validateRetentionAge(name string, age time.Duration) error {
	if age < 0 || age > youtubePlaneRetentionMaxAge {
		return fmt.Errorf("youtube plane %s retention must be between 0 and 3650 days", name)
	}
	return nil
}

func (c *YouTubePlaneConfig) validateProductionRetention(environment string) error {
	if !isProductionEnvironment(environment) || !c.Enabled {
		return nil
	}
	if !c.Retention.Enabled {
		return fmt.Errorf("YOUTUBE_PLANE_RETENTION_ENABLED=true is required in production")
	}
	if !c.Retention.PolicyApproved {
		return fmt.Errorf("YOUTUBE_PLANE_RETENTION_POLICY_APPROVED=true is required in production")
	}
	ages := []struct {
		key string
		age time.Duration
	}{
		{"YOUTUBE_PLANE_RETENTION_QUEUE_PROCESSED_DAYS", c.Retention.QueueProcessedAge},
		{"YOUTUBE_PLANE_RETENTION_QUEUE_DLQ_DAYS", c.Retention.QueueDLQAge},
		{"YOUTUBE_PLANE_RETENTION_COLLISION_DAYS", c.Retention.CollisionAge},
		{"YOUTUBE_PLANE_RETENTION_REPLAY_AUDIT_DAYS", c.Retention.ReplayAuditAge},
		{"YOUTUBE_PLANE_RETENTION_PROJECTION_RETIRED_DAYS", c.Retention.ProjectionRetiredAge},
		{"YOUTUBE_PLANE_RETENTION_COMMUNITY_PAGE_DAYS", c.Retention.CommunityPageAge},
		{"YOUTUBE_PLANE_RETENTION_VIDEO_LIST_DAYS", c.Retention.VideoListAge},
		{"YOUTUBE_PLANE_RETENTION_SHORTS_LIST_DAYS", c.Retention.ShortsListAge},
		{"YOUTUBE_PLANE_RETENTION_LIVE_SNAPSHOT_DAYS", c.Retention.LiveSnapshotAge},
		{"YOUTUBE_PLANE_RETENTION_VIEWER_SAMPLE_DAYS", c.Retention.ViewerSampleAge},
		{"YOUTUBE_PLANE_RETENTION_CHANNEL_STATS_DAYS", c.Retention.ChannelStatsAge},
		{"YOUTUBE_PLANE_RETENTION_CHANNEL_PROFILE_DAYS", c.Retention.ChannelProfileAge},
		{"YOUTUBE_PLANE_RETENTION_CHANNEL_PHOTO_DAYS", c.Retention.ChannelPhotoAge},
		{"YOUTUBE_PLANE_RETENTION_SCHEDULE_SNAPSHOT_DAYS", c.Retention.ScheduleSnapshotAge},
	}
	for _, item := range ages {
		if item.age <= 0 {
			return fmt.Errorf("%s must be positive in production", item.key)
		}
	}
	return nil
}
