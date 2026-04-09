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
	workerCount                int
}

// WithChannelPollerRegistrations: 채널 폴러 등록 정책을 주입한다.
func WithChannelPollerRegistrations(registrations []ChannelPollerRegistration) ScraperSchedulerOption {
	copied := make([]ChannelPollerRegistration, len(registrations))
	copy(copied, registrations)

	return func(options *scraperSchedulerOptions) {
		options.channelPollerRegistrations = copied
	}
}

// WithSchedulerWorkerCount: scraper scheduler worker 수를 주입한다.
func WithSchedulerWorkerCount(workerCount int) ScraperSchedulerOption {
	return func(options *scraperSchedulerOptions) {
		options.workerCount = workerCount
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
