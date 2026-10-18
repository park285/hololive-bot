package config

import (
	"fmt"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/envload"
)

const (
	youtubePlaneMaxPoolConns       = 16
	youtubePlaneMaxConsumerWorkers = 16
	youtubePlaneMaxClaimBatchSize  = 100
)

type YouTubePlaneRetentionConfig struct {
	Enabled              bool
	PolicyApproved       bool
	Interval             time.Duration
	BatchSize            int
	QueueProcessedAge    time.Duration
	QueueDLQAge          time.Duration
	CollisionAge         time.Duration
	ReplayAuditAge       time.Duration
	LiveAbsenceSlotAge   time.Duration
	CheckpointHistoryAge time.Duration
	ProjectionRetiredAge time.Duration
	CommunityPageAge     time.Duration
	VideoListAge         time.Duration
	ShortsListAge        time.Duration
	LiveSnapshotAge      time.Duration
	ChannelProfileAge    time.Duration
	ChannelPhotoAge      time.Duration
	ScheduleSnapshotAge  time.Duration
	ChannelLiveCheckAge  time.Duration
	VideoLiveCheckAge    time.Duration
}

type YouTubePlaneReplayConfig struct {
	Enabled   bool
	Interval  time.Duration
	BatchSize int
}

type YouTubePlaneTargetProjectionConfig struct {
	Interval time.Duration
	Validity time.Duration
}

type YouTubePlaneLiveEndFinalizerConfig struct {
	Enabled  bool
	Interval time.Duration
}

type YouTubePlaneConfig struct {
	Enabled bool

	PostgresPoolMinConns int
	PostgresPoolMaxConns int

	ConsumerWorkers        int
	DBOperationConcurrency int
	ClaimBatchSize         int
	ClaimLease             time.Duration
	ClaimInterval          time.Duration
	TransactionTimeout     time.Duration

	ShutdownTimeout     time.Duration
	Retention           YouTubePlaneRetentionConfig
	Replay              YouTubePlaneReplayConfig
	TargetProjection    YouTubePlaneTargetProjectionConfig
	LiveEndFinalizer    YouTubePlaneLiveEndFinalizerConfig
	ContentAbsenceGrace time.Duration
	LiveEndGrace        time.Duration
	// LiveUnresolvableGrace는 LIVE 영상의 identity_missing이 UNRESOLVABLE_VIDEO 종료 전에 이어져야 하는 최소 지속 시간이다.
	LiveUnresolvableGrace time.Duration
	// TimeFinalizationBacklogBatchSize는 0이면 과거 방송 시각 확정 수집을 중지합니다.
	TimeFinalizationBacklogBatchSize int

	ProfileClearMinObservations int
	ProfileClearStability       time.Duration
	PhotoChangeMinObservations  int
	PhotoChangeStability        time.Duration
}

func DefaultYouTubePlaneConfig() YouTubePlaneConfig {
	return YouTubePlaneConfig{
		PostgresPoolMinConns: 1,
		PostgresPoolMaxConns: 4,
		Retention:            defaultYouTubePlaneRetentionConfig(),
		Replay:               defaultYouTubePlaneReplayConfig(),
		TargetProjection: YouTubePlaneTargetProjectionConfig{
			Interval: 5 * time.Second,
			Validity: time.Hour,
		},
		LiveEndFinalizer: YouTubePlaneLiveEndFinalizerConfig{
			Enabled:  true,
			Interval: time.Minute,
		},
		LiveEndGrace:          2 * time.Minute,
		LiveUnresolvableGrace: 10 * time.Minute,
	}
}

func loadYouTubePlaneConfig() (YouTubePlaneConfig, error) {
	defaults := DefaultYouTubePlaneConfig()
	config := defaults

	if err := loadYouTubePlanePool(&config, &defaults); err != nil {
		return YouTubePlaneConfig{}, fmt.Errorf("load youtube plane pool: %w", err)
	}

	if err := loadYouTubePlaneRetention(&config); err != nil {
		return YouTubePlaneConfig{}, fmt.Errorf("load youtube plane retention: %w", err)
	}

	if err := loadYouTubePlaneReplay(&config); err != nil {
		return YouTubePlaneConfig{}, fmt.Errorf("load youtube plane replay: %w", err)
	}

	if err := loadYouTubePlaneSchedules(&config, &defaults); err != nil {
		return YouTubePlaneConfig{}, fmt.Errorf("load youtube plane schedules: %w", err)
	}

	if err := loadContentAbsenceGrace(&config, &defaults); err != nil {
		return YouTubePlaneConfig{}, err
	}

	if err := loadLiveEndGrace(&config, &defaults); err != nil {
		return YouTubePlaneConfig{}, err
	}

	if err := loadLiveUnresolvableGrace(&config, &defaults); err != nil {
		return YouTubePlaneConfig{}, err
	}

	if err := loadProfilePhotoStability(&config, &defaults); err != nil {
		return YouTubePlaneConfig{}, fmt.Errorf("load profile photo stability: %w", err)
	}

	return config, nil
}

