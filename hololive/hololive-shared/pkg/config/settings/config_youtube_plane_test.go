package settings

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultYouTubePlaneConfigValidates(t *testing.T) {
	t.Parallel()
	cfg := DefaultYouTubePlaneConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default youtube plane config: %v", err)
	}
	if !cfg.Enabled || cfg.PostgresPoolMaxConns != 4 || cfg.ConsumerWorkers != 2 || cfg.DBOperationConcurrency != 3 {
		t.Fatalf("default pool/worker budget = %#v", cfg)
	}
	if !cfg.LiveEndFinalizer.Enabled || cfg.LiveEndFinalizer.Interval != time.Minute {
		t.Fatalf("default live end finalizer = %#v", cfg.LiveEndFinalizer)
	}
	if cfg.Retention.Enabled || cfg.Retention.PolicyApproved || cfg.Replay.Enabled {
		t.Fatalf("retention/replay must stay disabled until enabled: %#v %#v", cfg.Retention, cfg.Replay)
	}
	if cfg.Retention.BatchSize != 1000 || cfg.Retention.Interval != time.Hour {
		t.Fatalf("default retention batch/interval = %#v", cfg.Retention)
	}
	if cfg.Retention.ChannelStatsAge != 180*24*time.Hour ||
		cfg.Retention.LiveSnapshotAge != 365*24*time.Hour ||
		cfg.Retention.ViewerSampleAge != 30*24*time.Hour {
		t.Fatalf("inventoried evidence ages = %#v", cfg.Retention)
	}
	if cfg.Retention.QueueProcessedAge != 0 || cfg.Retention.QueueDLQAge != 0 ||
		cfg.Retention.CollisionAge != 0 || cfg.Retention.ReplayAuditAge != 0 || cfg.Retention.ProjectionRetiredAge != 0 {
		t.Fatalf("uninventoried retention ages must stay disabled: %#v", cfg.Retention)
	}
}

