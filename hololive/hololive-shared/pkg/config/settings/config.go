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
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/park285/iris-client-go/v4/iris"
	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/envload"
)

// LoadIrisConfig는 Iris egress 클라이언트 입력(IRIS_*)을 읽는다. 필수 여부 판단은 runtime이
// ValidateIrisEgressInputs로 결정한다.
func LoadIrisConfig() (IrisConfig, error) {
	webhookToken, botToken := LoadIrisTokens()

	var env envload.StrictEnv

	config := IrisConfig{
		BaseURL:                 sharedenv.String("IRIS_BASE_URL", ""),
		BaseURLFile:             sharedenv.String("IRIS_BASE_URL_FILE", ""),
		WebhookToken:            webhookToken,
		BotToken:                botToken,
		HTTPTimeout:             env.Seconds("IRIS_HTTP_TIMEOUT_SECONDS", 10*time.Second),
		H3CACertFile:            sharedenv.String("IRIS_H3_CA_CERT_FILE", ""),
		H3ServerName:            sharedenv.String("IRIS_H3_SERVER_NAME", ""),
		BaseURLAllowedHosts:     splitIrisAllowedHosts(os.Getenv("IRIS_BASE_URL_ALLOWED_HOSTS")),
		ValidateBaseURLFileStat: strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production"),
	}

	if err := env.Err(); err != nil {
		return IrisConfig{}, fmt.Errorf("load iris config: %w", err)
	}

	transport, err := parseIrisTransport(os.Getenv("IRIS_TRANSPORT"))
	if err != nil {
		return IrisConfig{}, fmt.Errorf("load iris config: %w", err)
	}

	config.Transport = transport

	return config, nil
}

// parseIrisTransport는 IRIS_TRANSPORT를 SDK 어휘(h3, http1)로 해석한다. 빈 값은 SDK 기본 h3이고,
// quic·http3 같은 옛 별칭은 기동 시 거절한다.
func parseIrisTransport(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return iris.TransportH3, nil
	}

	transport, err := iris.ParseTransport(raw)
	if err != nil {
		return "", fmt.Errorf("IRIS_TRANSPORT: %w", err)
	}

	return transport, nil
}

// splitIrisAllowedHosts는 쉼표 구분 allowlist에서 빈 항목을 버린다. 빈 항목만 있으면 미설정과 같다.
func splitIrisAllowedHosts(raw string) []string {
	var hosts []string

	for host := range strings.SplitSeq(raw, ",") {
		if host = strings.TrimSpace(host); host != "" {
			hosts = append(hosts, host)
		}
	}

	return hosts
}

// LoadKakaoConfig는 room ACL 초기값을 읽는다. 반환값은 잠금을 포함하므로 호출자는 값 복사 없이 필드를 옮긴다.
func LoadKakaoConfig() (*KakaoConfig, error) {
	enabled, err := loadKakaoACLEnabled()
	if err != nil {
		return nil, fmt.Errorf("load kakao ACL enabled: %w", err)
	}

	mode, err := loadKakaoACLMode()
	if err != nil {
		return nil, fmt.Errorf("load kakao ACL mode: %w", err)
	}

	// KAKAO_ROOMS는 ACL 초기 seed이며 정규 signed i64 chatID 목록이 필수다.
	return &KakaoConfig{
		Rooms:      envload.CommaSeparated(sharedenv.String("KAKAO_ROOMS", "")),
		ACLEnabled: enabled,
		ACLMode:    mode,
	}, nil
}

func loadKakaoACLEnabled() (bool, error) {
	const key = kakaoACLEnabledEnv

	raw, found := os.LookupEnv(key)

	if !found {
		return true, nil
	}

	if strings.TrimSpace(raw) == "" {
		return false, fmt.Errorf("%s must not be empty", key)
	}

	enabled, err := sharedenv.BoolE(key, true)
	if err != nil {
		return false, fmt.Errorf("read bool env: %w", err)
	}

	return enabled, nil
}

