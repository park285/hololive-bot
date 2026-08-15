package settings

import (
	"fmt"
	"time"

	sharedenv "github.com/park285/shared-go/pkg/envutil"
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
	ProjectionRetiredAge time.Duration
	CommunityPageAge     time.Duration
	VideoListAge         time.Duration
	ShortsListAge        time.Duration
	LiveSnapshotAge      time.Duration
	ViewerSampleAge      time.Duration
	ChannelStatsAge      time.Duration
	ChannelProfileAge    time.Duration
	ChannelPhotoAge      time.Duration
	ScheduleSnapshotAge  time.Duration
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

	ProfileClearMinObservations int
	ProfileClearStability       time.Duration
	PhotoChangeMinObservations  int
	PhotoChangeStability        time.Duration
}

func DefaultYouTubePlaneConfig() YouTubePlaneConfig {
	return YouTubePlaneConfig{
		Enabled:                true,
		PostgresPoolMinConns:   1,
		PostgresPoolMaxConns:   4,
		ConsumerWorkers:        2,
		DBOperationConcurrency: 3,
		ClaimBatchSize:         4,
		ClaimLease:             time.Minute,
		ClaimInterval:          2 * time.Second,
		TransactionTimeout:     10 * time.Second,
		ShutdownTimeout:        30 * time.Second,
		Retention:              defaultYouTubePlaneRetentionConfig(),
		Replay:                 defaultYouTubePlaneReplayConfig(),
		TargetProjection: YouTubePlaneTargetProjectionConfig{
			Interval: 5 * time.Second,
			Validity: time.Hour,
		},
		LiveEndFinalizer: YouTubePlaneLiveEndFinalizerConfig{
			Enabled:  true,
			Interval: time.Minute,
		},
		LiveEndGrace: 2 * time.Minute,
	}
}

func loadYouTubePlaneConfig() (YouTubePlaneConfig, error) {
	defaults := DefaultYouTubePlaneConfig()
	config := defaults
	if err := loadYouTubePlanePool(&config, &defaults); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if err := loadYouTubePlaneClaim(&config, &defaults); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if err := loadYouTubePlaneRetention(&config); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if err := loadYouTubePlaneReplay(&config); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if err := loadYouTubePlaneSchedules(&config, &defaults); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if err := loadContentAbsenceGrace(&config, &defaults); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if err := loadLiveEndGrace(&config, &defaults); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if err := loadProfilePhotoStability(&config, &defaults); err != nil {
		return YouTubePlaneConfig{}, err
	}
	return config, nil
}

func loadYouTubePlanePool(config, defaults *YouTubePlaneConfig) error {
	var err error
	if config.Enabled, err = sharedenv.BoolE("YOUTUBE_PLANE_ENABLED", defaults.Enabled); err != nil {
		return err
	}
	if config.PostgresPoolMinConns, err = sharedenv.IntE("YOUTUBE_PLANE_POSTGRES_POOL_MIN_CONNS", defaults.PostgresPoolMinConns); err != nil {
		return err
	}
	if config.PostgresPoolMaxConns, err = sharedenv.IntE("YOUTUBE_PLANE_POSTGRES_POOL_MAX_CONNS", defaults.PostgresPoolMaxConns); err != nil {
		return err
	}
	if config.ConsumerWorkers, err = sharedenv.IntE("YOUTUBE_PLANE_CONSUMER_WORKERS", defaults.ConsumerWorkers); err != nil {
		return err
	}
	if config.DBOperationConcurrency, err = sharedenv.IntE("YOUTUBE_PLANE_DB_OPERATION_CONCURRENCY", defaults.DBOperationConcurrency); err != nil {
		return err
	}
	return nil
}

