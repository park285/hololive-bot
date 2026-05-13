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

type ChannelPollerRegistration struct {
	Poller                      poller.Poller
	Priority                    poller.Priority
	Interval                    time.Duration
	ChannelIDs                  []string
	HasExplicitChannelIDs       bool
	TargetGroup                 ChannelTargetGroup
	RequestsPerRun              int
	WorstCaseAttempts           int
	WorstCaseRequestUnitsPerRun float64
}

type ChannelTargetGroup string

const (
	ChannelTargetGroupDefault      ChannelTargetGroup = "default"
	ChannelTargetGroupNotification ChannelTargetGroup = "notification"
	ChannelTargetGroupActive       ChannelTargetGroup = "notification_active"
	ChannelTargetGroupWarm         ChannelTargetGroup = "notification_warm"
	ChannelTargetGroupCold         ChannelTargetGroup = "notification_cold"
	ChannelTargetGroupStats        ChannelTargetGroup = "stats"
	ChannelTargetGroupGlobal       ChannelTargetGroup = "global"

	SyntheticGlobalPollerChannelID = "__global__"
)

func NewChannelPollerRegistration(p poller.Poller, priority poller.Priority, interval time.Duration) ChannelPollerRegistration {
	return ChannelPollerRegistration{
		Poller:         p,
		Priority:       priority,
		Interval:       interval,
		TargetGroup:    ChannelTargetGroupDefault,
		RequestsPerRun: 1,
	}
}

func (r ChannelPollerRegistration) WithChannelIDs(channelIDs []string) ChannelPollerRegistration {
	r.ChannelIDs = append([]string(nil), channelIDs...)
	r.HasExplicitChannelIDs = true
	return r
}

func (r ChannelPollerRegistration) WithTargetGroup(group ChannelTargetGroup) ChannelPollerRegistration {
	r.TargetGroup = group
	return r
}

func (r ChannelPollerRegistration) WithRequestsPerRun(requestsPerRun int) ChannelPollerRegistration {
	if requestsPerRun > 0 {
		r.RequestsPerRun = requestsPerRun
	}
	return r
}

func (r ChannelPollerRegistration) WithWorstCaseAttempts(attempts int) ChannelPollerRegistration {
	if attempts > 0 {
		r.WorstCaseAttempts = attempts
	}
	return r
}

func (r ChannelPollerRegistration) WithWorstCaseRequestUnitsPerRun(units float64) ChannelPollerRegistration {
	if units > 0 {
		r.WorstCaseRequestUnitsPerRun = units
	}
	return r
}

func NewGlobalPollerRegistration(p poller.Poller, priority poller.Priority, interval time.Duration) ChannelPollerRegistration {
	return NewChannelPollerRegistration(p, priority, interval).
		WithChannelIDs([]string{SyntheticGlobalPollerChannelID}).
		WithTargetGroup(ChannelTargetGroupGlobal)
}

func (r ChannelPollerRegistration) ToTargetSync() poller.PollerTargetSync {
	return poller.PollerTargetSync{
		Poller:     r.Poller,
		Priority:   r.Priority,
		Interval:   r.Interval,
		ChannelIDs: append([]string(nil), r.ChannelIDs...),
	}
}

type ScraperSchedulerOption func(*scraperSchedulerOptions)

type scraperSchedulerOptions struct {
	channelPollerRegistrations []ChannelPollerRegistration
	workerCount                int
	pollTimeout                time.Duration
	errorBackoffMin            time.Duration
	errorBackoffMax            time.Duration
	channelIDs                 []string
}

func WithChannelPollerRegistrations(registrations []ChannelPollerRegistration) ScraperSchedulerOption {
	copied := make([]ChannelPollerRegistration, len(registrations))
	copy(copied, registrations)

	return func(options *scraperSchedulerOptions) {
		options.channelPollerRegistrations = copied
	}
}

func WithSchedulerWorkerCount(workerCount int) ScraperSchedulerOption {
	return func(options *scraperSchedulerOptions) {
		options.workerCount = workerCount
	}
}

func WithSchedulerPollTimeout(timeout time.Duration) ScraperSchedulerOption {
	return func(options *scraperSchedulerOptions) {
		options.pollTimeout = timeout
	}
}

func WithSchedulerErrorBackoff(minBackoff, maxBackoff time.Duration) ScraperSchedulerOption {
	return func(options *scraperSchedulerOptions) {
		options.errorBackoffMin = minBackoff
		options.errorBackoffMax = maxBackoff
	}
}

func WithSchedulerChannelIDs(channelIDs []string) ScraperSchedulerOption {
	copied := append([]string(nil), channelIDs...)

	return func(options *scraperSchedulerOptions) {
		options.channelIDs = copied
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
