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
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	"maps"
	"time"
)

type ChannelPollerRegistration struct {
	Poller   scheduler.Poller
	Priority scheduler.Priority
	Interval time.Duration
	*ChannelPollerRegistrationOptions
}

type ChannelPollerRegistrationOptions struct {
	ChannelIDs                  []string
	HasExplicitChannelIDs       bool
	TargetGroup                 ChannelTargetGroup
	RequestsPerRun              int
	WorstCaseAttempts           int
	WorstCaseRequestUnitsPerRun float64
	BudgetProfile               polling.BudgetProfile
	HasBudgetProfile            bool
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

func NewChannelPollerRegistration(p scheduler.Poller, priority scheduler.Priority, interval time.Duration) ChannelPollerRegistration {
	return ChannelPollerRegistration{
		Poller:   p,
		Priority: priority,
		Interval: interval,
		ChannelPollerRegistrationOptions: &ChannelPollerRegistrationOptions{
			TargetGroup:    ChannelTargetGroupDefault,
			RequestsPerRun: 1,
		},
	}
}

func (r ChannelPollerRegistration) WithChannelIDs(channelIDs []string) ChannelPollerRegistration {
	options := r.cloneOptions()
	options.ChannelIDs = append([]string(nil), channelIDs...)
	options.HasExplicitChannelIDs = true
	return r
}

func (r ChannelPollerRegistration) WithTargetGroup(group ChannelTargetGroup) ChannelPollerRegistration {
	r.cloneOptions().TargetGroup = group
	return r
}

func (r ChannelPollerRegistration) WithRequestsPerRun(requestsPerRun int) ChannelPollerRegistration {
	if requestsPerRun > 0 {
		r.cloneOptions().RequestsPerRun = requestsPerRun
	}
	return r
}

func (r ChannelPollerRegistration) WithWorstCaseAttempts(attempts int) ChannelPollerRegistration {
	if attempts > 0 {
		r.cloneOptions().WorstCaseAttempts = attempts
	}
	return r
}

func (r ChannelPollerRegistration) WithWorstCaseRequestUnitsPerRun(units float64) ChannelPollerRegistration {
	if units > 0 {
		r.cloneOptions().WorstCaseRequestUnitsPerRun = units
	}
	return r
}

func (r ChannelPollerRegistration) WithBudgetProfile(profile polling.BudgetProfile) ChannelPollerRegistration {
	if profile.SourceUnits != nil {
		sourceUnits := make(map[polling.BudgetSource]float64, len(profile.SourceUnits))
		maps.Copy(sourceUnits, profile.SourceUnits)
		profile.SourceUnits = sourceUnits
	}
	if profile.FallbackSourceUnits != nil {
		fallbackSourceUnits := make(map[polling.BudgetSource]float64, len(profile.FallbackSourceUnits))
		maps.Copy(fallbackSourceUnits, profile.FallbackSourceUnits)
		profile.FallbackSourceUnits = fallbackSourceUnits
	}
	options := r.cloneOptions()
	options.BudgetProfile = profile
	options.HasBudgetProfile = true
	return r
}

func NewGlobalPollerRegistration(p scheduler.Poller, priority scheduler.Priority, interval time.Duration) ChannelPollerRegistration {
	return NewChannelPollerRegistration(p, priority, interval).
		WithChannelIDs([]string{SyntheticGlobalPollerChannelID}).
		WithTargetGroup(ChannelTargetGroupGlobal)
}

func (r ChannelPollerRegistration) ToTargetSync() scheduler.PollerTargetSync {
	options := r.optionsOrDefault()
	return scheduler.PollerTargetSync{
		Poller:        r.Poller,
		Priority:      r.Priority,
		Interval:      r.Interval,
		ChannelIDs:    append([]string(nil), options.ChannelIDs...),
		BudgetProfile: options.BudgetProfile,
	}
}

func (r *ChannelPollerRegistration) ensureOptions() *ChannelPollerRegistrationOptions {
	if r.ChannelPollerRegistrationOptions == nil {
		r.ChannelPollerRegistrationOptions = defaultChannelPollerRegistrationOptions()
	}
	return r.ChannelPollerRegistrationOptions
}

func (r *ChannelPollerRegistration) cloneOptions() *ChannelPollerRegistrationOptions {
	if r.ChannelPollerRegistrationOptions == nil {
		r.ChannelPollerRegistrationOptions = defaultChannelPollerRegistrationOptions()
		return r.ChannelPollerRegistrationOptions
	}
	options := *r.ChannelPollerRegistrationOptions
	options.ChannelIDs = append([]string(nil), options.ChannelIDs...)
	r.ChannelPollerRegistrationOptions = &options
	return r.ChannelPollerRegistrationOptions
}

func (r *ChannelPollerRegistration) optionsOrDefault() *ChannelPollerRegistrationOptions {
	if r == nil || r.ChannelPollerRegistrationOptions == nil {
		return defaultChannelPollerRegistrationOptions()
	}
	return r.ChannelPollerRegistrationOptions
}

func defaultChannelPollerRegistrationOptions() *ChannelPollerRegistrationOptions {
	return &ChannelPollerRegistrationOptions{
		TargetGroup:    ChannelTargetGroupDefault,
		RequestsPerRun: 1,
	}
}

type ScraperSchedulerOption func(*scraperSchedulerOptions)

type scraperSchedulerOptions struct {
	channelPollerRegistrations []ChannelPollerRegistration
	workerCount                int
	pollTimeout                time.Duration
	errorBackoffMin            time.Duration
	errorBackoffMax            time.Duration
	jobClaimer                 polling.JobClaimer
	budgetLimiter              polling.GlobalBudgetLimiter
	budgetContext              polling.BudgetContext
	budgetAcquireTimeout       time.Duration
	channelIDs                 []string
}

func WithChannelPollerRegistrations(registrations []ChannelPollerRegistration) ScraperSchedulerOption {
	copied := make([]ChannelPollerRegistration, len(registrations))
	copy(copied, registrations)
	for i := range copied {
		copied[i].ensureOptions()
	}

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

func WithSchedulerJobClaimer(claimer polling.JobClaimer) ScraperSchedulerOption {
	return func(options *scraperSchedulerOptions) {
		options.jobClaimer = claimer
	}
}

func WithSchedulerBudgetLimiter(limiter polling.GlobalBudgetLimiter) ScraperSchedulerOption {
	return func(options *scraperSchedulerOptions) {
		options.budgetLimiter = limiter
	}
}

func WithSchedulerBudgetContext(budgetContext polling.BudgetContext) ScraperSchedulerOption {
	return func(options *scraperSchedulerOptions) {
		options.budgetContext = budgetContext
	}
}

func WithSchedulerBudgetAcquireTimeout(timeout time.Duration) ScraperSchedulerOption {
	return func(options *scraperSchedulerOptions) {
		options.budgetAcquireTimeout = timeout
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
