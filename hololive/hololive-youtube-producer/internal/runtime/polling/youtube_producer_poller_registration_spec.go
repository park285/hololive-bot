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
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type registrationSpec struct {
	Poller                poller.Poller
	Priority              poller.Priority
	Interval              time.Duration
	ChannelIDs            []string
	TargetGroup           providers.ChannelTargetGroup
	WorstCaseAttempts     int
	WorstCaseRequestUnits float64
	BudgetProfile         poller.BudgetProfile
}

func buildRegistration(spec registrationSpec) providers.ChannelPollerRegistration {
	return providers.NewChannelPollerRegistration(spec.Poller, spec.Priority, spec.Interval).
		WithChannelIDs(spec.ChannelIDs).
		WithTargetGroup(spec.TargetGroup).
		WithWorstCaseAttempts(spec.WorstCaseAttempts).
		WithWorstCaseRequestUnitsPerRun(spec.WorstCaseRequestUnits).
		WithBudgetProfile(spec.BudgetProfile)
}

func buildStatsRegistration(statsPoller poller.Poller, interval time.Duration, channelIDs []string) providers.ChannelPollerRegistration {
	return buildRegistration(registrationSpec{
		Poller:                statsPoller,
		Priority:              poller.PriorityLow,
		Interval:              interval,
		ChannelIDs:            channelIDs,
		TargetGroup:           providers.ChannelTargetGroupStats,
		WorstCaseAttempts:     scraper.FetchPageMaxAttempts,
		WorstCaseRequestUnits: channelStatsWorstCaseRequestUnits(),
		BudgetProfile:         youtubeScraperBudgetProfile(channelStatsWorstCaseRequestUnits(), poller.BudgetBurstPrimary, poller.BudgetPriorityLow),
	})
}

func videosWorstCaseRequestUnits() float64 {
	return float64(scraper.FetchPageMaxAttempts * 3)
}

func channelStatsWorstCaseRequestUnits() float64 {
	return float64(scraper.FetchPageMaxAttempts * 2)
}

func shortsWorstCaseRequestUnits(inlineResolveMissingPublishedAt bool, maxResults int) float64 {
	units := 1.0
	if inlineResolveMissingPublishedAt {
		units += float64(scraper.FetchPageMaxAttempts)
		units += float64(maxResults)
	}
	return units
}

func communityWorstCaseRequestUnits(inlineResolveMissingPublishedAt bool, maxResults int) float64 {
	units := 1.0
	if inlineResolveMissingPublishedAt {
		units += float64(maxResults)
	}
	return units
}
