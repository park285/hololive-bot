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

	cfg := loadYouTubePlaneConfig()
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
	if err := cfg.Validate(); err != nil {
		t.Fatalf("overridden config: %v", err)
	}
}
