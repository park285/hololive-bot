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

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
)

func TestYouTubeStackNilSafeAccessors(t *testing.T) {
	t.Parallel()

	var stack *YouTubeStack

	if stack.GetService() != nil {
		t.Fatal("GetService() must return nil for nil receiver")
	}
}

func TestChannelPollerRegistrationWithBudgetProfileCopiesSourceUnits(t *testing.T) {
	t.Parallel()

	sourceUnits := map[polling.BudgetSource]float64{
		polling.BudgetSourceYouTubeScraper: 3,
		polling.BudgetSourcePostgresWrite:  1,
	}
	fallbackSourceUnits := map[polling.BudgetSource]float64{
		polling.BudgetSourceYouTubeScraper: 12,
	}
	profile := polling.BudgetProfile{
		SourceUnits:         sourceUnits,
		FallbackSourceUnits: fallbackSourceUnits,
		BurstClass:          polling.BudgetBurstPrimary,
		Priority:            polling.BudgetPriorityHigh,
	}

	registration := NewChannelPollerRegistration(nil, scheduler.PriorityHigh, time.Minute).
		WithBudgetProfile(profile)

	sourceUnits[polling.BudgetSourceYouTubeScraper] = 999
	sourceUnits[polling.BudgetSourceHolodexLive] = 2
	fallbackSourceUnits[polling.BudgetSourceYouTubeScraper] = 999
	fallbackSourceUnits[polling.BudgetSourceHolodexLive] = 2

	if !registration.HasBudgetProfile {
		t.Fatal("WithBudgetProfile must mark profile as explicit")
	}

	if registration.BudgetProfile.BurstClass != polling.BudgetBurstPrimary {
		t.Fatalf("unexpected burst class: %q", registration.BudgetProfile.BurstClass)
	}

	if registration.BudgetProfile.Priority != polling.BudgetPriorityHigh {
		t.Fatalf("unexpected priority: %q", registration.BudgetProfile.Priority)
	}

	if got := registration.BudgetProfile.SourceUnits[polling.BudgetSourceYouTubeScraper]; got != 3 {
		t.Fatalf("registration source units were not defensively copied: got %v", got)
	}

	if _, ok := registration.BudgetProfile.SourceUnits[polling.BudgetSourceHolodexLive]; ok {
		t.Fatal("registration source units must not observe mutations to the original map")
	}

	if got := registration.BudgetProfile.FallbackSourceUnits[polling.BudgetSourceYouTubeScraper]; got != 12 {
		t.Fatalf("registration fallback source units were not defensively copied: got %v", got)
	}

	if _, ok := registration.BudgetProfile.FallbackSourceUnits[polling.BudgetSourceHolodexLive]; ok {
		t.Fatal("registration fallback source units must not observe mutations to the original map")
	}

	target := registration.ToTargetSync()
	if target.BudgetProfile.BurstClass != polling.BudgetBurstPrimary {
		t.Fatalf("target sync burst class was not propagated: %q", target.BudgetProfile.BurstClass)
	}

	if target.BudgetProfile.Priority != polling.BudgetPriorityHigh {
		t.Fatalf("target sync priority was not propagated: %q", target.BudgetProfile.Priority)
	}

	if got := target.BudgetProfile.SourceUnits[polling.BudgetSourcePostgresWrite]; got != 1 {
		t.Fatalf("target sync budget profile was not propagated: got %v", got)
	}

	if got := target.BudgetProfile.FallbackSourceUnits[polling.BudgetSourceYouTubeScraper]; got != 12 {
		t.Fatalf("target sync fallback budget profile was not propagated: got %v", got)
	}
}

func TestChannelPollerRegistrationDefaultHasNoBudgetProfile(t *testing.T) {
	t.Parallel()

	registration := NewChannelPollerRegistration(nil, scheduler.PriorityNormal, time.Minute)
	if registration.HasBudgetProfile {
		t.Fatal("new registration must not have an explicit budget profile")
	}

	target := registration.ToTargetSync()
	if target.BudgetProfile.SourceUnits != nil {
		t.Fatal("target sync must not have source units when profile is not configured")
	}
}
