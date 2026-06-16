package settings

import (
	"strings"
	"time"
)

type ScraperPoll struct {
	Videos    time.Duration
	Shorts    time.Duration
	Community time.Duration
	Stats     time.Duration
	Live      time.Duration
}

type ScraperPublishedAtResolverConfig struct {
	Enabled           bool
	Interval          time.Duration
	BatchSize         int
	MaxResolvePerRun  int
	MaxRunDuration    time.Duration
	ResolveTimeout    time.Duration
	MinDetectedAge    time.Duration
	FailureBackoffTTL time.Duration
}

func DefaultScraperWorkerCount() int {
	return 4
}

const (
	ScraperFetcherEngineNetHTTP         = "nethttp"
	ScraperFetcherEngineGoScrapy        = "goscrapy"
	ScraperFetcherEngineBrowserSnapshot = "browser_snapshot"
)

func DefaultScraperFetcherEngine() string {
	return ScraperFetcherEngineNetHTTP
}

func NormalizeScraperFetcherEngine(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", ScraperFetcherEngineNetHTTP:
		return DefaultScraperFetcherEngine()
	case ScraperFetcherEngineGoScrapy:
		return normalized
	default:
		return normalized
	}
}

type ScraperSnapshotConfig struct {
	Enabled      bool
	Dir          string
	MaxBodyBytes int
	MinInterval  time.Duration
}

type ScraperChannelHealthConfig struct {
	Enabled           bool
	Enforce           bool
	TTL               time.Duration
	ParserDriftBase   time.Duration
	ParserDriftMax    time.Duration
	TransportBase     time.Duration
	TransportMax      time.Duration
	TimeoutBase       time.Duration
	TimeoutMax        time.Duration
	HTTPStatusBase    time.Duration
	HTTPStatusMax     time.Duration
	SuccessDecaySteps int
}

type ScraperBrowserDiagnosticConfig struct {
	Enabled  bool
	Endpoint string
	Timeout  time.Duration
}

type ScraperPollTieringConfig struct {
	Enabled bool
}

type ScraperActiveActiveConfig struct {
	Enabled    bool
	InstanceID string
	Namespace  string
}

type ScraperConfig struct {
	ProxyEnabled        bool
	ProxyURL            string
	FetcherEngine       string
	WorkerCount         int
	Scheduler           ScraperSchedulerConfig
	Poll                ScraperPoll
	PublishedAtResolver ScraperPublishedAtResolverConfig
	Snapshot            ScraperSnapshotConfig
	ChannelHealth       ScraperChannelHealthConfig
	BrowserDiagnostic   ScraperBrowserDiagnosticConfig
	PollTiering         ScraperPollTieringConfig
	Backfill            ScraperBackfillConfig
	ActiveActive        ScraperActiveActiveConfig
}

type ScraperSchedulerConfig struct {
	PollTimeout     time.Duration
	ErrorBackoffMin time.Duration
	ErrorBackoffMax time.Duration
}
