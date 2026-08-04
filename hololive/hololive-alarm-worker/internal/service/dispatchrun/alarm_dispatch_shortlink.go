package dispatchrun

import (
	"errors"
	"fmt"
	"os"

	shortlinkservice "github.com/kapu/hololive-shared/pkg/service/shortlink"
)

const alarmShortLinkBaseURLEnv = "ALARM_SHORT_LINK_BASE_URL"

// ValidateAlarmShortLinkConfig는 섬네일 없는 텍스트 링크와 Karing 카드가 동시에 켜지는 구성을 거부합니다.
func ValidateAlarmShortLinkConfig(karingEnabled bool) error {
	builder, err := configuredAlarmShortLinkBuilder()
	if err != nil {
		return err
	}
	if builder.Enabled() && karingEnabled {
		return errors.New("ALARM_SHORT_LINK_BASE_URL requires ALARM_DISPATCH_KARING_ENABLED=false")
	}
	return nil
}

func configuredAlarmShortLinkBuilder() (shortlinkservice.YouTubeBuilder, error) {
	builder, err := shortlinkservice.NewYouTubeBuilder(os.Getenv(alarmShortLinkBaseURLEnv))
	if err != nil {
		return shortlinkservice.YouTubeBuilder{}, fmt.Errorf("%s: %w", alarmShortLinkBaseURLEnv, err)
	}
	return builder, nil
}