func loadKakaoACLMode() (string, error) {
	const key = kakaoACLModeEnv

	raw, found := os.LookupEnv(key)

	if !found {
		return "whitelist", nil
	}

	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "whitelist", "blacklist":
		return mode, nil
	default:
		return "", fmt.Errorf("invalid %s: %q", key, raw)
	}
}

func LoadLoggingConfig() (LoggingConfig, error) {
	var env envload.StrictEnv

	config := LoggingConfig{
		Level:      sharedenv.String("LOG_LEVEL", "info"),
		Dir:        sharedenv.String("LOG_DIR", ""),
		MaxSizeMB:  env.Int("LOG_MAX_SIZE_MB", 5),
		MaxBackups: env.Int("LOG_MAX_BACKUPS", 5),
		MaxAgeDays: env.Int("LOG_MAX_AGE_DAYS", 30),
		Compress:   env.Bool("LOG_COMPRESS", true),
	}

	if err := env.Err(); err != nil {
		return LoggingConfig{}, fmt.Errorf("load logging config: %w", err)
	}

	return config, nil
}

// LoadHolodexConfig는 Holodex 클라이언트 운영값을 읽는다. 범위 검증은 ValidateHolodexConfig가 소유한다.
func LoadHolodexConfig() (HolodexConfig, error) {
	apiKey := envload.HolodexAPIKey()

	d := DefaultHolodexOperationalConfig()

	var env envload.StrictEnv

	config := HolodexConfig{
		BaseURL:           sharedenv.String("HOLODEX_BASE_URL", d.BaseURL),
		APIKey:            apiKey,
		Timeout:           env.Seconds("HOLODEX_TIMEOUT_SECONDS", d.Timeout),
		PerAttemptTimeout: env.Seconds("HOLODEX_PER_ATTEMPT_TIMEOUT_SECONDS", d.PerAttemptTimeout),
		MaxRetryAttempts:  env.Int("HOLODEX_MAX_RETRY_ATTEMPTS", d.MaxRetryAttempts),
		Transport: HolodexTransportConfig{
			MaxConnsPerHost:     env.Int("HOLODEX_MAX_CONNS_PER_HOST", d.Transport.MaxConnsPerHost),
			MaxIdleConnsPerHost: env.Int("HOLODEX_MAX_IDLE_CONNS_PER_HOST", d.Transport.MaxIdleConnsPerHost),
			IdleConnTimeout:     env.Seconds("HOLODEX_IDLE_CONN_TIMEOUT_SECONDS", d.Transport.IdleConnTimeout),
		},
		Concurrency: HolodexConcurrencyConfig{
			MaxConcurrentRequests: env.Int("HOLODEX_MAX_CONCURRENT_REQUESTS", d.Concurrency.MaxConcurrentRequests),
			OrgAllParallelism:     env.Int("HOLODEX_ORG_ALL_PARALLELISM", d.Concurrency.OrgAllParallelism),
			RequestDelay:          env.Millis("HOLODEX_REQUEST_DELAY_MS", d.Concurrency.RequestDelay),
		},
		DistributedRateLimit: DistributedRateLimitConfig{
			Enabled:    env.Bool("HOLODEX_DISTRIBUTED_RATELIMIT_ENABLED", d.DistributedRateLimit.Enabled),
			Limit:      env.Int("HOLODEX_DISTRIBUTED_RATELIMIT_LIMIT", d.DistributedRateLimit.Limit),
			Window:     env.Millis("HOLODEX_DISTRIBUTED_RATELIMIT_WINDOW_MS", d.DistributedRateLimit.Window),
			KeyPrefix:  sharedenv.String("HOLODEX_DISTRIBUTED_RATELIMIT_KEY_PREFIX", d.DistributedRateLimit.KeyPrefix),
			BucketBase: sharedenv.String("HOLODEX_DISTRIBUTED_RATELIMIT_BUCKET_BASE", d.DistributedRateLimit.BucketBase),
		},
	}

	if err := env.Err(); err != nil {
		return HolodexConfig{}, fmt.Errorf("load holodex config: %w", err)
	}

	return config, nil
}

