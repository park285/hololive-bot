package dispatchrun

import (
	"errors"
	"fmt"
	"strings"

	shortlinkservice "github.com/kapu/hololive-shared/pkg/service/shortlink"
)

const (
	alarmShortLinkBaseURLEnv = "ALARM_SHORT_LINK_BASE_URL"
	alarmShortLinkOrigin     = "https://short.holoshi.com"
)

// ValidateAlarmShortLinkConfig는 섬네일 없는 텍스트 링크와 Karing 카드가 동시에 켜지는 구성을 거부합니다.
func ValidateAlarmShortLinkConfig(baseURL string, karingEnabled bool) error {
	builder, err := configuredAlarmShortLinkBuilder(baseURL)
	if err != nil {
		return fmt.Errorf("configured alarm short link builder: %w", err)
	}

	if builder.Enabled() && karingEnabled {
		return errors.New("ALARM_SHORT_LINK_BASE_URL requires ALARM_DISPATCH_KARING_ENABLED=false")
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
