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

package polltarget

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	pollscheduler "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
)

type refreshTestTargetSnapshotPoller struct {
	name    string
	targets []string
}

func (p *refreshTestTargetSnapshotPoller) Poll(context.Context, string) error {
	return nil
}

func (p *refreshTestTargetSnapshotPoller) Name() string {
	return p.name
}

func (p *refreshTestTargetSnapshotPoller) ChannelTargets() []string {
	return append([]string(nil), p.targets...)
}

func (p *refreshTestTargetSnapshotPoller) WithChannelTargets(channelIDs []string) (pollscheduler.Poller, polling.BudgetProfile) {
	updated := &refreshTestTargetSnapshotPoller{name: p.name, targets: append([]string(nil), channelIDs...)}
	return updated, polling.BudgetProfile{
		SourceUnits: map[polling.BudgetSource]float64{
			polling.BudgetSourcePostgresWrite: float64(len(channelIDs)),
		},
	}
}

func TestShouldSyncYouTubePollRegistration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		registration providers.ChannelPollerRegistration
		want         bool
	}{
		{
			name: "syncs explicit registration with poller and interval",
			registration: providers.NewChannelPollerRegistration(refreshTestPoller{name: "videos"}, pollscheduler.PriorityNormal, time.Minute).
				WithChannelIDs([]string{"UC_NOTIFY"}).
				WithTargetGroup(providers.ChannelTargetGroupNotification),
			want: true,
		},
		{
			name: "skips nil poller",
			registration: providers.NewChannelPollerRegistration(nil, pollscheduler.PriorityNormal, time.Minute).
				WithChannelIDs([]string{"UC_NOTIFY"}).
				WithTargetGroup(providers.ChannelTargetGroupNotification),
		},
		{
			name: "skips zero interval",
			registration: providers.NewChannelPollerRegistration(refreshTestPoller{name: "videos"}, pollscheduler.PriorityNormal, 0).
				WithChannelIDs([]string{"UC_NOTIFY"}).
				WithTargetGroup(providers.ChannelTargetGroupNotification),
		},
		{
			name: "skips implicit channel registration",
			registration: providers.NewChannelPollerRegistration(refreshTestPoller{name: "videos"}, pollscheduler.PriorityNormal, time.Minute).
				WithTargetGroup(providers.ChannelTargetGroupNotification),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := shouldSyncYouTubePollRegistration(&tt.registration)

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestYouTubePollRegistrationChannelIDs(t *testing.T) {
	t.Parallel()

	targets := Targets{
		NotificationChannelIDs: []string{"UC_NOTIFY"},
		OperationalChannelIDs:  []string{"UC_OPERATIONAL"},
	}
	tieredTargets := TieredTargets{
		ActiveNotificationChannelIDs: []string{"UC_ACTIVE"},
		WarmNotificationChannelIDs:   []string{"UC_WARM"},
		ColdNotificationChannelIDs:   []string{"UC_COLD"},
	}

	tests := []struct {
		name             string
		group            providers.ChannelTargetGroup
		hasTieredTargets bool
		want             []string
	}{
		{
			name:  "notification uses resolved notification targets",
			group: providers.ChannelTargetGroupNotification,
			want:  []string{"UC_NOTIFY"},
		},
		{
			name:  "operational uses resolved operational targets",
			group: providers.ChannelTargetGroupOperational,
			want:  []string{"UC_OPERATIONAL"},
		},
		{
			name:  "global keeps registration targets",
			group: providers.ChannelTargetGroupGlobal,
			want:  []string{"UC_REGISTRATION"},
		},
		{
			name:             "active tier uses active targets",
			group:            providers.ChannelTargetGroupActive,
			hasTieredTargets: true,
			want:             []string{"UC_ACTIVE"},
		},
		{
			name:             "warm tier uses warm targets",
			group:            providers.ChannelTargetGroupWarm,
			hasTieredTargets: true,
			want:             []string{"UC_WARM"},
		},
		{
			name:             "cold tier uses cold targets",
			group:            providers.ChannelTargetGroupCold,
			hasTieredTargets: true,
			want:             []string{"UC_COLD"},
		},
		{
			name:  "tiered group falls back to registration targets without tiered result",
			group: providers.ChannelTargetGroupActive,
			want:  []string{"UC_REGISTRATION"},
		},
		{
			name:  "default group keeps registration targets",
			group: providers.ChannelTargetGroupDefault,
			want:  []string{"UC_REGISTRATION"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registration := providers.NewChannelPollerRegistration(refreshTestPoller{name: "videos"}, pollscheduler.PriorityNormal, time.Minute).
				WithChannelIDs([]string{"UC_REGISTRATION"}).
				WithTargetGroup(tt.group)

			got := youtubePollRegistrationChannelIDs(&registration, targets, &tieredTargets, tt.hasTieredTargets)

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestYouTubePollRegistrationTargetSync(t *testing.T) {
	t.Parallel()

	targets := Targets{
		NotificationChannelIDs: []string{"UC_NOTIFY"},
		OperationalChannelIDs:  []string{"UC_OPERATIONAL"},
	}
	tieredTargets := TieredTargets{
		ActiveNotificationChannelIDs: []string{"UC_ACTIVE"},
		WarmNotificationChannelIDs:   []string{"UC_WARM"},
		ColdNotificationChannelIDs:   []string{"UC_COLD"},
	}

	tests := []struct {
		name           string
		group          providers.ChannelTargetGroup
		interval       time.Duration
		hasTiered      bool
		wantChannelIDs []string
		wantImmediate  bool
	}{
		{
			name:           "notification targets run immediately on first sync",
			group:          providers.ChannelTargetGroupNotification,
			interval:       time.Minute,
			wantChannelIDs: []string{"UC_NOTIFY"},
			wantImmediate:  true,
		},
		{
			name:           "warm tier keeps registration interval and runs immediately",
			group:          providers.ChannelTargetGroupWarm,
			interval:       2 * time.Minute,
			hasTiered:      true,
			wantChannelIDs: []string{"UC_WARM"},
			wantImmediate:  true,
		},
		{
			name:           "cold tier keeps longer registration interval",
			group:          providers.ChannelTargetGroupCold,
			interval:       6 * time.Minute,
			hasTiered:      true,
			wantChannelIDs: []string{"UC_COLD"},
			wantImmediate:  true,
		},
		{
			name:           "operational targets do not force immediate first run",
			group:          providers.ChannelTargetGroupOperational,
			interval:       4 * time.Hour,
			wantChannelIDs: []string{"UC_OPERATIONAL"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registration := providers.NewChannelPollerRegistration(refreshTestPoller{name: "videos"}, pollscheduler.PriorityNormal, tt.interval).
				WithChannelIDs([]string{"UC_REGISTRATION"}).
				WithTargetGroup(tt.group)

			got := youtubePollRegistrationTargetSync(&registration, targets, &tieredTargets, tt.hasTiered)

			assert.Equal(t, registration.Poller, got.Poller)
			assert.Equal(t, registration.Priority, got.Priority)
			assert.Equal(t, tt.interval, got.Interval)
			assert.Equal(t, tt.wantChannelIDs, got.ChannelIDs)
			assert.Equal(t, tt.wantImmediate, got.ForceImmediateFirstRun)
		})
	}
}

func TestYouTubePollRegistrationTargetSyncReplacesSnapshotAndBudgetAtomically(t *testing.T) {
	t.Parallel()

	poller := &refreshTestTargetSnapshotPoller{name: "live_batch", targets: []string{"UC_OLD"}}
	registration := providers.NewChannelPollerRegistration(poller, pollscheduler.PriorityHigh, time.Minute).
		WithChannelIDs([]string{providers.SyntheticGlobalPollerChannelID}).
		WithTargetGroup(providers.ChannelTargetGroupOperational)

	sync := youtubePollRegistrationTargetSync(&registration, Targets{
		OperationalChannelIDs: []string{"UC_NEW_A", "UC_NEW_B"},
	}, nil, false)
	updated := sync.Poller.(*refreshTestTargetSnapshotPoller)

	assert.Equal(t, []string{providers.SyntheticGlobalPollerChannelID}, sync.ChannelIDs)
	assert.Equal(t, []string{"UC_OLD"}, poller.ChannelTargets())
	assert.Equal(t, []string{"UC_NEW_A", "UC_NEW_B"}, updated.ChannelTargets())
	assert.Equal(t, 2.0, sync.BudgetProfile.SourceUnits[polling.BudgetSourcePostgresWrite])

	resolved := resolveYouTubePollTargetsFromRegistrations([]providers.ChannelPollerRegistration{registration})
	assert.Equal(t, []string{"UC_OLD"}, resolved.OperationalChannelIDs)
}

func TestYouTubePollSchedulerSyncerSyncAtHandlesNilDependencies(t *testing.T) {
	t.Parallel()

	targets := Targets{NotificationChannelIDs: []string{"UC_NOTIFY"}}
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)

	assert.NotPanics(t, func() {
		var syncer *SchedulerSyncer
		syncer.SyncAt(context.Background(), targets, now)
	})
	assert.NotPanics(t, func() {
		(&SchedulerSyncer{}).SyncAt(context.Background(), targets, now)
	})
}

func TestYouTubePollSchedulerSyncerSyncAtClearsEmptyTargets(t *testing.T) {
	t.Parallel()

	registration := providers.NewChannelPollerRegistration(refreshTestPoller{name: "videos"}, pollscheduler.PriorityNormal, time.Minute).
		WithChannelIDs([]string{"UC_OLD"}).
		WithTargetGroup(providers.ChannelTargetGroupNotification)
	registrations := []providers.ChannelPollerRegistration{registration}
	scheduler := providers.ProvideScraperScheduler(
		nil,
		newYouTubePollTargetTestLogger(),
		providers.WithChannelPollerRegistrations(registrations),
	)
	require.Contains(t, schedulerJobKeys(t, scheduler), "UC_OLD:videos")

	syncer := &SchedulerSyncer{
		scheduler:     scheduler,
		registrations: registrations,
	}

	syncer.SyncAt(context.Background(), Targets{}, time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC))

	require.NotContains(t, schedulerJobKeys(t, scheduler), "UC_OLD:videos")
}

func TestYouTubePollSchedulerSyncerSyncAtSkipsTieredClassifyOnCancelledContext(t *testing.T) {
	t.Parallel()

	registration := providers.NewChannelPollerRegistration(refreshTestPoller{name: "videos"}, pollscheduler.PriorityNormal, time.Minute).
		WithChannelIDs([]string{"UC_ACTIVE_REG"}).
		WithTargetGroup(providers.ChannelTargetGroupActive)
	registrations := []providers.ChannelPollerRegistration{registration}
	targets := Targets{NotificationChannelIDs: []string{"UC_NOTIFY"}}
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)

	liveScheduler := providers.ProvideScraperScheduler(
		nil,
		newYouTubePollTargetTestLogger(),
		providers.WithChannelPollerRegistrations(registrations),
	)
	liveSyncer := &SchedulerSyncer{scheduler: liveScheduler, registrations: registrations}
	liveSyncer.SyncAt(context.Background(), targets, now)
	require.Contains(t, schedulerJobKeys(t, liveScheduler), "UC_NOTIFY:videos",
		"non-cancelled context must classify tiers and sync resolved notification targets")

	cancelledScheduler := providers.ProvideScraperScheduler(
		nil,
		newYouTubePollTargetTestLogger(),
		providers.WithChannelPollerRegistrations(registrations),
	)
	cancelledSyncer := &SchedulerSyncer{scheduler: cancelledScheduler, registrations: registrations}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelledSyncer.SyncAt(ctx, targets, now)

	keys := schedulerJobKeys(t, cancelledScheduler)
	require.Contains(t, keys, "UC_ACTIVE_REG:videos",
		"cancelled context must skip the tiered sync and keep the scheduler's existing jobs")
	require.NotContains(t, keys, "UC_NOTIFY:videos",
		"cancelled context must not run tiered classify against resolved notification targets")
}
