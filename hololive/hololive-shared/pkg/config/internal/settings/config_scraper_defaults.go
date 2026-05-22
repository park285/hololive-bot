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

func DefaultScraperPublishedAtResolverConfig() ScraperPublishedAtResolverConfig {
	return ScraperPublishedAtResolverConfig{
		Enabled:           true,
		Interval:          3 * time.Minute,
		BatchSize:         10,
		MaxResolvePerRun:  1,
		MaxRunDuration:    12 * time.Second,
		ResolveTimeout:    10 * time.Second,
		MinDetectedAge:    30 * time.Second,
		FailureBackoffTTL: 5 * time.Minute,
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

func (c ScraperConfig) PollOrDefault() ScraperPoll {
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

func (c ScraperConfig) SchedulerOrDefault() ScraperSchedulerConfig {
	defaults := DefaultScraperSchedulerConfig()
	config := c.Scheduler

	if config.PollTimeout <= 0 {
		config.PollTimeout = defaults.PollTimeout
	}
	if config.ErrorBackoffMin <= 0 {
		config.ErrorBackoffMin = defaults.ErrorBackoffMin
	}
	if config.ErrorBackoffMax <= 0 {
		config.ErrorBackoffMax = defaults.ErrorBackoffMax
	}
	if config.ErrorBackoffMax < config.ErrorBackoffMin {
		config.ErrorBackoffMax = config.ErrorBackoffMin
	}

	return config
}

func (c ScraperConfig) SnapshotOrDefault() ScraperSnapshotConfig {
	defaults := DefaultScraperSnapshotConfig()
	config := c.Snapshot
	if strings.TrimSpace(config.Dir) == "" {
		config.Dir = defaults.Dir
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = defaults.MaxBodyBytes
	}
	if config.MinInterval <= 0 {
		config.MinInterval = defaults.MinInterval
	}
	return config
}

func (c ScraperConfig) ChannelHealthOrDefault() ScraperChannelHealthConfig {
	defaults := DefaultScraperChannelHealthConfig()
	config := c.ChannelHealth
	fillDefaultDuration(&config.TTL, defaults.TTL)
	fillDefaultDuration(&config.ParserDriftBase, defaults.ParserDriftBase)
	fillDefaultDuration(&config.ParserDriftMax, defaults.ParserDriftMax)
	fillDefaultDuration(&config.TransportBase, defaults.TransportBase)
	fillDefaultDuration(&config.TransportMax, defaults.TransportMax)
	fillDefaultDuration(&config.TimeoutBase, defaults.TimeoutBase)
	fillDefaultDuration(&config.TimeoutMax, defaults.TimeoutMax)
	fillDefaultDuration(&config.HTTPStatusBase, defaults.HTTPStatusBase)
	fillDefaultDuration(&config.HTTPStatusMax, defaults.HTTPStatusMax)
	if config.SuccessDecaySteps <= 0 {
		config.SuccessDecaySteps = defaults.SuccessDecaySteps
	}
	return config
}

func fillDefaultDuration(value *time.Duration, fallback time.Duration) {
	if value != nil && *value <= 0 {
		*value = fallback
	}
}

func (c ScraperConfig) BrowserDiagnosticOrDefault() ScraperBrowserDiagnosticConfig {
	defaults := DefaultScraperBrowserDiagnosticConfig()
	config := c.BrowserDiagnostic
	if config.Timeout <= 0 {
		config.Timeout = defaults.Timeout
	}
	return config
}

func (c ScraperConfig) WorkerCountOrDefault() int {
	if c.WorkerCount > 0 {
		return c.WorkerCount
	}

	return DefaultScraperWorkerCount()
}
