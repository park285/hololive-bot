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
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

func TestYouTubeStackNilSafeAccessors(t *testing.T) {
	t.Parallel()

	var stack *YouTubeStack
	if stack.GetService() != nil {
		t.Fatal("GetService() must return nil for nil receiver")
	}
	if stack.GetScheduler() != nil {
		t.Fatal("GetScheduler() must return nil for nil receiver")
	}
	if stack.GetStatsRepository() != nil {
		t.Fatal("GetStatsRepository() must return nil for nil receiver")
	}
}

func TestChannelPollerRegistrationWithBudgetProfileCopiesSourceUnits(t *testing.T) {
	t.Parallel()

	sourceUnits := map[poller.BudgetSource]float64{
		poller.BudgetSourceYouTubeScraper: 3,
		poller.BudgetSourcePostgresWrite:  1,
	}
	profile := poller.BudgetProfile{
		SourceUnits: sourceUnits,
		BurstClass:  poller.BudgetBurstPrimary,
		Priority:    poller.BudgetPriorityHigh,
	}

	registration := NewChannelPollerRegistration(nil, poller.PriorityHigh, time.Minute).
		WithBudgetProfile(profile)
	sourceUnits[poller.BudgetSourceYouTubeScraper] = 999
	sourceUnits[poller.BudgetSourceHolodexLive] = 2

	if !registration.HasBudgetProfile {
		t.Fatal("WithBudgetProfile must mark profile as explicit")
	}
	if registration.BudgetProfile.BurstClass != poller.BudgetBurstPrimary {
		t.Fatalf("unexpected burst class: %q", registration.BudgetProfile.BurstClass)
	}
	if registration.BudgetProfile.Priority != poller.BudgetPriorityHigh {
		t.Fatalf("unexpected priority: %q", registration.BudgetProfile.Priority)
	}
	if got := registration.BudgetProfile.SourceUnits[poller.BudgetSourceYouTubeScraper]; got != 3 {
		t.Fatalf("registration source units were not defensively copied: got %v", got)
	}
	if _, ok := registration.BudgetProfile.SourceUnits[poller.BudgetSourceHolodexLive]; ok {
		t.Fatal("registration source units must not observe mutations to the original map")
	}

	target := registration.ToTargetSync()
	if target.BudgetProfile.BurstClass != poller.BudgetBurstPrimary {
		t.Fatalf("target sync burst class was not propagated: %q", target.BudgetProfile.BurstClass)
	}
	if target.BudgetProfile.Priority != poller.BudgetPriorityHigh {
		t.Fatalf("target sync priority was not propagated: %q", target.BudgetProfile.Priority)
	}
	if got := target.BudgetProfile.SourceUnits[poller.BudgetSourcePostgresWrite]; got != 1 {
		t.Fatalf("target sync budget profile was not propagated: got %v", got)
	}
}

func TestChannelPollerRegistrationDefaultHasNoBudgetProfile(t *testing.T) {
	t.Parallel()

	registration := NewChannelPollerRegistration(nil, poller.PriorityNormal, time.Minute)
	if registration.HasBudgetProfile {
		t.Fatal("new registration must not have an explicit budget profile")
	}

	target := registration.ToTargetSync()
	if target.BudgetProfile.SourceUnits != nil {
		t.Fatal("target sync must not have source units when profile is not configured")
	}
}