func TestYouTubePlaneConfigValidateFailsClosed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*YouTubePlaneConfig)
		wantErr string
	}{
		{
			name:    "zero max pool",
			mutate:  func(c *YouTubePlaneConfig) { c.PostgresPoolMaxConns = 0 },
			wantErr: "postgres pool bounds are invalid",
		},
		{
			name:    "min exceeds max",
			mutate:  func(c *YouTubePlaneConfig) { c.PostgresPoolMinConns = 5 },
			wantErr: "pool min exceeds max",
		},
		{
			name:    "pool exceeds process budget",
			mutate:  func(c *YouTubePlaneConfig) { c.PostgresPoolMaxConns = 17 },
			wantErr: "pool max must not exceed 16",
		},
		{
			name:    "no reserved connection",
			mutate:  func(c *YouTubePlaneConfig) { c.DBOperationConcurrency = 4 },
			wantErr: "leave one pool connection reserved",
		},
		{
			name: "workers exceed db budget",
			mutate: func(c *YouTubePlaneConfig) {
				c.ConsumerWorkers = 3
				c.DBOperationConcurrency = 2
			},
			wantErr: "consumers exceed the shared DB operation budget",
		},
		{
			name:    "lease too short",
			mutate:  func(c *YouTubePlaneConfig) { c.ClaimLease = 40 * time.Second },
			wantErr: "claim lease must be at least",
		},
		{
			name:    "zero transaction timeout",
			mutate:  func(c *YouTubePlaneConfig) { c.TransactionTimeout = 0 },
			wantErr: "transaction timeout must be positive",
		},
		{
			name:    "transaction timeout exceeds repository contract",
			mutate:  func(c *YouTubePlaneConfig) { c.TransactionTimeout = 61 * time.Second },
			wantErr: "transaction timeout must be between 1s and 1m",
		},
		{
			name:    "shutdown cannot cover release",
			mutate:  func(c *YouTubePlaneConfig) { c.ShutdownTimeout = 15 * time.Second },
			wantErr: "shutdown timeout must cover transaction and claim release timeouts",
		},
		{
			name:    "retention batch too large",
			mutate:  func(c *YouTubePlaneConfig) { c.Retention.BatchSize = 1001 },
			wantErr: "retention batch size must be between 1 and 1000",
		},
		{
			name:    "retention batch zero",
			mutate:  func(c *YouTubePlaneConfig) { c.Retention.BatchSize = 0 },
			wantErr: "retention batch size must be between 1 and 1000",
		},
		{
			name:    "retention enabled without interval",
			mutate:  func(c *YouTubePlaneConfig) { c.Retention.Enabled = true; c.Retention.Interval = 0 },
			wantErr: "retention interval must be positive when enabled",
		},
		{
			name: "replay audit shorter than evidence",
			mutate: func(c *YouTubePlaneConfig) {
				c.Retention.Enabled = true
				c.Retention.ReplayAuditAge = 30 * 24 * time.Hour
			},
			wantErr: "replay audit retention must cover the longest evidence retention",
		},
		{
			name:    "negative queue processed age",
			mutate:  func(c *YouTubePlaneConfig) { c.Retention.QueueProcessedAge = -time.Hour },
			wantErr: "queue processed retention must be between 0 and 3650 days",
		},
		{
			name:    "replay batch too large",
			mutate:  func(c *YouTubePlaneConfig) { c.Replay.BatchSize = 1001 },
			wantErr: "replay batch size must be between 1 and 1000",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := DefaultYouTubePlaneConfig()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadYouTubePlaneConfigNonDefaultOverride(t *testing.T) {
	t.Setenv("YOUTUBE_PLANE_ENABLED", "true")
	t.Setenv("YOUTUBE_PLANE_POSTGRES_POOL_MIN_CONNS", "1")
	t.Setenv("YOUTUBE_PLANE_POSTGRES_POOL_MAX_CONNS", "2")
	t.Setenv("YOUTUBE_PLANE_CONSUMER_WORKERS", "1")
	t.Setenv("YOUTUBE_PLANE_DB_OPERATION_CONCURRENCY", "1")
	t.Setenv("YOUTUBE_PLANE_CLAIM_BATCH_SIZE", "2")
	t.Setenv("YOUTUBE_PLANE_CLAIM_LEASE_SECONDS", "40")
	t.Setenv("YOUTUBE_PLANE_CLAIM_INTERVAL_MS", "1500")
	t.Setenv("YOUTUBE_PLANE_TRANSACTION_TIMEOUT_SECONDS", "8")
	t.Setenv("YOUTUBE_PLANE_SHUTDOWN_TIMEOUT_SECONDS", "20")
	t.Setenv("YOUTUBE_PLANE_TARGET_PROJECTION_INTERVAL_MS", "7000")
	t.Setenv("YOUTUBE_PLANE_TARGET_PROJECTION_VALIDITY_SECONDS", "1800")

	cfg, err := loadYouTubePlaneConfig()
	if err != nil {
		t.Fatalf("loadYouTubePlaneConfig() error = %v", err)
	}
	if cfg.PostgresPoolMaxConns != 2 || cfg.ConsumerWorkers != 1 || cfg.DBOperationConcurrency != 1 {
		t.Fatalf("override pool/worker budget = %d %d %d", cfg.PostgresPoolMaxConns, cfg.ConsumerWorkers, cfg.DBOperationConcurrency)
	}
	if cfg.ClaimBatchSize != 2 || cfg.ClaimLease != 40*time.Second || cfg.ClaimInterval != 1500*time.Millisecond {
		t.Fatalf("override claim = %d %s %s", cfg.ClaimBatchSize, cfg.ClaimLease, cfg.ClaimInterval)
	}
	if cfg.TransactionTimeout != 8*time.Second || cfg.ShutdownTimeout != 20*time.Second {
		t.Fatalf("override timeouts = %s %s", cfg.TransactionTimeout, cfg.ShutdownTimeout)
	}
	if cfg.TargetProjection.Interval != 7*time.Second || cfg.TargetProjection.Validity != 30*time.Minute {
		t.Fatalf("override projection = %#v", cfg.TargetProjection)
	}
	if cfg.ContentAbsenceGrace != 0 {
		t.Fatalf("default content absence grace = %s", cfg.ContentAbsenceGrace)
	}
	if cfg.LiveEndGrace != 2*time.Minute {
		t.Fatalf("default live end grace = %s", cfg.LiveEndGrace)
	}
	if !cfg.LiveEndFinalizer.Enabled || cfg.LiveEndFinalizer.Interval != time.Minute {
		t.Fatalf("default live end finalizer after load = %#v", cfg.LiveEndFinalizer)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("overridden config: %v", err)
	}
}

func TestLoadYouTubePlaneConfigContentAbsenceGraceOverride(t *testing.T) {
	t.Setenv("YOUTUBE_PLANE_CONTENT_ABSENCE_GRACE_SECONDS", "90")
	cfg, err := loadYouTubePlaneConfig()
	if err != nil {
		t.Fatalf("loadYouTubePlaneConfig() error = %v", err)
	}
	if cfg.ContentAbsenceGrace != 90*time.Second {
		t.Fatalf("ContentAbsenceGrace = %s, want 90s", cfg.ContentAbsenceGrace)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("overridden content absence grace: %v", err)
	}
}

func TestYouTubePlaneConfigRejectsInvalidContentAbsenceGrace(t *testing.T) {
	t.Parallel()
	cfg := DefaultYouTubePlaneConfig()
	cfg.ContentAbsenceGrace = 25 * time.Hour
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "content absence grace") {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestLoadYouTubePlaneConfigRetentionOverride(t *testing.T) {
	t.Setenv("YOUTUBE_PLANE_RETENTION_ENABLED", "true")
	t.Setenv("YOUTUBE_PLANE_RETENTION_POLICY_APPROVED", "true")
	t.Setenv("YOUTUBE_PLANE_RETENTION_INTERVAL_SECONDS", "1800")
	t.Setenv("YOUTUBE_PLANE_RETENTION_BATCH_SIZE", "25")
	t.Setenv("YOUTUBE_PLANE_RETENTION_QUEUE_PROCESSED_DAYS", "7")
	t.Setenv("YOUTUBE_PLANE_RETENTION_QUEUE_DLQ_DAYS", "14")
	t.Setenv("YOUTUBE_PLANE_RETENTION_COLLISION_DAYS", "21")
	t.Setenv("YOUTUBE_PLANE_RETENTION_REPLAY_AUDIT_DAYS", "180")
	t.Setenv("YOUTUBE_PLANE_RETENTION_PROJECTION_RETIRED_DAYS", "45")
	t.Setenv("YOUTUBE_PLANE_RETENTION_COMMUNITY_PAGE_DAYS", "31")
	t.Setenv("YOUTUBE_PLANE_RETENTION_VIDEO_LIST_DAYS", "32")
	t.Setenv("YOUTUBE_PLANE_RETENTION_SHORTS_LIST_DAYS", "33")
	t.Setenv("YOUTUBE_PLANE_RETENTION_CHANNEL_STATS_DAYS", "90")
	t.Setenv("YOUTUBE_PLANE_RETENTION_LIVE_SNAPSHOT_DAYS", "120")
	t.Setenv("YOUTUBE_PLANE_RETENTION_VIEWER_SAMPLE_DAYS", "10")
	t.Setenv("YOUTUBE_PLANE_RETENTION_CHANNEL_PROFILE_DAYS", "34")
	t.Setenv("YOUTUBE_PLANE_RETENTION_CHANNEL_PHOTO_DAYS", "35")
	t.Setenv("YOUTUBE_PLANE_RETENTION_SCHEDULE_SNAPSHOT_DAYS", "36")
	t.Setenv("YOUTUBE_PLANE_REPLAY_ENABLED", "true")
	t.Setenv("YOUTUBE_PLANE_REPLAY_INTERVAL_SECONDS", "45")
	t.Setenv("YOUTUBE_PLANE_REPLAY_BATCH_SIZE", "8")

	cfg, err := loadYouTubePlaneConfig()
	if err != nil {
		t.Fatalf("loadYouTubePlaneConfig() error = %v", err)
	}
	if !cfg.Retention.Enabled || !cfg.Retention.PolicyApproved || cfg.Retention.Interval != 30*time.Minute || cfg.Retention.BatchSize != 25 {
		t.Fatalf("retention override = %#v", cfg.Retention)
	}
	if cfg.Retention.QueueProcessedAge != 7*24*time.Hour || cfg.Retention.QueueDLQAge != 14*24*time.Hour {
		t.Fatalf("queue ages = %s %s", cfg.Retention.QueueProcessedAge, cfg.Retention.QueueDLQAge)
	}
	if cfg.Retention.CollisionAge != 21*24*time.Hour || cfg.Retention.ReplayAuditAge != 180*24*time.Hour {
		t.Fatalf("audit ages = %s %s", cfg.Retention.CollisionAge, cfg.Retention.ReplayAuditAge)
	}
	if cfg.Retention.ProjectionRetiredAge != 45*24*time.Hour {
		t.Fatalf("projection retired age = %s", cfg.Retention.ProjectionRetiredAge)
	}
	if cfg.Retention.ChannelStatsAge != 90*24*time.Hour ||
		cfg.Retention.LiveSnapshotAge != 120*24*time.Hour ||
		cfg.Retention.ViewerSampleAge != 10*24*time.Hour {
		t.Fatalf("evidence ages = %#v", cfg.Retention)
	}
	if cfg.Retention.CommunityPageAge != 31*24*time.Hour ||
		cfg.Retention.VideoListAge != 32*24*time.Hour ||
		cfg.Retention.ShortsListAge != 33*24*time.Hour ||
		cfg.Retention.ChannelProfileAge != 34*24*time.Hour ||
		cfg.Retention.ChannelPhotoAge != 35*24*time.Hour ||
		cfg.Retention.ScheduleSnapshotAge != 36*24*time.Hour {
		t.Fatalf("remaining evidence ages = %#v", cfg.Retention)
	}
	if !cfg.Replay.Enabled || cfg.Replay.Interval != 45*time.Second || cfg.Replay.BatchSize != 8 {
		t.Fatalf("replay override = %#v", cfg.Replay)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("overridden retention: %v", err)
	}
}

func TestYouTubePlaneProductionRetentionRequiresApprovedBoundedPolicy(t *testing.T) {
	cfg := DefaultYouTubePlaneConfig()
	if err := cfg.validateProductionRetention("development"); err != nil {
		t.Fatalf("development retention validation: %v", err)
	}
	if err := cfg.validateProductionRetention("production"); err == nil ||
		!strings.Contains(err.Error(), "YOUTUBE_PLANE_RETENTION_ENABLED=true") {
		t.Fatalf("disabled production retention error = %v", err)
	}
	cfg.Retention.Enabled = true
	if err := cfg.validateProductionRetention("production"); err == nil ||
		!strings.Contains(err.Error(), "YOUTUBE_PLANE_RETENTION_POLICY_APPROVED=true") {
		t.Fatalf("unapproved production retention error = %v", err)
	}
	cfg.Retention.PolicyApproved = true
	if err := cfg.validateProductionRetention("production"); err == nil ||
		!strings.Contains(err.Error(), "YOUTUBE_PLANE_RETENTION_QUEUE_PROCESSED_DAYS") {
		t.Fatalf("unbounded production retention error = %v", err)
	}

	oneDay := 24 * time.Hour
	cfg.Retention.QueueProcessedAge = oneDay
	cfg.Retention.QueueDLQAge = oneDay
	cfg.Retention.CollisionAge = oneDay
	cfg.Retention.ReplayAuditAge = cfg.Retention.LiveSnapshotAge
	cfg.Retention.ProjectionRetiredAge = oneDay
	cfg.Retention.CommunityPageAge = oneDay
	cfg.Retention.VideoListAge = oneDay
	cfg.Retention.ShortsListAge = oneDay
	cfg.Retention.ChannelProfileAge = oneDay
	cfg.Retention.ChannelPhotoAge = oneDay
	cfg.Retention.ScheduleSnapshotAge = oneDay
	if err := cfg.Validate(); err != nil {
		t.Fatalf("bounded production retention config: %v", err)
	}
	if err := cfg.validateProductionRetention("production"); err != nil {
		t.Fatalf("approved production retention policy: %v", err)
	}
}

func TestLoadYouTubePlaneConfigRetentionCanDisableInventoriedAges(t *testing.T) {
	t.Setenv("YOUTUBE_PLANE_RETENTION_CHANNEL_STATS_DAYS", "0")
	t.Setenv("YOUTUBE_PLANE_RETENTION_LIVE_SNAPSHOT_DAYS", "0")
	t.Setenv("YOUTUBE_PLANE_RETENTION_VIEWER_SAMPLE_DAYS", "0")
	cfg, err := loadYouTubePlaneConfig()
	if err != nil {
		t.Fatalf("loadYouTubePlaneConfig() error = %v", err)
	}
	if cfg.Retention.ChannelStatsAge != 0 || cfg.Retention.LiveSnapshotAge != 0 || cfg.Retention.ViewerSampleAge != 0 {
		t.Fatalf("disabled inventoried ages = %#v", cfg.Retention)
	}
}

func TestLoadYouTubePlaneConfigLiveEndFinalizerOverride(t *testing.T) {
	t.Setenv("YOUTUBE_PLANE_LIVE_END_FINALIZER_ENABLED", "false")
	t.Setenv("YOUTUBE_PLANE_LIVE_END_FINALIZER_INTERVAL_SECONDS", "30")
	cfg, err := loadYouTubePlaneConfig()
	if err != nil {
		t.Fatalf("loadYouTubePlaneConfig() error = %v", err)
	}
	if cfg.LiveEndFinalizer.Enabled {
		t.Fatal("LiveEndFinalizer.Enabled override to false was ignored")
	}
	if cfg.LiveEndFinalizer.Interval != 30*time.Second {
		t.Fatalf("LiveEndFinalizer.Interval = %s, want 30s", cfg.LiveEndFinalizer.Interval)
	}
}

func TestLoadYouTubePlaneConfigLiveEndGraceOverride(t *testing.T) {
	t.Setenv("YOUTUBE_PLANE_LIVE_END_GRACE_SECONDS", "180")
	cfg, err := loadYouTubePlaneConfig()
	if err != nil {
		t.Fatalf("loadYouTubePlaneConfig() error = %v", err)
	}
	if cfg.LiveEndGrace != 180*time.Second {
		t.Fatalf("LiveEndGrace = %s, want 180s", cfg.LiveEndGrace)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("overridden live end grace: %v", err)
	}
}

func TestYouTubePlaneConfigRejectsInvalidLiveEndGrace(t *testing.T) {
	t.Parallel()
	cfg := DefaultYouTubePlaneConfig()
	cfg.LiveEndGrace = 25 * time.Hour
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "live end grace") {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestDefaultYouTubePlaneConfigDisablesUnapprovedProfilePhotoStability(t *testing.T) {
	t.Parallel()
	cfg := DefaultYouTubePlaneConfig()
	if cfg.ProfileClearMinObservations != 0 || cfg.ProfileClearStability != 0 {
		t.Fatalf("unapproved profile clear defaults = %d %s", cfg.ProfileClearMinObservations, cfg.ProfileClearStability)
	}
	if cfg.PhotoChangeMinObservations != 0 || cfg.PhotoChangeStability != 0 {
		t.Fatalf("unapproved photo change defaults = %d %s", cfg.PhotoChangeMinObservations, cfg.PhotoChangeStability)
	}
}

func TestLoadYouTubePlaneConfigProfilePhotoStabilityOverride(t *testing.T) {
	t.Setenv("YOUTUBE_PLANE_PROFILE_CLEAR_MIN_OBSERVATIONS", "2")
	t.Setenv("YOUTUBE_PLANE_PROFILE_CLEAR_STABILITY_SECONDS", "3600")
	t.Setenv("YOUTUBE_PLANE_PHOTO_CHANGE_MIN_OBSERVATIONS", "3")
	t.Setenv("YOUTUBE_PLANE_PHOTO_CHANGE_STABILITY_SECONDS", "7200")
	cfg, err := loadYouTubePlaneConfig()
	if err != nil {
		t.Fatalf("loadYouTubePlaneConfig() error = %v", err)
	}
	if cfg.ProfileClearMinObservations != 2 || cfg.ProfileClearStability != time.Hour {
		t.Fatalf("profile clear override = %d %s", cfg.ProfileClearMinObservations, cfg.ProfileClearStability)
	}
	if cfg.PhotoChangeMinObservations != 3 || cfg.PhotoChangeStability != 2*time.Hour {
		t.Fatalf("photo change override = %d %s", cfg.PhotoChangeMinObservations, cfg.PhotoChangeStability)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("overridden profile/photo stability: %v", err)
	}
}

func TestYouTubePlaneConfigRejectsInvalidProfilePhotoStability(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*YouTubePlaneConfig)
		wantErr string
	}{
		{
			name:    "profile clear half enabled",
			mutate:  func(c *YouTubePlaneConfig) { c.ProfileClearMinObservations = 2 },
			wantErr: "profile clear min observations and stability must be enabled together",
		},
		{
			name: "profile clear below threshold",
			mutate: func(c *YouTubePlaneConfig) {
				c.ProfileClearMinObservations = 1
				c.ProfileClearStability = time.Hour
			},
			wantErr: "profile clear min observations must be at least 2",
		},
		{
			name:    "photo change half enabled",
			mutate:  func(c *YouTubePlaneConfig) { c.PhotoChangeStability = time.Hour },
			wantErr: "photo change min observations and stability must be enabled together",
		},
		{
			name:    "photo change overflow",
			mutate:  func(c *YouTubePlaneConfig) { c.PhotoChangeMinObservations = 101 },
			wantErr: "photo change min observations must be between 0 and 100",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := DefaultYouTubePlaneConfig()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadYouTubePlaneConfigRejectsInvalidExplicitValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "invalid bool", key: "YOUTUBE_PLANE_ENABLED", value: "not-a-bool"},
		{name: "invalid integer", key: "YOUTUBE_PLANE_POSTGRES_POOL_MAX_CONNS", value: "not-an-int"},
		{name: "overflowing duration", key: "YOUTUBE_PLANE_CLAIM_LEASE_SECONDS", value: "9223372036854775807"},
		{name: "overflowing retention days", key: "YOUTUBE_PLANE_RETENTION_CHANNEL_STATS_DAYS", value: "9223372036854775807"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			_, err := loadYouTubePlaneConfig()
			if err == nil || !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("loadYouTubePlaneConfig() error = %v, want key %s", err, tt.key)
			}
		})
	}
}

func TestLoadYouTubePlaneConfigPreservesExplicitInvalidBounds(t *testing.T) {
	t.Setenv("YOUTUBE_PLANE_POSTGRES_POOL_MAX_CONNS", "0")

	cfg, err := loadYouTubePlaneConfig()
	if err != nil {
		t.Fatalf("loadYouTubePlaneConfig() error = %v", err)
	}
	if cfg.PostgresPoolMaxConns != 0 {
		t.Fatalf("PostgresPoolMaxConns = %d, want explicit 0", cfg.PostgresPoolMaxConns)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid explicit bound")
	}
}

func TestLoadHololiveAPIRuntimeRejectsInvalidYouTubePlaneEnv(t *testing.T) {
	clearRuntimeRoleEnv(t)
	clearTracingEnv(t)
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("ALARM_INTERNAL_URL", "http://127.0.0.1:30007")
	t.Setenv("YOUTUBE_PLANE_ENABLED", "not-a-bool")

	_, err := LoadHololiveAPIRuntime()
	if err == nil || !strings.Contains(err.Error(), "YOUTUBE_PLANE_ENABLED") {
		t.Fatalf("LoadHololiveAPIRuntime() error = %v, want invalid YouTube plane env", err)
	}
}
