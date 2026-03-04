package providers

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

// ChannelPollerRegistration: 채널별로 스케줄러에 등록할 폴러/우선순위/간격 정책 단위.
type ChannelPollerRegistration struct {
	Poller   poller.Poller
	Priority poller.Priority
	Interval time.Duration
}

// NewChannelPollerRegistration: ChannelPollerRegistration 생성 헬퍼.
func NewChannelPollerRegistration(p poller.Poller, priority poller.Priority, interval time.Duration) ChannelPollerRegistration {
	return ChannelPollerRegistration{
		Poller:   p,
		Priority: priority,
		Interval: interval,
	}
}

// ScraperSchedulerOption: ProvideScraperScheduler 구성 옵션.
type ScraperSchedulerOption func(*scraperSchedulerOptions)

type scraperSchedulerOptions struct {
	channelPollerRegistrations []ChannelPollerRegistration
}

// WithChannelPollerRegistrations: 채널 폴러 등록 정책을 주입한다.
func WithChannelPollerRegistrations(registrations []ChannelPollerRegistration) ScraperSchedulerOption {
	copied := make([]ChannelPollerRegistration, len(registrations))
	copy(copied, registrations)

	return func(options *scraperSchedulerOptions) {
		options.channelPollerRegistrations = copied
	}
}

func resolveScraperSchedulerOptions(opts ...ScraperSchedulerOption) scraperSchedulerOptions {
	resolved := scraperSchedulerOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&resolved)
		}
	}
	return resolved
}
