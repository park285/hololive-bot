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

package polling

import (
	"time"

	providers "github.com/kapu/hololive-shared/pkg/providers"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/pollers"
)

type registrationSpec struct {
	Poller                scheduler.Poller
	Priority              scheduler.Priority
	Interval              time.Duration
	ChannelIDs            []string
	TargetGroup           providers.ChannelTargetGroup
	WorstCaseAttempts     int
	WorstCaseRequestUnits float64
	BudgetProfile         polling.BudgetProfile
}

func buildRegistration(spec *registrationSpec) providers.ChannelPollerRegistration {
	return providers.NewChannelPollerRegistration(spec.Poller, spec.Priority, spec.Interval).
		WithChannelIDs(spec.ChannelIDs).
		WithTargetGroup(spec.TargetGroup).
		WithWorstCaseAttempts(spec.WorstCaseAttempts).
		WithWorstCaseRequestUnitsPerRun(spec.WorstCaseRequestUnits).
		WithBudgetProfile(spec.BudgetProfile)
}

func buildStatsRegistration(statsPoller scheduler.Poller, interval time.Duration, channelIDs []string) providers.ChannelPollerRegistration {
	return buildRegistration(&registrationSpec{
		Poller:                statsPoller,
		Priority:              scheduler.PriorityLow,
		Interval:              interval,
		ChannelIDs:            channelIDs,
		TargetGroup:           providers.ChannelTargetGroupOperational,
		WorstCaseAttempts:     scraper.FetchPageMaxAttempts,
		WorstCaseRequestUnits: channelStatsWorstCaseRequestUnits(),
		BudgetProfile:         youtubeScraperBudgetProfile(channelStatsWorstCaseRequestUnits(), polling.BudgetBurstPrimary, polling.BudgetPriorityLow),
	})
}

func videosWorstCaseRequestUnits() float64 {
	return pollers.VideosWorstCaseRequestUnits()
}

func channelStatsWorstCaseRequestUnits() float64 {
	return float64(scraper.FetchPageMaxAttempts * 2)
}

func shortsWorstCaseRequestUnits() float64 {
	return pollers.ShortsWorstCaseRequestUnits()
}
