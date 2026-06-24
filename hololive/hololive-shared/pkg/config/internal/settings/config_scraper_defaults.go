package settings

import (
	"strings"
	"time"
)

func DefaultScraperPoll() ScraperPoll {
	return ScraperPoll{
		Videos:    15 * time.Minute,
		Shorts:    6 * time.Minute,
		Community: 15 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      2 * time.Minute,
	}
}

func DefaultScraperSchedulerConfig() ScraperSchedulerConfig {
	return ScraperSchedulerConfig{
		PollTimeout:     45 * time.Second,
		ErrorBackoffMin: 30 * time.Second,
		ErrorBackoffMax: 5 * time.Minute,
	}
}

func DefaultScraperSnapshotConfig() ScraperSnapshotConfig {
	return ScraperSnapshotConfig{
		Enabled:      false,
		Dir:          "./artifacts/youtube-producer",
		MaxBodyBytes: 512 << 10,
		MinInterval:  30 * time.Minute,
	}
}

func DefaultScraperChannelHealthConfig() ScraperChannelHealthConfig {
	return ScraperChannelHealthConfig{
		Enabled:           true,
		Enforce:           false,
		TTL:               24 * time.Hour,
		ParserDriftBase:   10 * time.Minute,
		ParserDriftMax:    6 * time.Hour,
		TransportBase:     2 * time.Minute,
		TransportMax:      30 * time.Minute,
		TimeoutBase:       2 * time.Minute,
		TimeoutMax:        30 * time.Minute,
		HTTPStatusBase:    5 * time.Minute,
		HTTPStatusMax:     time.Hour,
		SuccessDecaySteps: 1,
	}
}

func DefaultScraperPollTieringConfig() ScraperPollTieringConfig {
	return ScraperPollTieringConfig{Enabled: false}
}

func DefaultScraperActiveActiveConfig() ScraperActiveActiveConfig {
	return ScraperActiveActiveConfig{
		Enabled:   false,
		Namespace: "production",
	}
}

func DefaultScraperBrowserDiagnosticConfig() ScraperBrowserDiagnosticConfig {
	return ScraperBrowserDiagnosticConfig{
		Enabled: false,
		Timeout: 20 * time.Second,
	}
}

func (p ScraperPoll) EstimatedRequestsPerMinute() float64 {
	var rpm float64
	if p.Videos > 0 {
		rpm += 60.0 / p.Videos.Seconds()
	}
	if p.Shorts > 0 {
		rpm += 60.0 / p.Shorts.Seconds()
	}
	if p.Community > 0 {
		rpm += 60.0 / p.Community.Seconds()
	}
	if p.Stats > 0 {
		rpm += 60.0 / p.Stats.Seconds()
	}
	if p.Live > 0 {
		rpm += 60.0 / p.Live.Seconds()
	}
	return rpm
}

func (c *ScraperConfig) PollOrDefault() ScraperPoll {
	poll := DefaultScraperPoll()

	if c.Poll.Videos > 0 {
		poll.Videos = c.Poll.Videos
	}
	if c.Poll.Shorts > 0 {
		poll.Shorts = c.Poll.Shorts
	}
	if c.Poll.Community > 0 {
		poll.Community = c.Poll.Community
	}
	if c.Poll.Stats > 0 {
		poll.Stats = c.Poll.Stats
	}
	if c.Poll.Live > 0 {
		poll.Live = c.Poll.Live
	}

	return poll
}

func (c *ScraperConfig) SchedulerOrDefault() ScraperSchedulerConfig {
	defaults := DefaultScraperSchedulerConfig()
	cfg := c.Scheduler

	if cfg.PollTimeout <= 0 {
		cfg.PollTimeout = defaults.PollTimeout
	}
	if cfg.ErrorBackoffMin <= 0 {
		cfg.ErrorBackoffMin = defaults.ErrorBackoffMin
	}
	if cfg.ErrorBackoffMax <= 0 {
		cfg.ErrorBackoffMax = defaults.ErrorBackoffMax
	}
	if cfg.ErrorBackoffMax < cfg.ErrorBackoffMin {
		cfg.ErrorBackoffMax = cfg.ErrorBackoffMin
	}

	return cfg
}

func (c *ScraperConfig) SnapshotOrDefault() ScraperSnapshotConfig {
	defaults := DefaultScraperSnapshotConfig()
	cfg := c.Snapshot
	if strings.TrimSpace(cfg.Dir) == "" {
		cfg.Dir = defaults.Dir
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaults.MaxBodyBytes
	}
	if cfg.MinInterval <= 0 {
		cfg.MinInterval = defaults.MinInterval
	}
	return cfg
}

func (c *ScraperConfig) ChannelHealthOrDefault() ScraperChannelHealthConfig {
	defaults := DefaultScraperChannelHealthConfig()
	cfg := c.ChannelHealth
	fillDefaultDuration(&cfg.TTL, defaults.TTL)
	fillDefaultDuration(&cfg.ParserDriftBase, defaults.ParserDriftBase)
	fillDefaultDuration(&cfg.ParserDriftMax, defaults.ParserDriftMax)
	fillDefaultDuration(&cfg.TransportBase, defaults.TransportBase)
	fillDefaultDuration(&cfg.TransportMax, defaults.TransportMax)
	fillDefaultDuration(&cfg.TimeoutBase, defaults.TimeoutBase)
	fillDefaultDuration(&cfg.TimeoutMax, defaults.TimeoutMax)
	fillDefaultDuration(&cfg.HTTPStatusBase, defaults.HTTPStatusBase)
	fillDefaultDuration(&cfg.HTTPStatusMax, defaults.HTTPStatusMax)
	if cfg.SuccessDecaySteps <= 0 {
		cfg.SuccessDecaySteps = defaults.SuccessDecaySteps
	}
	return cfg
}

func fillDefaultDuration(value *time.Duration, fallback time.Duration) {
	if value != nil && *value <= 0 {
		*value = fallback
	}
}

func (c *ScraperConfig) BrowserDiagnosticOrDefault() ScraperBrowserDiagnosticConfig {
	defaults := DefaultScraperBrowserDiagnosticConfig()
	cfg := c.BrowserDiagnostic
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaults.Timeout
	}
	return cfg
}

func (c *ScraperConfig) WorkerCountOrDefault() int {
	if c.WorkerCount > 0 {
		return c.WorkerCount
	}

	return DefaultScraperWorkerCount()
}
