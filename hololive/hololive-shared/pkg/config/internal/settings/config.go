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

package settings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/park285/iris-client-go/iris"
	sharedenv "github.com/park285/shared-go/pkg/envutil"
	"github.com/park285/shared-go/pkg/workerconfig"
)

type Config struct {
	Iris                 IrisConfig
	Server               ServerConfig
	Kakao                KakaoConfig
	Holodex              HolodexConfig
	YouTube              YouTubeConfig
	Ingestion            IngestionConfig
	Chzzk                ChzzkConfig
	Twitch               TwitchConfig
	Valkey               ValkeyConfig
	Postgres             PostgresConfig
	Notification         NotificationConfig
	Logging              LoggingConfig
	Bot                  BotConfig
	Services             ServicesConfig
	Environment          string
	Scraper              ScraperConfig
	Webhook              WebhookConfig
	WorkerPool           WorkerPoolConfig
	WorkerProfile        WorkerProfileConfig
	CORS                 CORSConfig
	Cliproxy             CliproxyConfig
	LLM                  LLMConfig
	Exa                  ExaConfig
	OfficialSchedule     OfficialScheduleConfig
	OfficialProfile      OfficialProfileConfig
	MaxResponseBodyBytes int64
	LLMSchedulerURL      string
	AlarmServiceURL      string
	Version              string
}

type configLoadOptions struct {
	FetchIrisWorkerProfile bool
	CORSDefaultEnforce     bool
}

func Load() (*Config, error) {
	return loadConfigValidated((*Config).Validate, configLoadOptions{FetchIrisWorkerProfile: true})
}

func LoadAdminAPIRuntime() (*Config, error) {
	return loadConfigValidated((*Config).ValidateAdminAPIRuntime, configLoadOptions{CORSDefaultEnforce: true})
}

// LoadYouTubeProducerRuntime: youtube-producer는 compose 보안 계약상 nonEgress라
// Iris egress 토큰·KAKAO_ROOMS를 받지 않으므로 해당 필수 검증을 면제합니다.
func LoadYouTubeProducerRuntime() (*Config, error) {
	return loadConfigValidated((*Config).ValidateYouTubeProducerRuntime, configLoadOptions{})
}

