package settings

import (
	"errors"
	"fmt"
	"time"

	sharedenv "github.com/park285/shared-go/pkg/envutil"
)

const (
	youtubePlaneMaxConsumerWorkers = 16
	youtubePlaneMaxClaimBatchSize  = 100
)

type YouTubePlaneRetentionConfig struct {
	Enabled  bool
	Interval time.Duration
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

	ShutdownTimeout  time.Duration
	Retention        YouTubePlaneRetentionConfig
	Replay           YouTubePlaneRetentionConfig
	TargetProjection YouTubePlaneTargetProjectionConfig
	LiveEndFinalizer YouTubePlaneLiveEndFinalizerConfig
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
		TargetProjection: YouTubePlaneTargetProjectionConfig{
			Interval: 5 * time.Second,
			Validity: time.Hour,
		},
	}
}

func loadYouTubePlaneConfig() YouTubePlaneConfig {
	defaults := DefaultYouTubePlaneConfig()
	return YouTubePlaneConfig{
		Enabled:                sharedenv.Bool("YOUTUBE_PLANE_ENABLED", defaults.Enabled),
		PostgresPoolMinConns:   sharedenv.Int("YOUTUBE_PLANE_POSTGRES_POOL_MIN_CONNS", defaults.PostgresPoolMinConns),
		PostgresPoolMaxConns:   positiveIntEnv("YOUTUBE_PLANE_POSTGRES_POOL_MAX_CONNS", defaults.PostgresPoolMaxConns),
		ConsumerWorkers:        positiveIntEnv("YOUTUBE_PLANE_CONSUMER_WORKERS", defaults.ConsumerWorkers),
		DBOperationConcurrency: positiveIntEnv("YOUTUBE_PLANE_DB_OPERATION_CONCURRENCY", defaults.DBOperationConcurrency),
		ClaimBatchSize:         positiveIntEnv("YOUTUBE_PLANE_CLAIM_BATCH_SIZE", defaults.ClaimBatchSize),
		ClaimLease:             durationSecondsEnv("YOUTUBE_PLANE_CLAIM_LEASE_SECONDS", defaults.ClaimLease),
		ClaimInterval:          durationMillisEnv("YOUTUBE_PLANE_CLAIM_INTERVAL_MS", defaults.ClaimInterval),
		TransactionTimeout:     durationSecondsEnv("YOUTUBE_PLANE_TRANSACTION_TIMEOUT_SECONDS", defaults.TransactionTimeout),
		ShutdownTimeout:        durationSecondsEnv("YOUTUBE_PLANE_SHUTDOWN_TIMEOUT_SECONDS", defaults.ShutdownTimeout),
		Retention: YouTubePlaneRetentionConfig{
			Enabled:  sharedenv.Bool("YOUTUBE_PLANE_RETENTION_ENABLED", false),
			Interval: durationSecondsEnv("YOUTUBE_PLANE_RETENTION_INTERVAL_SECONDS", time.Hour),
		},
		Replay: YouTubePlaneRetentionConfig{
			Enabled:  sharedenv.Bool("YOUTUBE_PLANE_REPLAY_ENABLED", false),
			Interval: durationSecondsEnv("YOUTUBE_PLANE_REPLAY_INTERVAL_SECONDS", time.Hour),
		},
		TargetProjection: YouTubePlaneTargetProjectionConfig{
			Interval: durationMillisEnv("YOUTUBE_PLANE_TARGET_PROJECTION_INTERVAL_MS", defaults.TargetProjection.Interval),
			Validity: durationSecondsEnv("YOUTUBE_PLANE_TARGET_PROJECTION_VALIDITY_SECONDS", defaults.TargetProjection.Validity),
		},
		LiveEndFinalizer: YouTubePlaneLiveEndFinalizerConfig{
			Enabled:  sharedenv.Bool("YOUTUBE_PLANE_LIVE_END_FINALIZER_ENABLED", false),
			Interval: durationSecondsEnv("YOUTUBE_PLANE_LIVE_END_FINALIZER_INTERVAL_SECONDS", time.Minute),
		},
	}
}

func (c YouTubePlaneConfig) Validate() error {
	if c.PostgresPoolMinConns < 0 || c.PostgresPoolMaxConns <= 0 {
		return errors.New("youtube plane postgres pool bounds are invalid")
	}
	if c.PostgresPoolMinConns > c.PostgresPoolMaxConns {
		return errors.New("youtube plane postgres pool min exceeds max")
	}
	if c.ConsumerWorkers < 1 || c.ConsumerWorkers > youtubePlaneMaxConsumerWorkers {
		return errors.New("youtube plane consumer workers must be between 1 and 16")
	}
	if c.DBOperationConcurrency < 1 || c.DBOperationConcurrency >= c.PostgresPoolMaxConns {
		return errors.New("youtube plane DB operation concurrency must leave one pool connection reserved")
	}
	if c.ConsumerWorkers > c.DBOperationConcurrency {
		return errors.New("youtube plane consumers exceed the shared DB operation budget")
	}
	if c.ClaimBatchSize < 1 || c.ClaimBatchSize > youtubePlaneMaxClaimBatchSize {
		return errors.New("youtube plane claim batch must be between 1 and 100")
	}
	if c.TransactionTimeout <= 0 {
		return errors.New("youtube plane transaction timeout must be positive")
	}
	minimumLease := time.Duration(c.ClaimBatchSize)*c.TransactionTimeout + 10*time.Second
	if c.ClaimLease < minimumLease {
		return fmt.Errorf(
			"youtube plane claim lease must be at least %s for the configured batch",
			minimumLease,
		)
	}
	if c.ClaimInterval <= 0 {
		return errors.New("youtube plane claim interval must be positive")
	}
	if c.ShutdownTimeout <= 0 {
		return errors.New("youtube plane shutdown timeout must be positive")
	}
	if c.TargetProjection.Interval <= 0 {
		return errors.New("youtube plane target projection interval must be positive")
	}
	if c.TargetProjection.Validity < 5*time.Second || c.TargetProjection.Validity > 24*time.Hour {
		return errors.New("youtube plane target projection validity must be between 5s and 24h")
	}
	if c.Retention.Enabled && c.Retention.Interval <= 0 {
		return errors.New("youtube plane retention interval must be positive when enabled")
	}
	if c.Replay.Enabled && c.Replay.Interval <= 0 {
		return errors.New("youtube plane replay interval must be positive when enabled")
	}
	if c.LiveEndFinalizer.Enabled && c.LiveEndFinalizer.Interval <= 0 {
		return errors.New("youtube plane live end finalizer interval must be positive when enabled")
	}
	return nil
}
