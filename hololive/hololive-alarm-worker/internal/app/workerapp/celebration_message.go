package workerapp

import (
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func renderCelebrationMessage(envelope *domain.AlarmQueueEnvelope) (string, error) {
	if envelope.Celebration == nil {
		return "", fmt.Errorf("render celebration: payload is nil")
	}
	p := envelope.Celebration
	switch p.Kind {
	case domain.CelebrationKindBirthday:
		return renderBirthdayCelebrationMessage(p), nil
	case domain.CelebrationKindAnniversary:
		return renderAnniversaryCelebrationMessage(p)
	default:
		return "", fmt.Errorf("render celebration: unknown kind %q", p.Kind)
	}
}

func renderBirthdayCelebrationMessage(p *domain.CelebrationDispatchPayload) string {
	msg := fmt.Sprintf("🎂 %s 생일 축하합니다!", p.MemberName)
	if p.Ordinal > 0 {
		msg = fmt.Sprintf("🎂 %s %d번째 생일 축하합니다!", p.MemberName, p.Ordinal)
	}
	return appendCelebrationChannelLink(msg, p.ChannelID)
}

func renderAnniversaryCelebrationMessage(p *domain.CelebrationDispatchPayload) (string, error) {
	if p.Years <= 0 {
		return "", fmt.Errorf("render celebration: anniversary years must be positive, got %d", p.Years)
	}
	msg := fmt.Sprintf("🎉 %s 데뷔 %d주년 축하합니다!", p.MemberName, p.Years)
	return appendCelebrationChannelLink(msg, p.ChannelID), nil
}

func appendCelebrationChannelLink(msg, channelID string) string {
	if channelID == "" {
		return msg
	}
	return msg + "\n🔗 " + youtubeChannelURL(channelID)
}

func youtubeChannelURL(channelID string) string {
	return "https://youtube.com/channel/" + channelID
}
