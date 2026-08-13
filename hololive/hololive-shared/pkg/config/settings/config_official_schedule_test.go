package settings

import (
	"testing"
	"time"
)

func TestValidateOfficialScheduleConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(*OfficialScheduleConfig) int64
		wantFail bool
	}{
		{name: "valid", mutate: func(*OfficialScheduleConfig) int64 { return DefaultMaxResponseBodyBytes }},
		{name: "http base URL", mutate: func(config *OfficialScheduleConfig) int64 {
			config.BaseURL = "http://schedule.example"
			return DefaultMaxResponseBodyBytes
		}, wantFail: true},
		{name: "base URL query", mutate: func(config *OfficialScheduleConfig) int64 {
			config.BaseURL = "https://schedule.example?mode=invalid"
			return DefaultMaxResponseBodyBytes
		}, wantFail: true},
		{name: "base URL path", mutate: func(config *OfficialScheduleConfig) int64 {
			config.BaseURL = "https://schedule.example/api"
			return DefaultMaxResponseBodyBytes
		}, wantFail: true},
		{name: "zero timeout", mutate: func(config *OfficialScheduleConfig) int64 {
			config.Timeout = 0
			return DefaultMaxResponseBodyBytes
		}, wantFail: true},
		{name: "zero cache expiry", mutate: func(config *OfficialScheduleConfig) int64 {
			config.CacheExpiry = 0
			return DefaultMaxResponseBodyBytes
		}, wantFail: true},
		{name: "negative page cache TTL", mutate: func(config *OfficialScheduleConfig) int64 {
			config.PageCacheTTL = -time.Second
			return DefaultMaxResponseBodyBytes
		}, wantFail: true},
		{name: "zero body limit", mutate: func(*OfficialScheduleConfig) int64 { return 0 }, wantFail: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := DefaultOfficialScheduleConfig()
			maxBytes := test.mutate(&config)
			err := validateOfficialScheduleConfig(&config, maxBytes)
			if (err != nil) != test.wantFail {
				t.Fatalf("validateOfficialScheduleConfig() error = %v, wantFail %v", err, test.wantFail)
			}
		})
	}
}

func TestLoadOfficialScheduleRuntimeConfig(t *testing.T) {
	t.Setenv("OFFICIAL_SCHEDULE_BASE_URL", "https://schedule.example")
	t.Setenv("OFFICIAL_SCHEDULE_TIMEOUT_SECONDS", "7")
	t.Setenv("OFFICIAL_SCHEDULE_CACHE_EXPIRY_SECONDS", "600")
	t.Setenv("OFFICIAL_SCHEDULE_PAGE_CACHE_TTL_SECONDS", "9")
	t.Setenv("MAX_RESPONSE_BODY_BYTES", "12345")

	config := LoadOfficialScheduleRuntimeConfig()
	if config.OfficialSchedule.BaseURL != "https://schedule.example" {
		t.Fatalf("BaseURL = %q", config.OfficialSchedule.BaseURL)
	}
	if config.OfficialSchedule.Timeout != 7*time.Second {
		t.Fatalf("Timeout = %s", config.OfficialSchedule.Timeout)
	}
	if config.OfficialSchedule.CacheExpiry != 10*time.Minute {
		t.Fatalf("CacheExpiry = %s", config.OfficialSchedule.CacheExpiry)
	}
	if config.OfficialSchedule.PageCacheTTL != 9*time.Second {
		t.Fatalf("PageCacheTTL = %s", config.OfficialSchedule.PageCacheTTL)
	}
	if config.MaxResponseBodyBytes != 12345 {
		t.Fatalf("MaxResponseBodyBytes = %d", config.MaxResponseBodyBytes)
	}
}
