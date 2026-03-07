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
	providers "github.com/kapu/hololive-shared/pkg/providers"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	membermocks "github.com/kapu/hololive-shared/pkg/service/member/mocks"
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
		scraper.ProxyConfig{},
		scraper.NewRateLimiter(time.Second),
		nil,
	)

	if len(registrations) != 5 {
		t.Fatalf("len(registrations) = %d, want 5", len(registrations))
	}

	intervals := providers.DefaultPollerIntervals()
	expected := []struct {
		name     string
		priority poller.Priority
		interval time.Duration
	}{
		{name: "videos", priority: poller.PriorityNormal, interval: intervals.Videos},
		{name: "shorts", priority: poller.PriorityLow, interval: intervals.Shorts},
		{name: "community", priority: poller.PriorityLow, interval: intervals.Community},
		{name: "channel_stats", priority: poller.PriorityLow, interval: intervals.Stats},
		{name: "live", priority: poller.PriorityHigh, interval: intervals.Live},
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

	membersData := &membermocks.DataProvider{
		GetAllMembersFunc: func() []*domain.Member {
			return []*domain.Member{
				{ChannelID: "UCACTIVE"},
				{ChannelID: "UCGRADUATED", IsGraduated: true},
			}
		},
	}

	scheduler, dispatcher := buildStreamIngesterYouTubeComponents(
		config.ScraperConfig{},
		postgres,
		membersData,
		nil,
		nil,
		nil,
		scraper.NewRateLimiter(time.Second),
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
