package config

import (
	"errors"
	"fmt"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

const (
	youtubePlaneRetentionMaxBatchSize = 1000
	youtubePlaneRetentionMaxAge       = 3650 * 24 * time.Hour
	youtubePlaneRetentionDay          = 24 * time.Hour
)

func defaultYouTubePlaneRetentionConfig() YouTubePlaneRetentionConfig {
	return YouTubePlaneRetentionConfig{
		Interval:             120 * time.Second,
		BatchSize:            youtubePlaneRetentionMaxBatchSize,
		QueueProcessedAge:    youtubePlaneRetentionDay,
		QueueDLQAge:          14 * youtubePlaneRetentionDay,
		CollisionAge:         30 * youtubePlaneRetentionDay,
		ReplayAuditAge:       30 * youtubePlaneRetentionDay,
		LiveAbsenceSlotAge:   14 * youtubePlaneRetentionDay,
		CheckpointHistoryAge: 2 * youtubePlaneRetentionDay,
		ProjectionRetiredAge: 2 * youtubePlaneRetentionDay,
		CommunityPageAge:     7 * youtubePlaneRetentionDay,
		VideoListAge:         7 * youtubePlaneRetentionDay,
		ShortsListAge:        7 * youtubePlaneRetentionDay,
		LiveSnapshotAge:      7 * youtubePlaneRetentionDay,
		ChannelProfileAge:    30 * youtubePlaneRetentionDay,
		ChannelPhotoAge:      30 * youtubePlaneRetentionDay,
		ScheduleSnapshotAge:  14 * youtubePlaneRetentionDay,
		ChannelLiveCheckAge:  2 * youtubePlaneRetentionDay,
		VideoLiveCheckAge:    2 * youtubePlaneRetentionDay,
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
		return fmt.Errorf("load retention enabled: %w", err)
	}

	if config.Retention.PolicyApproved, err = sharedenv.BoolE("YOUTUBE_PLANE_RETENTION_POLICY_APPROVED", false); err != nil {
		return fmt.Errorf("load retention policy approval: %w", err)
	}

	if config.Retention.Interval, err = envload.StrictDurationUnitEnv(
		"YOUTUBE_PLANE_RETENTION_INTERVAL_SECONDS",
		defaults.Interval,
		time.Second,
	); err != nil {
		return fmt.Errorf("load retention interval: %w", err)
	}

	if config.Retention.BatchSize, err = sharedenv.IntE(
		"YOUTUBE_PLANE_RETENTION_BATCH_SIZE",
		defaults.BatchSize,
	); err != nil {
		return fmt.Errorf("load retention batch size: %w", err)
	}

	if err := loadYouTubePlaneRetentionAges(config, &defaults); err != nil {
		return err
	}

	return nil
}

// youtubePlaneRetentionAgeEnv는 일 단위 보존 기간 env 하나와 그 값을 담는 필드를 잇는다.
type youtubePlaneRetentionAgeEnv struct {
	key string
	age *time.Duration
}

// youtubePlaneRetentionAgeEnvs는 보존 기간 env와 필드의 대응을 한 곳에서 정한다. 적재 함수와 production 검증이 같은 표를 쓴다.
func youtubePlaneRetentionAgeEnvs(retention *YouTubePlaneRetentionConfig) []youtubePlaneRetentionAgeEnv {
	return []youtubePlaneRetentionAgeEnv{
		{"YOUTUBE_PLANE_RETENTION_QUEUE_PROCESSED_DAYS", &retention.QueueProcessedAge},
		{"YOUTUBE_PLANE_RETENTION_QUEUE_DLQ_DAYS", &retention.QueueDLQAge},
		{"YOUTUBE_PLANE_RETENTION_COLLISION_DAYS", &retention.CollisionAge},
		{"YOUTUBE_PLANE_RETENTION_REPLAY_AUDIT_DAYS", &retention.ReplayAuditAge},
		{"YOUTUBE_PLANE_RETENTION_LIVE_ABSENCE_SLOTS_DAYS", &retention.LiveAbsenceSlotAge},
		{"YOUTUBE_PLANE_RETENTION_CHECKPOINT_HISTORY_DAYS", &retention.CheckpointHistoryAge},
		{"YOUTUBE_PLANE_RETENTION_PROJECTION_RETIRED_DAYS", &retention.ProjectionRetiredAge},
		{"YOUTUBE_PLANE_RETENTION_COMMUNITY_PAGE_DAYS", &retention.CommunityPageAge},
		{"YOUTUBE_PLANE_RETENTION_VIDEO_LIST_DAYS", &retention.VideoListAge},
		{"YOUTUBE_PLANE_RETENTION_SHORTS_LIST_DAYS", &retention.ShortsListAge},
		{"YOUTUBE_PLANE_RETENTION_LIVE_SNAPSHOT_DAYS", &retention.LiveSnapshotAge},
		{"YOUTUBE_PLANE_RETENTION_CHANNEL_PROFILE_DAYS", &retention.ChannelProfileAge},
		{"YOUTUBE_PLANE_RETENTION_CHANNEL_PHOTO_DAYS", &retention.ChannelPhotoAge},
		{"YOUTUBE_PLANE_RETENTION_SCHEDULE_SNAPSHOT_DAYS", &retention.ScheduleSnapshotAge},
		{"YOUTUBE_PLANE_RETENTION_CHANNEL_LIVE_CHECK_DAYS", &retention.ChannelLiveCheckAge},
		{"YOUTUBE_PLANE_RETENTION_VIDEO_LIVE_CHECK_DAYS", &retention.VideoLiveCheckAge},
	}
}

func loadYouTubePlaneRetentionAges(config *YouTubePlaneConfig, defaults *YouTubePlaneRetentionConfig) error {
	targets := youtubePlaneRetentionAgeEnvs(&config.Retention)
	fallbacks := youtubePlaneRetentionAgeEnvs(defaults)

	for i, target := range targets {
		value, err := envload.StrictDurationUnitEnv(target.key, *fallbacks[i].age, youtubePlaneRetentionDay)
		if err != nil {
			return fmt.Errorf("load retention age: %w", err)
		}

		*target.age = value
	}

	return nil
}

func loadYouTubePlaneReplay(config *YouTubePlaneConfig) error {
	defaults := defaultYouTubePlaneReplayConfig()

	var err error

	if config.Replay.Enabled, err = sharedenv.BoolE("YOUTUBE_PLANE_REPLAY_ENABLED", false); err != nil {
		return fmt.Errorf("load replay enabled: %w", err)
	}

	if config.Replay.Interval, err = envload.StrictDurationUnitEnv(
		"YOUTUBE_PLANE_REPLAY_INTERVAL_SECONDS",
		defaults.Interval,
		time.Second,
	); err != nil {
		return fmt.Errorf("load replay interval: %w", err)
	}

	if config.Replay.BatchSize, err = sharedenv.IntE(
		"YOUTUBE_PLANE_REPLAY_BATCH_SIZE",
		defaults.BatchSize,
	); err != nil {
		return fmt.Errorf("load replay batch size: %w", err)
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
		{"live absence slots", cfg.LiveAbsenceSlotAge},
		{"checkpoint history", cfg.CheckpointHistoryAge},
		{"retired projection", cfg.ProjectionRetiredAge},
		{"community page", cfg.CommunityPageAge},
		{"video list", cfg.VideoListAge},
		{"shorts list", cfg.ShortsListAge},
		{"live snapshot", cfg.LiveSnapshotAge},
		{"channel profile", cfg.ChannelProfileAge},
		{"channel photo", cfg.ChannelPhotoAge},
		{"schedule snapshot", cfg.ScheduleSnapshotAge},
		{"channel live check", cfg.ChannelLiveCheckAge},
		{"video live check", cfg.VideoLiveCheckAge},
	}
	for _, item := range ages {
		if err := validateRetentionAge(item.name, item.age); err != nil {
			return fmt.Errorf("validate retention age: %w", err)
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
		cfg.ChannelProfileAge,
		cfg.ChannelPhotoAge,
		cfg.ScheduleSnapshotAge,
		cfg.ChannelLiveCheckAge,
		cfg.VideoLiveCheckAge,
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
	if !runtimepolicy.IsProduction(environment) || !c.Enabled {
		return nil
	}

	if !c.Retention.Enabled {
		return errors.New("YOUTUBE_PLANE_RETENTION_ENABLED=true is required in production")
	}

	if !c.Retention.PolicyApproved {
		return errors.New("YOUTUBE_PLANE_RETENTION_POLICY_APPROVED=true is required in production")
	}

	for _, item := range youtubePlaneRetentionAgeEnvs(&c.Retention) {
		if *item.age <= 0 {
			return fmt.Errorf("%s must be positive in production", item.key)
		}
	}

	return nil
}
