package alarmdispatch

import (
	"fmt"
	"strings"

	shortlinkservice "github.com/kapu/hololive-shared/pkg/service/shortlink"
)

const (
	alarmShortLinkBaseURLEnv = "ALARM_SHORT_LINK_BASE_URL"
	alarmShortLinkOrigin     = "https://short.holoshi.com"
)

// NewAlarmShortLinkBuilder는 grouped message의 short-link builder를 만든다. 빈 값은 비활성 builder이고, 활성
// origin은 alarmShortLinkOrigin만 허용한다.
func NewAlarmShortLinkBuilder(baseURL string) (shortlinkservice.YouTubeBuilder, error) {
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