// LoadOfficialScheduleRuntimeConfig는 공식 일정 클라이언트 설정과 응답 본문 상한(MAX_RESPONSE_BODY_BYTES)을 함께 읽는다.
// 두 값의 오류는 합쳐서 돌려주며, 검증은 ValidateOfficialScheduleRuntimeConfig가 소유한다.
func LoadOfficialScheduleRuntimeConfig() (OfficialScheduleRuntimeConfig, error) {
	officialSchedule, officialScheduleErr := loadOfficialScheduleConfig()
	maxResponseBodyBytes, maxResponseBodyBytesErr := loadMaxResponseBodyBytes()

	if err := errors.Join(officialScheduleErr, maxResponseBodyBytesErr); err != nil {
		return OfficialScheduleRuntimeConfig{}, err
	}

	return OfficialScheduleRuntimeConfig{
		OfficialSchedule:     officialSchedule,
		MaxResponseBodyBytes: maxResponseBodyBytes,
	}, nil
}

func loadOfficialScheduleConfig() (OfficialScheduleConfig, error) {
	d := DefaultOfficialScheduleConfig()

	var env envload.StrictEnv

	config := OfficialScheduleConfig{
		BaseURL:      sharedenv.String("OFFICIAL_SCHEDULE_BASE_URL", d.BaseURL),
		Timeout:      env.Seconds("OFFICIAL_SCHEDULE_TIMEOUT_SECONDS", d.Timeout),
		PageCacheTTL: env.Seconds("OFFICIAL_SCHEDULE_PAGE_CACHE_TTL_SECONDS", d.PageCacheTTL),
	}

	if err := env.Err(); err != nil {
		return OfficialScheduleConfig{}, fmt.Errorf("load official schedule config: %w", err)
	}

	return config, nil
}

func LoadIngestionConfig() (IngestionConfig, error) {
	photoSyncEnabled, err := sharedenv.BoolE("PHOTO_SYNC_ENABLED", true)
	if err != nil {
		return IngestionConfig{}, fmt.Errorf("load ingestion config: %w", err)
	}

	return IngestionConfig{PhotoSyncEnabled: photoSyncEnabled}, nil
}

const (
	seeMoreFoldEnv     = "BOT_SEE_MORE_FOLD"
	seeMoreFoldDefault = true
)

// LoadSeeMoreFold는 bot·llm plane이 공유하는 '전체보기' 접기 스위치를 읽는다. 기본값은 접기이며, 다른 bool env처럼
// 잘못된 값은 기본값으로 바꾸지 않고 오류로 돌려준다.
func LoadSeeMoreFold() (bool, error) {
	var env envload.StrictEnv

	fold := env.Bool(seeMoreFoldEnv, seeMoreFoldDefault)
	if err := env.Err(); err != nil {
		return false, fmt.Errorf("load see-more fold: %w", err)
	}

	return fold, nil
}

// LoadBotConfig는 채팅 명령 표시 설정(BOT_*)을 읽고 잘못된 값 오류를 모두 합쳐 돌려준다.
func LoadBotConfig() (BotConfig, error) {
	var env envload.StrictEnv

	seeMoreFold, foldErr := LoadSeeMoreFold()

	config := BotConfig{
		Prefix:                sharedenv.String("BOT_PREFIX", "!"),
		SelfUser:              sharedenv.String("BOT_SELF_USER", "iris"),
		MentionPrefix:         sharedenv.String("BOT_MENTION_PREFIX", "#kapu봇"),
		CalendarImageCacheDir: sharedenv.String("BOT_CALENDAR_IMAGE_CACHE_DIR", "data/calendar-cache"),
		CalendarEntryCacheTTL: env.Seconds("BOT_CALENDAR_ENTRY_CACHE_TTL_SECONDS", 24*time.Hour),
		SeeMoreFold:           seeMoreFold,
		MarkdownReplies:       env.Bool("BOT_MARKDOWN_REPLIES", false),
	}

	if err := errors.Join(env.Err(), foldErr); err != nil {
		return BotConfig{}, fmt.Errorf("load bot config: %w", err)
	}

	return config, nil
}
