package config

import settings "github.com/kapu/hololive-shared/pkg/config/internal/settings"

//lint:ignore SA1019 legacy compatibility facade.
type AdminAPIConfig = settings.AdminAPIConfig
type BotConfig = settings.BotConfig
type CORSConfig = settings.CORSConfig
type ChzzkConfig = settings.ChzzkConfig
type CliproxyConfig = settings.CliproxyConfig
type Config = settings.Config
type ConsensusLLMConfig = settings.ConsensusLLMConfig
type ExaConfig = settings.ExaConfig
type HolodexConfig = settings.HolodexConfig
type IngestionConfig = settings.IngestionConfig
type IrisConfig = settings.IrisConfig
type KakaoConfig = settings.KakaoConfig
type LLMConfig = settings.LLMConfig
type LLMSchedulerConfig = settings.LLMSchedulerConfig
type LoggingConfig = settings.LoggingConfig
type NotificationConfig = settings.NotificationConfig
type PostgresConfig = settings.PostgresConfig
type ScraperBackfillConfig = settings.ScraperBackfillConfig
type ScraperBrowserDiagnosticConfig = settings.ScraperBrowserDiagnosticConfig
type ScraperChannelHealthConfig = settings.ScraperChannelHealthConfig
type ScraperConfig = settings.ScraperConfig
type ScraperActiveActiveConfig = settings.ScraperActiveActiveConfig
type ScraperPoll = settings.ScraperPoll
type ScraperPollTieringConfig = settings.ScraperPollTieringConfig
type ScraperPublishedAtResolverConfig = settings.ScraperPublishedAtResolverConfig
type ScraperSchedulerConfig = settings.ScraperSchedulerConfig
type ScraperSnapshotConfig = settings.ScraperSnapshotConfig
type ServerConfig = settings.ServerConfig
type ServicesConfig = settings.ServicesConfig
type TwitchConfig = settings.TwitchConfig
type ValkeyConfig = settings.ValkeyConfig
type WebhookConfig = settings.WebhookConfig
type WorkerPoolConfig = settings.WorkerPoolConfig
type WorkerProfileConfig = settings.WorkerProfileConfig
type YouTubeConfig = settings.YouTubeConfig
type DistributedRateLimitConfig = settings.DistributedRateLimitConfig
type HolodexTransportConfig = settings.HolodexTransportConfig
type HolodexConcurrencyConfig = settings.HolodexConcurrencyConfig
type OfficialScheduleConfig = settings.OfficialScheduleConfig
type OfficialProfileConfig = settings.OfficialProfileConfig

const DefaultMaxResponseBodyBytes = settings.DefaultMaxResponseBodyBytes

const (
	ScraperFetcherEngineNetHTTP         = settings.ScraperFetcherEngineNetHTTP
	ScraperFetcherEngineGoScrapy        = settings.ScraperFetcherEngineGoScrapy
	ScraperFetcherEngineBrowserSnapshot = settings.ScraperFetcherEngineBrowserSnapshot
)

var Load = settings.Load

//lint:ignore SA1019 legacy compatibility facade.
var LoadAdminAPI = settings.LoadAdminAPI
var LoadLLMScheduler = settings.LoadLLMScheduler

var DefaultScraperWorkerCount = settings.DefaultScraperWorkerCount
var DefaultScraperFetcherEngine = settings.DefaultScraperFetcherEngine
var NormalizeScraperFetcherEngine = settings.NormalizeScraperFetcherEngine
var DefaultScraperPoll = settings.DefaultScraperPoll
var DefaultScraperSchedulerConfig = settings.DefaultScraperSchedulerConfig
var DefaultScraperPublishedAtResolverConfig = settings.DefaultScraperPublishedAtResolverConfig
var DefaultScraperSnapshotConfig = settings.DefaultScraperSnapshotConfig
var DefaultScraperChannelHealthConfig = settings.DefaultScraperChannelHealthConfig
var DefaultScraperPollTieringConfig = settings.DefaultScraperPollTieringConfig
var DefaultScraperBackfillConfig = settings.DefaultScraperBackfillConfig
var DefaultScraperBrowserDiagnosticConfig = settings.DefaultScraperBrowserDiagnosticConfig
var DefaultScraperActiveActiveConfig = settings.DefaultScraperActiveActiveConfig
var DefaultHolodexOperationalConfig = settings.DefaultHolodexOperationalConfig
var DefaultYouTubeOperationalConfig = settings.DefaultYouTubeOperationalConfig
var DefaultTwitchOperationalConfig = settings.DefaultTwitchOperationalConfig
var DefaultChzzkOperationalConfig = settings.DefaultChzzkOperationalConfig
var DefaultOfficialScheduleConfig = settings.DefaultOfficialScheduleConfig
var DefaultOfficialProfileConfig = settings.DefaultOfficialProfileConfig
