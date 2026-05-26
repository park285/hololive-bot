package workerapp

import (
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func renderCelebrationMessage(envelope domain.AlarmQueueEnvelope) (string, error) {
	if envelope.Celebration == nil {
		return "", fmt.Errorf("render celebration: payload is nil")
	}
	p := envelope.Celebration
	switch p.Kind {
	case domain.CelebrationKindBirthday:
		msg := fmt.Sprintf("🎂 %s 생일 축하합니다!", p.MemberName)
		if p.ChannelID != "" {
			msg += "\n🔗 " + youtubeChannelURL(p.ChannelID)
		}
		return msg, nil
	case domain.CelebrationKindAnniversary:
		if p.Years <= 0 {
			return "", fmt.Errorf("render celebration: anniversary years must be positive, got %d", p.Years)
		}
		msg := fmt.Sprintf("🎉 %s 데뷔 %d주년 축하합니다!", p.MemberName, p.Years)
		if p.ChannelID != "" {
			msg += "\n🔗 " + youtubeChannelURL(p.ChannelID)
		}
		return msg, nil
	default:
		return "", fmt.Errorf("render celebration: unknown kind %q", p.Kind)
	}
}

func youtubeChannelURL(channelID string) string {
	return "https://youtube.com/channel/" + channelID
}