func loadYouTubePlaneClaim(config, defaults *YouTubePlaneConfig) error {
	var err error
	if config.ClaimBatchSize, err = sharedenv.IntE("YOUTUBE_PLANE_CLAIM_BATCH_SIZE", defaults.ClaimBatchSize); err != nil {
		return err
	}
	if config.ClaimLease, err = strictDurationUnitEnv("YOUTUBE_PLANE_CLAIM_LEASE_SECONDS", defaults.ClaimLease, time.Second); err != nil {
		return err
	}
	if config.ClaimInterval, err = strictDurationUnitEnv("YOUTUBE_PLANE_CLAIM_INTERVAL_MS", defaults.ClaimInterval, time.Millisecond); err != nil {
		return err
	}
	if config.TransactionTimeout, err = strictDurationUnitEnv("YOUTUBE_PLANE_TRANSACTION_TIMEOUT_SECONDS", defaults.TransactionTimeout, time.Second); err != nil {
		return err
	}
	if config.ShutdownTimeout, err = strictDurationUnitEnv("YOUTUBE_PLANE_SHUTDOWN_TIMEOUT_SECONDS", defaults.ShutdownTimeout, time.Second); err != nil {
		return err
	}
	return nil
}

func loadYouTubePlaneSchedules(config, defaults *YouTubePlaneConfig) error {
	var err error
	if config.TargetProjection.Interval, err = strictDurationUnitEnv("YOUTUBE_PLANE_TARGET_PROJECTION_INTERVAL_MS", defaults.TargetProjection.Interval, time.Millisecond); err != nil {
		return err
	}
	if config.TargetProjection.Validity, err = strictDurationUnitEnv("YOUTUBE_PLANE_TARGET_PROJECTION_VALIDITY_SECONDS", defaults.TargetProjection.Validity, time.Second); err != nil {
		return err
	}
	if config.LiveEndFinalizer.Enabled, err = sharedenv.BoolE("YOUTUBE_PLANE_LIVE_END_FINALIZER_ENABLED", defaults.LiveEndFinalizer.Enabled); err != nil {
		return err
	}
	if config.LiveEndFinalizer.Interval, err = strictDurationUnitEnv("YOUTUBE_PLANE_LIVE_END_FINALIZER_INTERVAL_SECONDS", defaults.LiveEndFinalizer.Interval, time.Second); err != nil {
		return err
	}
	return nil
}

func loadContentAbsenceGrace(config, defaults *YouTubePlaneConfig) error {
	value, err := strictDurationUnitEnv("YOUTUBE_PLANE_CONTENT_ABSENCE_GRACE_SECONDS", defaults.ContentAbsenceGrace, time.Second)
	if err != nil {
		return err
	}
	config.ContentAbsenceGrace = value
	return nil
}

func loadLiveEndGrace(config, defaults *YouTubePlaneConfig) error {
	value, err := strictDurationUnitEnv("YOUTUBE_PLANE_LIVE_END_GRACE_SECONDS", defaults.LiveEndGrace, time.Second)
	if err != nil {
		return err
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
		return err
	}
	if config.ProfileClearStability, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_PROFILE_CLEAR_STABILITY_SECONDS",
		defaults.ProfileClearStability,
		time.Second,
	); err != nil {
		return err
	}
	if config.PhotoChangeMinObservations, err = sharedenv.IntE(
		"YOUTUBE_PLANE_PHOTO_CHANGE_MIN_OBSERVATIONS",
		defaults.PhotoChangeMinObservations,
	); err != nil {
		return err
	}
	if config.PhotoChangeStability, err = strictDurationUnitEnv(
		"YOUTUBE_PLANE_PHOTO_CHANGE_STABILITY_SECONDS",
		defaults.PhotoChangeStability,
		time.Second,
	); err != nil {
		return err
	}
	return nil
}

func strictDurationUnitEnv(key string, fallback, unit time.Duration) (time.Duration, error) {
	value, err := sharedenv.Int64E(key, int64(fallback/unit))
	if err != nil {
		return 0, err
	}
	const (
		maxDuration = time.Duration(1<<63 - 1)
		minDuration = time.Duration(-1 << 63)
	)
	if value > int64(maxDuration/unit) || value < int64(minDuration/unit) {
		return 0, fmt.Errorf("parse environment variable %s as duration: value is out of range", key)
	}
	return time.Duration(value) * unit, nil
}
