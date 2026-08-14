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
	return loadYouTubePlaneRetentionAges(config, defaults)
}

func loadYouTubePlaneRetentionAges(config *YouTubePlaneConfig, defaults YouTubePlaneRetentionConfig) error {
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
	return loadYouTubePlaneEvidenceAges(config, defaults)
}

func loadYouTubePlaneEvidenceAges(config *YouTubePlaneConfig, defaults YouTubePlaneRetentionConfig) error {
	var err error
	if config.Retention.ChannelStatsAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_CHANNEL_STATS_DAYS",
		defaults.ChannelStatsAge,
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
	if config.Retention.ViewerSampleAge, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_VIEWER_SAMPLE_DAYS",
		defaults.ViewerSampleAge,
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

func (c YouTubePlaneConfig) validateRetention() error {
	if err := validateRetentionLoop("retention", c.Retention.Enabled, c.Retention.Interval, c.Retention.BatchSize); err != nil {
		return err
	}
	return validateRetentionAges(c.Retention)
}

func (c YouTubePlaneConfig) validateReplay() error {
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

func validateRetentionAges(cfg YouTubePlaneRetentionConfig) error {
	ages := []struct {
		name string
		age  time.Duration
	}{
		{"queue processed", cfg.QueueProcessedAge},
		{"queue dlq", cfg.QueueDLQAge},
		{"collision", cfg.CollisionAge},
		{"replay audit", cfg.ReplayAuditAge},
		{"channel stats", cfg.ChannelStatsAge},
		{"live snapshot", cfg.LiveSnapshotAge},
		{"viewer sample", cfg.ViewerSampleAge},
	}
	for _, item := range ages {
		if err := validateRetentionAge(item.name, item.age); err != nil {
			return err
		}
	}
	return nil
}

func validateRetentionAge(name string, age time.Duration) error {
	if age < 0 || age > youtubePlaneRetentionMaxAge {
		return fmt.Errorf("youtube plane %s retention must be between 0 and 3650 days", name)
	}
	return nil
}
