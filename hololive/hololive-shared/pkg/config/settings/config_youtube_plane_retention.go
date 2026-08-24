package settings

import (
	"errors"
	"fmt"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"
)

const (
	youtubePlaneRetentionMaxBatchSize = 1000
	youtubePlaneRetentionMaxAge       = 3650 * 24 * time.Hour
	youtubePlaneRetentionDay          = 24 * time.Hour
)

func defaultYouTubePlaneRetentionConfig() YouTubePlaneRetentionConfig {
	return YouTubePlaneRetentionConfig{
		Interval:              120 * time.Second,
		BatchSize:             youtubePlaneRetentionMaxBatchSize,
		ApplicationAuditGrace: 60 * youtubePlaneRetentionDay,
		CheckpointHistoryAge:  7 * youtubePlaneRetentionDay,
		ChannelStatsAge:       180 * youtubePlaneRetentionDay,
		LiveSnapshotAge:       365 * youtubePlaneRetentionDay,
		ViewerSampleAge:       30 * youtubePlaneRetentionDay,
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
		return fmt.Errorf("read bool env: %w", err)
	}

	if config.Retention.PolicyApproved, err = sharedenv.BoolE("YOUTUBE_PLANE_RETENTION_POLICY_APPROVED", false); err != nil {
		return fmt.Errorf("read bool env: %w", err)
	}

	if config.Retention.Interval, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_INTERVAL_SECONDS",
		defaults.Interval,
		time.Second,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	if config.Retention.BatchSize, err = sharedenv.IntE(
		"YOUTUBE_PLANE_RETENTION_BATCH_SIZE",
		defaults.BatchSize,
	); err != nil {
		return fmt.Errorf("read int env: %w", err)
	}

	if err := loadYouTubePlaneRetentionAges(config, &defaults); err != nil {
		return fmt.Errorf("load youtube plane retention ages: %w", err)
	}

	return nil
}

func loadYouTubePlaneRetentionAges(config *YouTubePlaneConfig, defaults *YouTubePlaneRetentionConfig) error {
	var err error

	if config.Retention.QueueProcessedAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_QUEUE_PROCESSED_DAYS",
		defaults.QueueProcessedAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	if config.Retention.QueueDLQAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_QUEUE_DLQ_DAYS",
		defaults.QueueDLQAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	if config.Retention.CollisionAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_COLLISION_DAYS",
		defaults.CollisionAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	if config.Retention.ReplayAuditAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_REPLAY_AUDIT_DAYS",
		defaults.ReplayAuditAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	if err := loadYouTubePlaneRetentionSupportAges(config, defaults); err != nil {
		return fmt.Errorf("load youtube plane retention support ages: %w", err)
	}

	return nil
}

func loadYouTubePlaneRetentionSupportAges(config *YouTubePlaneConfig, defaults *YouTubePlaneRetentionConfig) error {
	var err error

	if config.Retention.ApplicationAuditGrace, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_APPLICATION_AUDIT_GRACE_DAYS",
		defaults.ApplicationAuditGrace,
		youtubePlaneRetentionDay,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	if config.Retention.CheckpointHistoryAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_CHECKPOINT_HISTORY_DAYS",
		defaults.CheckpointHistoryAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	if config.Retention.ProjectionRetiredAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_PROJECTION_RETIRED_DAYS",
		defaults.ProjectionRetiredAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	if err := loadYouTubePlaneEvidenceAges(config, defaults); err != nil {
		return fmt.Errorf("load youtube plane evidence ages: %w", err)
	}

	return nil
}

func loadYouTubePlaneEvidenceAges(config *YouTubePlaneConfig, defaults *YouTubePlaneRetentionConfig) error {
	if err := loadYouTubePlaneContentEvidenceAges(config, defaults); err != nil {
		return fmt.Errorf("load youtube plane content evidence ages: %w", err)
	}

	if err := loadYouTubePlaneChannelEvidenceAges(config, defaults); err != nil {
		return fmt.Errorf("load youtube plane channel evidence ages: %w", err)
	}

	return nil
}

func loadYouTubePlaneContentEvidenceAges(config *YouTubePlaneConfig, defaults *YouTubePlaneRetentionConfig) error {
	var err error

	if config.Retention.CommunityPageAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_COMMUNITY_PAGE_DAYS",
		defaults.CommunityPageAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	if config.Retention.VideoListAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_VIDEO_LIST_DAYS",
		defaults.VideoListAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	if config.Retention.ShortsListAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_SHORTS_LIST_DAYS",
		defaults.ShortsListAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	if config.Retention.LiveSnapshotAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_LIVE_SNAPSHOT_DAYS",
		defaults.LiveSnapshotAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
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
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	if config.Retention.ViewerSampleAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_VIEWER_SAMPLE_DAYS",
		defaults.ViewerSampleAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	if config.Retention.ChannelProfileAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_CHANNEL_PROFILE_DAYS",
		defaults.ChannelProfileAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	if config.Retention.ChannelPhotoAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_CHANNEL_PHOTO_DAYS",
		defaults.ChannelPhotoAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	if config.Retention.ScheduleSnapshotAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_SCHEDULE_SNAPSHOT_DAYS",
		defaults.ScheduleSnapshotAge,
		youtubePlaneRetentionDay,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	return nil
}

func loadYouTubePlaneReplay(config *YouTubePlaneConfig) error {
	defaults := defaultYouTubePlaneReplayConfig()

	var err error

	if config.Replay.Enabled, err = sharedenv.BoolE("YOUTUBE_PLANE_REPLAY_ENABLED", false); err != nil {
		return fmt.Errorf("read bool env: %w", err)
	}

	if config.Replay.Interval, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_REPLAY_INTERVAL_SECONDS",
		defaults.Interval,
		time.Second,
	); err != nil {
		return fmt.Errorf("strict duration unit env: %w", err)
	}

	if config.Replay.BatchSize, err = sharedenv.IntE(
		"YOUTUBE_PLANE_REPLAY_BATCH_SIZE",
		defaults.BatchSize,
	); err != nil {
		return fmt.Errorf("read int env: %w", err)
	}

	return nil
}

func (c *YouTubePlaneConfig) validateRetention() error {
	if err := validateRetentionLoop("retention", c.Retention.Enabled, c.Retention.Interval, c.Retention.BatchSize); err != nil {
		return fmt.Errorf("validate retention loop: %w", err)
	}

	if err := validateRetentionAges(&c.Retention); err != nil {
		return fmt.Errorf("validate retention ages: %w", err)
	}

	if c.Retention.Enabled && c.Retention.ReplayAuditAge > 0 &&
		c.Retention.ReplayAuditAge < maxEvidenceRetentionAge(&c.Retention) {
		return errors.New("youtube plane replay audit retention must cover the longest evidence retention")
	}

	return nil
}

func (c *YouTubePlaneConfig) validateReplay() error {
	if err := validateRetentionLoop("replay", c.Replay.Enabled, c.Replay.Interval, c.Replay.BatchSize); err != nil {
		return fmt.Errorf("validate retention loop: %w", err)
	}

	return nil
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
		{"application audit grace", cfg.ApplicationAuditGrace},
		{"checkpoint history", cfg.CheckpointHistoryAge},
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
			return fmt.Errorf("validate retention age: %w", err)
		}
	}

	return nil
}
