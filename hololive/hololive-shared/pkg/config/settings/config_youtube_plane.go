package settings

import (
	"errors"
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

	ShutdownTimeout     time.Duration
	Retention           YouTubePlaneRetentionConfig
	Replay              YouTubePlaneRetentionConfig
	TargetProjection    YouTubePlaneTargetProjectionConfig
	LiveEndFinalizer    YouTubePlaneLiveEndFinalizerConfig
	ContentAbsenceGrace time.Duration
	LiveEndGrace        time.Duration
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
	var err error
	if config.Enabled, err = sharedenv.BoolE("YOUTUBE_PLANE_ENABLED", defaults.Enabled); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.PostgresPoolMinConns, err = sharedenv.IntE("YOUTUBE_PLANE_POSTGRES_POOL_MIN_CONNS", defaults.PostgresPoolMinConns); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.PostgresPoolMaxConns, err = sharedenv.IntE("YOUTUBE_PLANE_POSTGRES_POOL_MAX_CONNS", defaults.PostgresPoolMaxConns); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.ConsumerWorkers, err = sharedenv.IntE("YOUTUBE_PLANE_CONSUMER_WORKERS", defaults.ConsumerWorkers); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.DBOperationConcurrency, err = sharedenv.IntE("YOUTUBE_PLANE_DB_OPERATION_CONCURRENCY", defaults.DBOperationConcurrency); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.ClaimBatchSize, err = sharedenv.IntE("YOUTUBE_PLANE_CLAIM_BATCH_SIZE", defaults.ClaimBatchSize); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.ClaimLease, err = strictDurationUnitEnv("YOUTUBE_PLANE_CLAIM_LEASE_SECONDS", defaults.ClaimLease, time.Second); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.ClaimInterval, err = strictDurationUnitEnv("YOUTUBE_PLANE_CLAIM_INTERVAL_MS", defaults.ClaimInterval, time.Millisecond); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.TransactionTimeout, err = strictDurationUnitEnv("YOUTUBE_PLANE_TRANSACTION_TIMEOUT_SECONDS", defaults.TransactionTimeout, time.Second); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.ShutdownTimeout, err = strictDurationUnitEnv("YOUTUBE_PLANE_SHUTDOWN_TIMEOUT_SECONDS", defaults.ShutdownTimeout, time.Second); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.Retention.Enabled, err = sharedenv.BoolE("YOUTUBE_PLANE_RETENTION_ENABLED", false); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.Retention.Interval, err = strictDurationUnitEnv("YOUTUBE_PLANE_RETENTION_INTERVAL_SECONDS", time.Hour, time.Second); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.Replay.Enabled, err = sharedenv.BoolE("YOUTUBE_PLANE_REPLAY_ENABLED", false); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.Replay.Interval, err = strictDurationUnitEnv("YOUTUBE_PLANE_REPLAY_INTERVAL_SECONDS", time.Hour, time.Second); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.TargetProjection.Interval, err = strictDurationUnitEnv("YOUTUBE_PLANE_TARGET_PROJECTION_INTERVAL_MS", defaults.TargetProjection.Interval, time.Millisecond); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.TargetProjection.Validity, err = strictDurationUnitEnv("YOUTUBE_PLANE_TARGET_PROJECTION_VALIDITY_SECONDS", defaults.TargetProjection.Validity, time.Second); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.LiveEndFinalizer.Enabled, err = sharedenv.BoolE("YOUTUBE_PLANE_LIVE_END_FINALIZER_ENABLED", defaults.LiveEndFinalizer.Enabled); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if config.LiveEndFinalizer.Interval, err = strictDurationUnitEnv("YOUTUBE_PLANE_LIVE_END_FINALIZER_INTERVAL_SECONDS", defaults.LiveEndFinalizer.Interval, time.Second); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if err := loadContentAbsenceGrace(&config, defaults); err != nil {
		return YouTubePlaneConfig{}, err
	}
	if err := loadLiveEndGrace(&config, defaults); err != nil {
		return YouTubePlaneConfig{}, err
	}
	return config, nil
}

func loadContentAbsenceGrace(config *YouTubePlaneConfig, defaults YouTubePlaneConfig) error {
	value, err := strictDurationUnitEnv("YOUTUBE_PLANE_CONTENT_ABSENCE_GRACE_SECONDS", defaults.ContentAbsenceGrace, time.Second)
	if err != nil {
		return err
	}
	config.ContentAbsenceGrace = value
	return nil
}

func loadLiveEndGrace(config *YouTubePlaneConfig, defaults YouTubePlaneConfig) error {
	value, err := strictDurationUnitEnv("YOUTUBE_PLANE_LIVE_END_GRACE_SECONDS", defaults.LiveEndGrace, time.Second)
	if err != nil {
		return err
	}
	config.LiveEndGrace = value
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

func (c YouTubePlaneConfig) Validate() error {
	if c.PostgresPoolMinConns < 0 || c.PostgresPoolMaxConns <= 0 {
		return errors.New("youtube plane postgres pool bounds are invalid")
	}
	if c.PostgresPoolMaxConns > youtubePlaneMaxPoolConns {
		return errors.New("youtube plane postgres pool max must not exceed 16")
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
	if c.TransactionTimeout < time.Second || c.TransactionTimeout > time.Minute {
		return errors.New("youtube plane transaction timeout must be between 1s and 1m")
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
	if c.ShutdownTimeout/2 < c.TransactionTimeout {
		return errors.New("youtube plane shutdown timeout must cover transaction and claim release timeouts")
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
	if err := c.validateContentAbsenceGrace(); err != nil {
		return err
	}
	return c.validateLiveEndGrace()
}

func (c YouTubePlaneConfig) validateContentAbsenceGrace() error {
	if c.ContentAbsenceGrace < 0 || c.ContentAbsenceGrace > 24*time.Hour {
		return errors.New("youtube plane content absence grace must be between 0 and 24h")
	}
	return nil
}

func (c YouTubePlaneConfig) validateLiveEndGrace() error {
	if c.LiveEndGrace < 0 || c.LiveEndGrace > 24*time.Hour {
		return errors.New("youtube plane live end grace must be between 0 and 24h")
	}
	return nil
}