func loadConfigValidated(validate func(*Config) error, options configLoadOptions) (*Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction := loadRuntimeTokensAndCORS()
	config, err := buildConfig(webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction, options)
	if err != nil {
		return nil, err
	}

	if err := validate(config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return config, nil
}

func buildConfig(
	webhookToken, botToken string,
	corsAllowedOrigins []string,
	corsMissingInProduction bool,
	options configLoadOptions,
) (*Config, error) {
	communityShortsBigBangCutoverAt, err := loadCommunityShortsBigBangCutoverAt()
	if err != nil {
		return nil, err
	}
	irisConfig := loadIrisConfig(webhookToken, botToken)
	workerProfile := workerconfig.LegacyIrisBotWebhookWorkerProfile()
	if options.FetchIrisWorkerProfile {
		workerProfile, err = fetchIrisBotWebhookWorkerProfile(&irisConfig)
		if err != nil && !errors.Is(err, workerconfig.ErrWorkerProfileDisabled) {
			return nil, fmt.Errorf("fetch Iris bot webhook worker profile: %w", err)
		}
	}

	return &Config{
		Iris:    irisConfig,
		Server:  loadServerConfig(),
		Kakao:   loadKakaoConfig(),
		Holodex: loadHolodexConfig(),
		YouTube: loadYouTubeConfig(),
		Ingestion: IngestionConfig{
			YouTubeEnabled:                  sharedenv.Bool("YOUTUBE_INGESTION_ENABLED", true),
			PhotoSyncEnabled:                sharedenv.Bool("PHOTO_SYNC_ENABLED", true),
			CommunityShortsBigBangCutoverAt: communityShortsBigBangCutoverAt,
		},
		Valkey:       loadValkeyConfig(),
		Postgres:     loadPostgresConfig(),
		Notification: loadNotificationConfig(),
		Logging:      loadLoggingConfig(),
		Bot:          loadBotConfig(),
		Services:     loadServicesConfig(),
		Environment:  loadAppEnvironment(),
		Scraper:      loadScraperConfig(),
		Webhook:      loadWebhookConfig(&workerProfile),
		WorkerPool:   loadWorkerPoolConfig(&workerProfile),
		WorkerProfile: WorkerProfileConfig{
			Version: workerProfile.Version,
			Hash:    workerProfile.ProfileHash(),
		},
		Chzzk:                loadChzzkConfig(),
		Twitch:               loadTwitchConfig(),
		Cliproxy:             loadCliproxyConfig(),
		LLM:                  loadLLMConfig(),
		Exa:                  loadExaConfig(),
		OfficialSchedule:     loadOfficialScheduleConfig(),
		OfficialProfile:      loadOfficialProfileConfig(),
		MaxResponseBodyBytes: int64(sharedenv.Int("MAX_RESPONSE_BODY_BYTES", int(DefaultMaxResponseBodyBytes))),
		LLMSchedulerURL:      sharedenv.String("LLM_SCHEDULER_INTERNAL_URL", ""),
		AlarmServiceURL:      sharedenv.String("ALARM_INTERNAL_URL", ""),
		CORS:                 loadCORSConfig(corsAllowedOrigins, corsMissingInProduction, options),
		Version:              sharedenv.String("APP_VERSION", "1.1.0-go"),
	}, nil
}

func loadCORSConfig(
	corsAllowedOrigins []string,
	corsMissingInProduction bool,
	options configLoadOptions,
) CORSConfig {
	return CORSConfig{
		AllowedOrigins:      corsAllowedOrigins,
		Enforce:             sharedenv.Bool("CORS_ENFORCE", options.CORSDefaultEnforce),
		MissingInProduction: corsMissingInProduction,
	}
}

func loadServicesConfig() ServicesConfig {
	return ServicesConfig{
		LLMSchedulerHealthURL: sharedenv.StringAny(
			"SERVICES_LLM_SCHEDULER_HEALTH_URL",
			"SERVICES_LLM_SERVER_HEALTH_URL",
		),
		GameBotTwentyQHealthURL: sharedenv.String("SERVICES_GAME_BOT_TWENTYQ_HEALTH_URL", ""),
		GameBotTurtleHealthURL:  sharedenv.String("SERVICES_GAME_BOT_TURTLE_HEALTH_URL", ""),
	}
}

func loadIrisConfig(webhookToken, botToken string) IrisConfig {
	return IrisConfig{
		BaseURL:                   sharedenv.String("IRIS_BASE_URL", ""),
		BaseURLFile:               sharedenv.String("IRIS_BASE_URL_FILE", ""),
		WebhookToken:              webhookToken,
		BotToken:                  botToken,
		HTTPTimeout:               time.Duration(sharedenv.Int("IRIS_HTTP_TIMEOUT_SECONDS", 10)) * time.Second,
		HTTPDialTimeout:           time.Duration(sharedenv.Int("IRIS_HTTP_DIAL_TIMEOUT_SECONDS", 3)) * time.Second,
		HTTPResponseHeaderTimeout: time.Duration(sharedenv.Int("IRIS_HTTP_RESP_HEADER_TIMEOUT_SECONDS", 5)) * time.Second,
	}
}

func fetchIrisBotWebhookWorkerProfile(config *IrisConfig) (profile workerconfig.IrisBotWebhookWorkerProfile, err error) {
	if strings.TrimSpace(config.BotToken) == "" {
		return workerconfig.LegacyIrisBotWebhookWorkerProfile(), workerconfig.ErrWorkerProfileDisabled
	}
	baseURL, err := resolveIrisBaseURL(config)
	if err != nil {
		return profile, err
	}
	irisClient, err := iris.NewClient(
		iris.WithBaseURL(baseURL),
		iris.WithBotToken(config.BotToken),
		iris.WithTransport(sharedenv.String("IRIS_TRANSPORT", "")),
		iris.WithTimeout(config.HTTPTimeout),
		iris.WithDialTimeout(config.HTTPDialTimeout),
		iris.WithResponseHeaderTimeout(config.HTTPResponseHeaderTimeout),
	)
	if err != nil {
		return profile, err
	}
	defer func() {
		if closeErr := irisClient.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close Iris client: %w", closeErr))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), config.HTTPTimeout)
	defer cancel()

	payload, err := irisClient.FetchBotWebhookWorkerProfile(ctx)
	if err != nil {
		return profile, err
	}
	if payload == nil {
		return profile, fmt.Errorf("empty worker profile response")
	}
	if err := jsonUnmarshalStrict(payload, &profile); err != nil {
		return profile, fmt.Errorf("decode worker profile: %w", err)
	}
	if err := profile.Validate(); err != nil {
		return profile, fmt.Errorf("validate worker profile: %w", err)
	}
	return profile, nil
}

func jsonUnmarshalStrict(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out)
}
