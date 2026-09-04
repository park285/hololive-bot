package dispatchrun

import (
	"fmt"
	"strings"

	shortlinkservice "github.com/kapu/hololive-shared/pkg/service/shortlink"
)

const (
	alarmShortLinkBaseURLEnv = "ALARM_SHORT_LINK_BASE_URL"
	alarmShortLinkOrigin     = "https://short.holoshi.com"
)

// ValidateAlarmShortLinkConfig는 grouped message의 short-link origin을 검증합니다.
func ValidateAlarmShortLinkConfig(baseURL string) error {
	if _, err := configuredAlarmShortLinkBuilder(baseURL); err != nil {
		return fmt.Errorf("configured alarm short link builder: %w", err)
	}

	return nil
}

func configuredAlarmShortLinkBuilder(baseURL string) (shortlinkservice.YouTubeBuilder, error) {
	rawOrigin := strings.TrimSpace(baseURL)

	builder, err := shortlinkservice.NewYouTubeBuilder(rawOrigin)
	if err != nil {
		return shortlinkservice.YouTubeBuilder{}, fmt.Errorf("%s: %w", alarmShortLinkBaseURLEnv, err)
	}

	if builder.Enabled() && strings.TrimSuffix(rawOrigin, "/") != alarmShortLinkOrigin {
		return shortlinkservice.YouTubeBuilder{}, fmt.Errorf(
			"%s must be %s when enabled",
			alarmShortLinkBaseURLEnv,
			alarmShortLinkOrigin,
		)
	}

	return builder, nil
}
