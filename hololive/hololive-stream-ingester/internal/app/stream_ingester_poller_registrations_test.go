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
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/providers"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type fakeTestPoller struct {
	name string
}

func (p fakeTestPoller) Poll(context.Context, string) error { return nil }
func (p fakeTestPoller) Name() string                       { return p.name }

func newPollerRegistrationTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

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
		[]string{"UC_NOTIFY_A", "UC_NOTIFY_B"},
		[]string{"UC_STATS_A"},
	)

	if len(registrations) != 5 {
		t.Fatalf("len(registrations) = %d, want 5", len(registrations))
	}

	expected := []struct {
		name     string
		priority poller.Priority
		interval time.Duration
		group    providers.ChannelTargetGroup
	}{
		{name: "videos", priority: poller.PriorityNormal, interval: 7 * time.Minute, group: providers.ChannelTargetGroupNotification},
		{name: "shorts", priority: poller.PriorityLow, interval: 11 * time.Minute, group: providers.ChannelTargetGroupNotification},
		{name: "community", priority: poller.PriorityLow, interval: 13 * time.Minute, group: providers.ChannelTargetGroupNotification},
		{name: "channel_stats", priority: poller.PriorityLow, interval: 4 * time.Hour, group: providers.ChannelTargetGroupStats},
		{name: "live", priority: poller.PriorityHigh, interval: 3 * time.Minute, group: providers.ChannelTargetGroupNotification},
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
		if reg.TargetGroup != expected[idx].group {
			t.Fatalf("registrations[%d].TargetGroup = %q, want %q", idx, reg.TargetGroup, expected[idx].group)
		}
		switch reg.Poller.Name() {
		case "channel_stats":
			if len(reg.ChannelIDs) != 1 || reg.ChannelIDs[0] != "UC_STATS_A" {
				t.Fatalf("registrations[%d].ChannelIDs = %#v, want [UC_STATS_A]", idx, reg.ChannelIDs)
			}
		default:
			if len(reg.ChannelIDs) != 2 || reg.ChannelIDs[0] != "UC_NOTIFY_A" || reg.ChannelIDs[1] != "UC_NOTIFY_B" {
				t.Fatalf("registrations[%d].ChannelIDs = %#v, want [UC_NOTIFY_A UC_NOTIFY_B]", idx, reg.ChannelIDs)
			}
		}
	}
}

func TestBuildStreamIngesterChannelPollerRegistrations_AllExplicit(t *testing.T) {
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
		[]string{"UC_NOTIFY_A", "UC_NOTIFY_B"},
		[]string{"UC_STATS_A"},
	)

	for idx, reg := range registrations {
		if reg.Poller == nil || reg.Interval <= 0 {
			continue
		}
		if !reg.HasExplicitChannelIDs {
			t.Fatalf("registrations[%d] (%s) missing explicit channel IDs", idx, reg.Poller.Name())
		}
	}
}

func TestValidateExplicitPollerRegistrations_ReturnsErrorOnActiveNonExplicitRegistration(t *testing.T) {
	t.Parallel()

	err := validateExplicitPollerRegistrations([]providers.ChannelPollerRegistration{
		providers.NewChannelPollerRegistration(
			fakeTestPoller{name: "videos"},
			poller.PriorityNormal,
			time.Minute,
		),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "videos")
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

	scheduler, dispatcher, registrations, err := buildStreamIngesterYouTubeComponents(
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
		communityShortsEnabledChannelIDs(operationalChannels),
		buildSharedYouTubeScraperClient(config.ScraperConfig{}, nil, scraper.NewRateLimiter(time.Second)),
		nil,
		nil,
		nil,
		nil,
		newPollerRegistrationTestLogger(),
	)
	require.NoError(t, err)

	if scheduler == nil {
		t.Fatal("scheduler is nil")
	}
	if dispatcher == nil {
		t.Fatal("dispatcher is nil")
	}
	if len(registrations) != 5 {
		t.Fatalf("len(registrations) = %d, want 5", len(registrations))
	}

	applied := scheduler.SetProxyEnabled(false)
	if applied != 5 {
		t.Fatalf("scheduler.SetProxyEnabled(false) = %d, want 5", applied)
	}
}
