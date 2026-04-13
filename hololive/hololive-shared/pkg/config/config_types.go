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

import "time"

type ServerConfig struct {
	Port   int
	APIKey string // API 인증용 시크릿 키 (X-API-Key 헤더로 검증)
}

type HolodexConfig struct {
	BaseURL string
	APIKey  string
}

type YouTubeConfig struct {
	APIKey              string
	EnableQuotaBuilding bool
}

type IngestionConfig struct {
	YouTubeEnabled                  bool
	PhotoSyncEnabled                bool
	CommunityShortsBigBangEnabled   bool
	CommunityShortsBigBangCutoverAt time.Time
}

type LoggingConfig struct {
	Level      string
	Dir        string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
}

type BotConfig struct {
	Prefix        string
	SelfUser      string
	AdminEnabled  bool
	MentionPrefix string // 멘션 기반 명령어 접두사 (예: @카푸봇)
}

type ServicesConfig struct {
	LLMSchedulerHealthURL   string
	GameBotTwentyQHealthURL string
	GameBotTurtleHealthURL  string
}

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

type ScraperConfig struct {
	ProxyEnabled        bool
	ProxyURL            string // SOCKS5 프록시 URL (예: socks5://user:pass@host:1080)
	WorkerCount         int
	Poll                ScraperPoll
	PublishedAtResolver ScraperPublishedAtResolverConfig
}

func DefaultScraperPoll() ScraperPoll {
	return ScraperPoll{
		Videos:    15 * time.Minute,
		Shorts:    6 * time.Minute,
		Community: 15 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      10 * time.Minute,
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

func (c ScraperConfig) WorkerCountOrDefault() int {
	if c.WorkerCount > 0 {
		return c.WorkerCount
	}

	return DefaultScraperWorkerCount()
}

type CORSConfig struct {
	AllowedOrigins      []string
	Enforce             bool
	MissingInProduction bool
}

type WebhookConfig struct {
	WorkerCount    int
	QueueSize      int
	EnqueueTimeout time.Duration
	HandlerTimeout time.Duration
	RequireHTTP2   bool
}

type ChzzkConfig struct {
	ClientID     string
	ClientSecret string
}

type TwitchConfig struct {
	ClientID     string
	ClientSecret string
}
