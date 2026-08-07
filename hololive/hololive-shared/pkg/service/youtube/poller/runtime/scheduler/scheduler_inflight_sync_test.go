package scheduler

import (
	"container/heap"
	"context"
	"sync/atomic"
	"testing"
	"time"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchedulerSyncPollerTargetsDefersInflightJobUpdateUntilReschedule(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	p := &togglePollerStub{name: "videos"}
	scheduler.Register("channel-live", p, PriorityNormal, time.Minute)

	job := scheduler.jobMap["channel-live:videos"]
	require.NotNil(t, job)
	heap.Pop(&scheduler.jobs)
	require.Equal(t, -1, job.index)

	scheduler.SyncPollerTargets(&PollerTargetSync{
		Poller:     p,
		Priority:   PriorityHigh,
		Interval:   2 * time.Minute,
		ChannelIDs: []string{"channel-live"},
	})

	assert.Equal(t, PriorityNormal, job.Priority)
	assert.Equal(t, time.Minute, job.Interval)
	require.NotNil(t, job.pendingSync)

	scheduler.rescheduleJob(job)

	assert.Equal(t, PriorityHigh, job.Priority)
	assert.Equal(t, 2*time.Minute, job.Interval)
	assert.Nil(t, job.pendingSync)
	require.GreaterOrEqual(t, job.index, 0)
}

func TestSchedulerSyncPollerTargetsStillUpdatesQueuedJobImmediately(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	p := &togglePollerStub{name: "videos"}
	scheduler.Register("channel-live", p, PriorityNormal, time.Minute)

	job := scheduler.jobMap["channel-live:videos"]
	require.NotNil(t, job)
	require.GreaterOrEqual(t, job.index, 0)

	scheduler.SyncPollerTargets(&PollerTargetSync{
		Poller:     p,
		Priority:   PriorityHigh,
		Interval:   2 * time.Minute,
		ChannelIDs: []string{"channel-live"},
	})

	assert.Equal(t, PriorityHigh, job.Priority)
	assert.Equal(t, 2*time.Minute, job.Interval)
	assert.Nil(t, job.pendingSync)
}

func TestSchedulerClaimSkipRescheduleAppliesPendingSync(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	p := &togglePollerStub{name: "videos"}
	scheduler.Register("channel-live", p, PriorityNormal, time.Minute)

	job := scheduler.jobMap["channel-live:videos"]
	require.NotNil(t, job)
	heap.Pop(&scheduler.jobs)
	require.Equal(t, -1, job.index)

	scheduler.SyncPollerTargets(&PollerTargetSync{
		Poller:     p,
		Priority:   PriorityHigh,
		Interval:   2 * time.Minute,
		ChannelIDs: []string{"channel-live"},
	})
	require.NotNil(t, job.pendingSync)

	scheduler.rescheduleJobAfterClaimSkip(job, time.Second)

	assert.Equal(t, PriorityHigh, job.Priority)
	assert.Equal(t, 2*time.Minute, job.Interval)
	assert.Nil(t, job.pendingSync)
}

func TestSchedulerBudgetSkipRescheduleAppliesPendingSync(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	p := &togglePollerStub{name: "videos"}
	scheduler.Register("channel-live", p, PriorityNormal, time.Minute)

	job := scheduler.jobMap["channel-live:videos"]
	require.NotNil(t, job)
	heap.Pop(&scheduler.jobs)
	require.Equal(t, -1, job.index)

	scheduler.SyncPollerTargets(&PollerTargetSync{
		Poller:     p,
		Priority:   PriorityHigh,
		Interval:   2 * time.Minute,
		ChannelIDs: []string{"channel-live"},
	})
	require.NotNil(t, job.pendingSync)

	scheduler.rescheduleJobAfterBudgetSkip(job, time.Second)

	assert.Equal(t, PriorityHigh, job.Priority)
	assert.Equal(t, 2*time.Minute, job.Interval)
	assert.Nil(t, job.pendingSync)
}

type alwaysFailPoller struct {
	name  string
	calls atomic.Int32
}

func (p *alwaysFailPoller) Poll(context.Context, string) error {
	p.calls.Add(1)
	return context.DeadlineExceeded
}

func (p *alwaysFailPoller) Name() string { return p.name }

type stubBudgetReservation struct{}

func (stubBudgetReservation) Commit(context.Context) error  { return nil }
func (stubBudgetReservation) Release(context.Context) error { return nil }

type stubBudgetLimiter struct{}

func (stubBudgetLimiter) TryReserve(context.Context, *polling.BudgetJob, polling.BudgetProfile, time.Duration) (polling.BudgetReservation, polling.BudgetDecision, error) {
	return stubBudgetReservation{}, polling.BudgetDecision{Allowed: true}, nil
}

// poll 실패 시 커밋이 생략되어 예약 해제 defer가 reschedule 이후에 실행된다 — 그 꼬리
// 구간에서 동시 sync의 budgetProfile 쓰기와 경합하지 않음을 race detector로 고정한다.
func TestSchedulerWorkerTailDoesNotRaceWithSyncAfterPollFailure(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		WorkerCount:     2,
		RequestInterval: 0,
		ErrorBackoffMin: time.Millisecond,
		ErrorBackoffMax: 2 * time.Millisecond,
		BudgetLimiter:   stubBudgetLimiter{},
		BudgetContext:   polling.BudgetContext{Namespace: "test", InstanceID: "a", Enabled: true},
	})
	p := &alwaysFailPoller{name: "videos"}
	require.NoError(t, scheduler.RegisterCheckedWithBudgetProfile(
		"channel-race", p, PriorityNormal, time.Millisecond,
		polling.BudgetProfile{SourceUnits: map[polling.BudgetSource]float64{polling.BudgetSourceYouTubeScraper: 1}},
	))

	scheduler.Start(context.Background())
	t.Cleanup(scheduler.Stop)

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		scheduler.SyncPollerTargets(&PollerTargetSync{
			Poller:   p,
			Priority: PriorityHigh,
			Interval: time.Millisecond,
			BudgetProfile: polling.BudgetProfile{
				SourceUnits: map[polling.BudgetSource]float64{polling.BudgetSourceHolodexLive: 1},
			},
			ChannelIDs: []string{"channel-race"},
		})
		time.Sleep(100 * time.Microsecond)
	}
	require.Positive(t, p.calls.Load())
}

type panicOncePoller struct {
	name   string
	calls  atomic.Int32
	polled chan struct{}
}

func (p *panicOncePoller) Poll(context.Context, string) error {
	if p.calls.Add(1) == 1 {
		panic("poll blew up")
	}
	select {
	case p.polled <- struct{}{}:
	default:
	}
	return nil
}

func (p *panicOncePoller) Name() string { return p.name }

func TestSchedulerWorkerSurvivesPollPanicAndReschedulesJob(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: 0,
		ErrorBackoffMin: 10 * time.Millisecond,
		ErrorBackoffMax: 20 * time.Millisecond,
	})
	p := &panicOncePoller{name: "videos", polled: make(chan struct{}, 1)}
	scheduler.Register("channel-panic", p, PriorityNormal, 10*time.Millisecond)

	scheduler.Start(context.Background())
	t.Cleanup(scheduler.Stop)

	select {
	case <-p.polled:
	case <-time.After(5 * time.Second):
		t.Fatal("job was not rescheduled after a poll panic; worker likely died")
	}
	require.GreaterOrEqual(t, p.calls.Load(), int32(2))
}
