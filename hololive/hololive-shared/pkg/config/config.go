// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package config

import settings "github.com/kapu/hololive-shared/pkg/config/internal/settings"

type Config = settings.Config
type HololiveAPIConfig = settings.HololiveAPIConfig
type IrisConfig = settings.IrisConfig
type ServerConfig = settings.ServerConfig
type KakaoConfig = settings.KakaoConfig
type HolodexTransportConfig = settings.HolodexTransportConfig
type HolodexConcurrencyConfig = settings.HolodexConcurrencyConfig
type DistributedRateLimitConfig = settings.DistributedRateLimitConfig
type HolodexLiveStatusFallbackConfig = settings.HolodexLiveStatusFallbackConfig
type HolodexConfig = settings.HolodexConfig
type YouTubeConfig = settings.YouTubeConfig
type IngestionConfig = settings.IngestionConfig
type ChzzkConfig = settings.ChzzkConfig
type TwitchConfig = settings.TwitchConfig
type ValkeyConfig = settings.ValkeyConfig
type PostgresConfig = settings.PostgresConfig
type NotificationConfig = settings.NotificationConfig
type LoggingConfig = settings.LoggingConfig
type BotConfig = settings.BotConfig
type ServicesConfig = settings.ServicesConfig
type ScraperConfig = settings.ScraperConfig
type WebhookConfig = settings.WebhookConfig
type WorkerPoolConfig = settings.WorkerPoolConfig
type WorkerProfileConfig = settings.WorkerProfileConfig
type CORSConfig = settings.CORSConfig
type CliproxyConfig = settings.CliproxyConfig
type LLMConfig = settings.LLMConfig
type ExaConfig = settings.ExaConfig
type OfficialScheduleConfig = settings.OfficialScheduleConfig
type OfficialProfileConfig = settings.OfficialProfileConfig
type LLMSchedulerConfig = settings.LLMSchedulerConfig
type MajorEventSchedulerConfig = settings.MajorEventSchedulerConfig
type MemberNewsSchedulerConfig = settings.MemberNewsSchedulerConfig
type ExaToolConfig = settings.ExaToolConfig
type SystemConfig = settings.SystemConfig
type ObservabilityConfig = settings.ObservabilityConfig
type MajorEventConfig = settings.MajorEventConfig

type ScraperPollingConfig = settings.ScraperPollingConfig
type ScraperBackfillConfig = settings.ScraperBackfillConfig
type ScraperRuntimeConfig = settings.ScraperRuntimeConfig
type YouTubeProducerConfig = settings.YouTubeProducerConfig
type HolodexPhotoSyncConfig = settings.HolodexPhotoSyncConfig
type YouTubeProducerRuntimeConfig = settings.YouTubeProducerRuntimeConfig
type YouTubeProducerPollerPlan = settings.YouTubeProducerPollerPlan
type YouTubeProducerPollerConfig = settings.YouTubeProducerPollerConfig
type YouTubeProducerInstanceMode = settings.YouTubeProducerInstanceMode

const (
	YouTubeProducerModeSingle      = settings.YouTubeProducerModeSingle
	YouTubeProducerModePrimary     = settings.YouTubeProducerModePrimary
	YouTubeProducerModeActive      = settings.YouTubeProducerModeActive
	YouTubeProducerModeActiveActive = settings.YouTubeProducerModeActiveActive
	YouTubeProducerModeStandby     = settings.YouTubeProducerModeStandby
	YouTubeProducerModeDrain       = settings.YouTubeProducerModeDrain
	YouTubeProducerModeDisabled    = settings.YouTubeProducerModeDisabled

	DefaultMaxResponseBodyBytes = settings.DefaultMaxResponseBodyBytes
)

var Load = settings.Load
var LoadBotRuntime = settings.LoadBotRuntime
var LoadHololiveAPIRuntime = settings.LoadHololiveAPIRuntime
var LoadAdminAPIRuntime = settings.LoadAdminAPIRuntime
var LoadAlarmWorkerRuntime = settings.LoadAlarmWorkerRuntime
var LoadYouTubeProducerRuntime = settings.LoadYouTubeProducerRuntime
var LoadLLMSchedulerRuntime = settings.LoadLLMSchedulerRuntime
var LoadYouTubeProducerRuntimeConfig = settings.LoadYouTubeProducerRuntimeConfig
var DefaultHolodexOperationalConfig = settings.DefaultHolodexOperationalConfig
var DefaultYouTubeOperationalConfig = settings.DefaultYouTubeOperationalConfig
var DefaultChzzkOperationalConfig = settings.DefaultChzzkOperationalConfig
var DefaultTwitchOperationalConfig = settings.DefaultTwitchOperationalConfig
var DefaultOfficialScheduleConfig = settings.DefaultOfficialScheduleConfig
var DefaultOfficialProfileConfig = settings.DefaultOfficialProfileConfig
