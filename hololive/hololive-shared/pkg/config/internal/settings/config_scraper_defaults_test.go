package settings

import (
	"testing"
	"time"
)

func TestScraperPoll_EstimatedRequestsPerMinute(t *testing.T) {
	t.Parallel()

	t.Run("default poll", func(t *testing.T) {
		t.Parallel()
		p := DefaultScraperPoll()
		rpm := p.EstimatedRequestsPerMinute()
		if rpm <= 0 {
			t.Fatalf("EstimatedRequestsPerMinute() = %f, want > 0", rpm)
		}
	})

	t.Run("zero durations", func(t *testing.T) {
		t.Parallel()
		p := ScraperPoll{}
		rpm := p.EstimatedRequestsPerMinute()
		if rpm != 0 {
			t.Fatalf("EstimatedRequestsPerMinute() with zero poll = %f, want 0", rpm)
		}
	})

	t.Run("partial durations", func(t *testing.T) {
		t.Parallel()
		p := ScraperPoll{Videos: time.Minute, Live: 30 * time.Second}
		rpm := p.EstimatedRequestsPerMinute()
		want := 3.0
		if rpm != want {
			t.Fatalf("EstimatedRequestsPerMinute() = %f, want %f", rpm, want)
		}
	})
}

func TestScraperConfig_PollOrDefault(t *testing.T) {
	t.Parallel()

	t.Run("zero config uses defaults", func(t *testing.T) {
		t.Parallel()
		c := ScraperConfig{}
		got := c.PollOrDefault()
		want := DefaultScraperPoll()
		if got != want {
			t.Fatalf("PollOrDefault() = %+v, want %+v", got, want)
		}
	})

	t.Run("partial overrides", func(t *testing.T) {
		t.Parallel()
		c := ScraperConfig{
			Poll: ScraperPoll{
				Videos: 5 * time.Minute,
				Shorts: 3 * time.Minute,
			},
		}
		got := c.PollOrDefault()
		if got.Videos != 5*time.Minute {
			t.Fatalf("PollOrDefault().Videos = %v, want 5m", got.Videos)
		}
		if got.Shorts != 3*time.Minute {
			t.Fatalf("PollOrDefault().Shorts = %v, want 3m", got.Shorts)
		}
		defaults := DefaultScraperPoll()
		if got.Community != defaults.Community {
			t.Fatalf("PollOrDefault().Community = %v, want default %v", got.Community, defaults.Community)
		}
		if got.Stats != defaults.Stats {
			t.Fatalf("PollOrDefault().Stats = %v, want default %v", got.Stats, defaults.Stats)
		}
		if got.Live != defaults.Live {
			t.Fatalf("PollOrDefault().Live = %v, want default %v", got.Live, defaults.Live)
		}
	})

	t.Run("full overrides", func(t *testing.T) {
		t.Parallel()
		c := ScraperConfig{
			Poll: ScraperPoll{
				Videos:    1 * time.Minute,
				Shorts:    2 * time.Minute,
				Community: 3 * time.Minute,
				Stats:     4 * time.Minute,
				Live:      5 * time.Minute,
			},
		}
		got := c.PollOrDefault()
		if got != c.Poll {
			t.Fatalf("PollOrDefault() = %+v, want %+v", got, c.Poll)
		}
	})
}

func TestScraperConfig_SchedulerOrDefault(t *testing.T) {
	t.Parallel()

	t.Run("zero config uses defaults", func(t *testing.T) {
		t.Parallel()
		c := ScraperConfig{}
		got := c.SchedulerOrDefault()
		want := DefaultScraperSchedulerConfig()
		if got != want {
			t.Fatalf("SchedulerOrDefault() = %+v, want %+v", got, want)
		}
	})

	t.Run("partial overrides", func(t *testing.T) {
		t.Parallel()
		c := ScraperConfig{
			Scheduler: ScraperSchedulerConfig{
				PollTimeout: 10 * time.Second,
			},
		}
		got := c.SchedulerOrDefault()
		if got.PollTimeout != 10*time.Second {
			t.Fatalf("SchedulerOrDefault().PollTimeout = %v, want 10s", got.PollTimeout)
		}
		defaults := DefaultScraperSchedulerConfig()
		if got.ErrorBackoffMin != defaults.ErrorBackoffMin {
			t.Fatalf("SchedulerOrDefault().ErrorBackoffMin = %v, want default %v", got.ErrorBackoffMin, defaults.ErrorBackoffMin)
		}
	})

	t.Run("max less than min gets clamped", func(t *testing.T) {
		t.Parallel()
		c := ScraperConfig{
			Scheduler: ScraperSchedulerConfig{
				PollTimeout:     30 * time.Second,
				ErrorBackoffMin: 10 * time.Second,
				ErrorBackoffMax: 5 * time.Second,
			},
		}
		got := c.SchedulerOrDefault()
		if got.ErrorBackoffMax < got.ErrorBackoffMin {
			t.Fatalf("ErrorBackoffMax (%v) < ErrorBackoffMin (%v), should be clamped", got.ErrorBackoffMax, got.ErrorBackoffMin)
		}
	})
}

