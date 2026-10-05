package settings

import (
	"fmt"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"
)

type DistributedRateLimitConfig struct {
	Enabled    bool
	Limit      int
	Window     time.Duration
	KeyPrefix  string
	BucketBase string
}

type HolodexTransportConfig struct {
	MaxConnsPerHost     int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
}

type HolodexConcurrencyConfig struct {
	MaxConcurrentRequests int
	OrgAllParallelism     int
	// RequestDelay는 로컬 요청 간격이며 0이면 간격 제한을 적용하지 않습니다.
	RequestDelay time.Duration
}

type OfficialScheduleConfig struct {
	BaseURL      string
	Timeout      time.Duration
	PageCacheTTL time.Duration
}

type OfficialScheduleRuntimeConfig struct {
	OfficialSchedule     OfficialScheduleConfig
	MaxResponseBodyBytes int64
}

func DefaultHolodexOperationalConfig() HolodexConfig {
	return HolodexConfig{
		BaseURL:           "https://holodex.net/api/v2",
		Timeout:           25 * time.Second,
		PerAttemptTimeout: 20 * time.Second,
		MaxRetryAttempts:  3,
		Transport: HolodexTransportConfig{
			MaxConnsPerHost:     50,
			MaxIdleConnsPerHost: 50,
			IdleConnTimeout:     30 * time.Second,
		},
		Concurrency: HolodexConcurrencyConfig{
			MaxConcurrentRequests: 2,
			OrgAllParallelism:     2,
			RequestDelay:          500 * time.Millisecond,
		},
		DistributedRateLimit: DistributedRateLimitConfig{
			Enabled:    true,
			Limit:      10,
			Window:     time.Second,
			KeyPrefix:  "ratelimit:sliding",
			BucketBase: "holodex:api",
		},
	}
}

func DefaultOfficialScheduleConfig() OfficialScheduleConfig {
	return OfficialScheduleConfig{
		BaseURL:      "https://schedule.hololive.tv",
		Timeout:      15 * time.Second,
		PageCacheTTL: 15 * time.Second,
	}
}

func loadMaxResponseBodyBytes() (int64, error) {
	maxBytes, err := sharedenv.Int64E("MAX_RESPONSE_BODY_BYTES", DefaultMaxResponseBodyBytes)
	if err != nil {
		return 0, fmt.Errorf("load max response body bytes: %w", err)
	}

	return maxBytes, nil
}

const DefaultMaxResponseBodyBytes int64 = 2 << 20
