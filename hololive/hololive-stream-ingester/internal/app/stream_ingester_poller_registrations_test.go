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

package app

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"gorm.io/gorm"
)

func TestBuildStreamIngesterChannelPollerRegistrations_DefaultOrdering(t *testing.T) {
	t.Parallel()

	postgres := &databasemocks.Client{
		GetGormDBFunc: func() *gorm.DB {
			return nil
		},
	}

	registrations := buildStreamIngesterChannelPollerRegistrations(
		postgres,
		config.ScraperConfig{
			Poll: config.ScraperPoll{
				Videos:    7 * time.Minute,
				Shorts:    11 * time.Minute,
				Community: 13 * time.Minute,
				Stats:     4 * time.Hour,
				Live:      3 * time.Minute,
			},
		},
		scraper.NewRateLimiter(time.Second),
		nil,
		nil,
	)

	if len(registrations) != 5 {
		t.Fatalf("len(registrations) = %d, want 5", len(registrations))
	}

	expected := []struct {
		name     string
		priority poller.Priority
		interval time.Duration
	}{
		{name: "videos", priority: poller.PriorityNormal, interval: 7 * time.Minute},
		{name: "shorts", priority: poller.PriorityLow, interval: 11 * time.Minute},
		{name: "community", priority: poller.PriorityLow, interval: 13 * time.Minute},
		{name: "channel_stats", priority: poller.PriorityLow, interval: 4 * time.Hour},
		{name: "live", priority: poller.PriorityHigh, interval: 3 * time.Minute},
	}

	for idx, reg := range registrations {
		if reg.Poller == nil {
			t.Fatalf("registrations[%d].Poller is nil", idx)
		}
		if reg.Poller.Name() != expected[idx].name {
			t.Fatalf("registrations[%d].Poller.Name() = %q, want %q", idx, reg.Poller.Name(), expected[idx].name)
		}
		if reg.Priority != expected[idx].priority {
			t.Fatalf("registrations[%d].Priority = %d, want %d", idx, reg.Priority, expected[idx].priority)
		}
		if reg.Interval != expected[idx].interval {
			t.Fatalf("registrations[%d].Interval = %s, want %s", idx, reg.Interval, expected[idx].interval)
		}
	}
}

func TestBuildStreamIngesterYouTubeComponents_GraduatedMembersFiltered(t *testing.T) {
	t.Parallel()

	postgres := &databasemocks.Client{
		GetGormDBFunc: func() *gorm.DB {
			return nil
		},
	}

	operationalChannels := mustResolveCommunityShortsOperationalChannels(t, &fakeMemberDataProvider{
		members: []*domain.Member{
			{ChannelID: " UCACTIVE "},
			{ChannelID: " ", Name: "missing"},
			{ChannelID: "UCGRADUATED", IsGraduated: true},
		},
	})

	scheduler, dispatcher := buildStreamIngesterYouTubeComponents(
		config.ScraperConfig{
			Poll: config.ScraperPoll{
				Videos:    5 * time.Minute,
				Shorts:    10 * time.Minute,
				Community: 10 * time.Minute,
				Stats:     6 * time.Hour,
				Live:      5 * time.Minute,
			},
		},
		postgres,
		communityShortsEnabledChannelIDs(operationalChannels),
		nil,
		nil,
		nil,
		scraper.NewRateLimiter(time.Second),
		nil,
		testLogger(),
	)

	if scheduler == nil {
		t.Fatal("scheduler is nil")
	}
	if dispatcher == nil {
		t.Fatal("dispatcher is nil")
	}

	applied := scheduler.SetProxyEnabled(false)
	if applied != 5 {
		t.Fatalf("scheduler.SetProxyEnabled(false) = %d, want 5", applied)
	}
}