func TestScraperConfig_SnapshotOrDefault(t *testing.T) {
	t.Parallel()

	t.Run("zero config uses defaults", func(t *testing.T) {
		t.Parallel()
		c := ScraperConfig{}
		got := c.SnapshotOrDefault()
		defaults := DefaultScraperSnapshotConfig()
		if got.Dir != defaults.Dir {
			t.Fatalf("SnapshotOrDefault().Dir = %q, want %q", got.Dir, defaults.Dir)
		}
		if got.MaxBodyBytes != defaults.MaxBodyBytes {
			t.Fatalf("SnapshotOrDefault().MaxBodyBytes = %d, want %d", got.MaxBodyBytes, defaults.MaxBodyBytes)
		}
		if got.MinInterval != defaults.MinInterval {
			t.Fatalf("SnapshotOrDefault().MinInterval = %v, want %v", got.MinInterval, defaults.MinInterval)
		}
	})

	t.Run("overrides are preserved", func(t *testing.T) {
		t.Parallel()
		c := ScraperConfig{
			Snapshot: ScraperSnapshotConfig{
				Enabled:      true,
				Dir:          "/custom",
				MaxBodyBytes: 1024,
				MinInterval:  time.Minute,
			},
		}
		got := c.SnapshotOrDefault()
		if got.Dir != "/custom" {
			t.Fatalf("SnapshotOrDefault().Dir = %q, want /custom", got.Dir)
		}
		if got.MaxBodyBytes != 1024 {
			t.Fatalf("SnapshotOrDefault().MaxBodyBytes = %d, want 1024", got.MaxBodyBytes)
		}
	})

	t.Run("whitespace-only dir gets default", func(t *testing.T) {
		t.Parallel()
		c := ScraperConfig{
			Snapshot: ScraperSnapshotConfig{Dir: "  "},
		}
		got := c.SnapshotOrDefault()
		if got.Dir != DefaultScraperSnapshotConfig().Dir {
			t.Fatalf("SnapshotOrDefault().Dir = %q, want default", got.Dir)
		}
	})
}

func TestScraperConfig_ChannelHealthOrDefault(t *testing.T) {
	t.Parallel()

	t.Run("zero config uses defaults", func(t *testing.T) {
		t.Parallel()
		c := ScraperConfig{}
		got := c.ChannelHealthOrDefault()
		defaults := DefaultScraperChannelHealthConfig()
		if got.TTL != defaults.TTL {
			t.Fatalf("ChannelHealthOrDefault().TTL = %v, want %v", got.TTL, defaults.TTL)
		}
		if got.SuccessDecaySteps != defaults.SuccessDecaySteps {
			t.Fatalf("ChannelHealthOrDefault().SuccessDecaySteps = %d, want %d", got.SuccessDecaySteps, defaults.SuccessDecaySteps)
		}
	})

	t.Run("overrides preserved", func(t *testing.T) {
		t.Parallel()
		c := ScraperConfig{
			ChannelHealth: ScraperChannelHealthConfig{
				Enabled:           true,
				TTL:               12 * time.Hour,
				ParserDriftBase:   5 * time.Minute,
				ParserDriftMax:    3 * time.Hour,
				TransportBase:     time.Minute,
				TransportMax:      15 * time.Minute,
				TimeoutBase:       time.Minute,
				TimeoutMax:        15 * time.Minute,
				HTTPStatusBase:    10 * time.Minute,
				HTTPStatusMax:     2 * time.Hour,
				SuccessDecaySteps: 3,
			},
		}
		got := c.ChannelHealthOrDefault()
		if got.TTL != 12*time.Hour {
			t.Fatalf("ChannelHealthOrDefault().TTL = %v, want 12h", got.TTL)
		}
		if got.SuccessDecaySteps != 3 {
			t.Fatalf("ChannelHealthOrDefault().SuccessDecaySteps = %d, want 3", got.SuccessDecaySteps)
		}
	})
}

func TestScraperConfig_BrowserDiagnosticOrDefault(t *testing.T) {
	t.Parallel()

	t.Run("zero config uses defaults", func(t *testing.T) {
		t.Parallel()
		c := ScraperConfig{}
		got := c.BrowserDiagnosticOrDefault()
		defaults := DefaultScraperBrowserDiagnosticConfig()
		if got.Timeout != defaults.Timeout {
			t.Fatalf("BrowserDiagnosticOrDefault().Timeout = %v, want %v", got.Timeout, defaults.Timeout)
		}
	})

	t.Run("override preserved", func(t *testing.T) {
		t.Parallel()
		c := ScraperConfig{
			BrowserDiagnostic: ScraperBrowserDiagnosticConfig{
				Enabled:  true,
				Endpoint: "http://browser:9222",
				Timeout:  30 * time.Second,
			},
		}
		got := c.BrowserDiagnosticOrDefault()
		if got.Timeout != 30*time.Second {
			t.Fatalf("BrowserDiagnosticOrDefault().Timeout = %v, want 30s", got.Timeout)
		}
		if got.Endpoint != "http://browser:9222" {
			t.Fatalf("BrowserDiagnosticOrDefault().Endpoint = %q, want http://browser:9222", got.Endpoint)
		}
	})
}

func TestScraperConfig_WorkerCountOrDefault(t *testing.T) {
	t.Parallel()

	t.Run("zero uses default", func(t *testing.T) {
		t.Parallel()
		c := ScraperConfig{}
		got := c.WorkerCountOrDefault()
		if got != DefaultScraperWorkerCount() {
			t.Fatalf("WorkerCountOrDefault() = %d, want %d", got, DefaultScraperWorkerCount())
		}
	})

	t.Run("positive value preserved", func(t *testing.T) {
		t.Parallel()
		c := ScraperConfig{WorkerCount: 8}
		got := c.WorkerCountOrDefault()
		if got != 8 {
			t.Fatalf("WorkerCountOrDefault() = %d, want 8", got)
		}
	})
}