func loadYouTubePlanePool(config, defaults *YouTubePlaneConfig) error {
	var err error

	if config.PostgresPoolMinConns, err = sharedenv.IntE("YOUTUBE_PLANE_POSTGRES_POOL_MIN_CONNS", defaults.PostgresPoolMinConns); err != nil {
		return fmt.Errorf("load postgres pool min conns: %w", err)
	}

	if config.PostgresPoolMaxConns, err = sharedenv.IntE("YOUTUBE_PLANE_POSTGRES_POOL_MAX_CONNS", defaults.PostgresPoolMaxConns); err != nil {
		return fmt.Errorf("load postgres pool max conns: %w", err)
	}

	return nil
}

func loadYouTubePlaneSchedules(config, defaults *YouTubePlaneConfig) error {
	var err error

	if config.TargetProjection.Interval, err = envload.StrictDurationUnitEnv("YOUTUBE_PLANE_TARGET_PROJECTION_INTERVAL_MS", defaults.TargetProjection.Interval, time.Millisecond); err != nil {
		return fmt.Errorf("load target projection interval: %w", err)
	}

	if config.TargetProjection.Validity, err = envload.StrictDurationUnitEnv("YOUTUBE_PLANE_TARGET_PROJECTION_VALIDITY_SECONDS", defaults.TargetProjection.Validity, time.Second); err != nil {
		return fmt.Errorf("load target projection validity: %w", err)
	}

	if config.TimeFinalizationBacklogBatchSize, err = sharedenv.IntE("YOUTUBE_PLANE_TIME_FINALIZATION_BACKLOG_BATCH_SIZE", defaults.TimeFinalizationBacklogBatchSize); err != nil {
		return fmt.Errorf("load time finalization backlog batch size: %w", err)
	}

	if config.LiveEndFinalizer.Enabled, err = sharedenv.BoolE("YOUTUBE_PLANE_LIVE_END_FINALIZER_ENABLED", defaults.LiveEndFinalizer.Enabled); err != nil {
		return fmt.Errorf("load live end finalizer enabled: %w", err)
	}

	if config.LiveEndFinalizer.Interval, err = envload.StrictDurationUnitEnv("YOUTUBE_PLANE_LIVE_END_FINALIZER_INTERVAL_SECONDS", defaults.LiveEndFinalizer.Interval, time.Second); err != nil {
		return fmt.Errorf("load live end finalizer interval: %w", err)
	}

	return nil
}

func loadContentAbsenceGrace(config, defaults *YouTubePlaneConfig) error {
	value, err := envload.StrictDurationUnitEnv("YOUTUBE_PLANE_CONTENT_ABSENCE_GRACE_SECONDS", defaults.ContentAbsenceGrace, time.Second)
	if err != nil {
		return fmt.Errorf("load content absence grace: %w", err)
	}

	config.ContentAbsenceGrace = value

	return nil
}

func loadLiveUnresolvableGrace(config, defaults *YouTubePlaneConfig) error {
	value, err := envload.StrictDurationUnitEnv("YOUTUBE_PLANE_LIVE_UNRESOLVABLE_GRACE_SECONDS", defaults.LiveUnresolvableGrace, time.Second)
	if err != nil {
		return fmt.Errorf("load live unresolvable grace: %w", err)
	}

	config.LiveUnresolvableGrace = value

	return nil
}

func loadLiveEndGrace(config, defaults *YouTubePlaneConfig) error {
	value, err := envload.StrictDurationUnitEnv("YOUTUBE_PLANE_LIVE_END_GRACE_SECONDS", defaults.LiveEndGrace, time.Second)
	if err != nil {
		return fmt.Errorf("load live end grace: %w", err)
	}

	config.LiveEndGrace = value

	return nil
}

func loadProfilePhotoStability(config, defaults *YouTubePlaneConfig) error {
	var err error

	if config.ProfileClearMinObservations, err = sharedenv.IntE(
		"YOUTUBE_PLANE_PROFILE_CLEAR_MIN_OBSERVATIONS",
		defaults.ProfileClearMinObservations,
	); err != nil {
		return fmt.Errorf("load profile clear min observations: %w", err)
	}

	if config.ProfileClearStability, err = envload.StrictDurationUnitEnv(
		"YOUTUBE_PLANE_PROFILE_CLEAR_STABILITY_SECONDS",
		defaults.ProfileClearStability,
		time.Second,
	); err != nil {
		return fmt.Errorf("load profile clear stability: %w", err)
	}

	if config.PhotoChangeMinObservations, err = sharedenv.IntE(
		"YOUTUBE_PLANE_PHOTO_CHANGE_MIN_OBSERVATIONS",
		defaults.PhotoChangeMinObservations,
	); err != nil {
		return fmt.Errorf("load photo change min observations: %w", err)
	}

	if config.PhotoChangeStability, err = envload.StrictDurationUnitEnv(
		"YOUTUBE_PLANE_PHOTO_CHANGE_STABILITY_SECONDS",
		defaults.PhotoChangeStability,
		time.Second,
	); err != nil {
		return fmt.Errorf("load photo change stability: %w", err)
	}

	return nil
}
