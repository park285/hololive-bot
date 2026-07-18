package workerruntime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotificationEgressRunnerRetriesHeldLeaseUntilAcquired(t *testing.T) {
	var setNXCalls atomic.Int32
	var schedulerStarts atomic.Int32
	cache := &cachemocks.Client{
		SetNXFunc: func(context.Context, string, string, time.Duration) (bool, error) {
			return setNXCalls.Add(1) > 1, nil
		},
		CompareAndDeleteFunc: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}
	runner := notificationEgressRunner{
		leaseCache:         cache,
		leaseEnabled:       true,
		leaseRetryInterval: time.Millisecond,
	}

	err := runner.startWithLease(t.Context(), []NamedScheduler{{
		Name:      "test",
		Scheduler: runtimeAlarmSchedulerFunc(func(context.Context) error { schedulerStarts.Add(1); return nil }),
	}})

	require.NoError(t, err)
	assert.GreaterOrEqual(t, setNXCalls.Load(), int32(2))
	assert.Equal(t, int32(1), schedulerStarts.Load())
}

func TestNotificationEgressRunnerReleaseLeaseIgnoresCanceledParentContext(t *testing.T) {
	var releaseSawCanceledContext bool
	cache := &cachemocks.Client{
		SetNXFunc: func(_ context.Context, _ string, _ string, _ time.Duration) (bool, error) {
			return true, nil
		},
		CompareAndDeleteFunc: func(ctx context.Context, _ string, _ string) (bool, error) {
			releaseSawCanceledContext = ctx.Err() != nil
			return true, nil
		},
	}
	lease, err := egress.AcquireNotificationEgressLease(t.Context(), cache, nil)
	require.NoError(t, err)

	parent, cancel := context.WithCancel(t.Context())
	cancel()

	runner := notificationEgressRunner{leaseEnabled: true}
	runner.releaseLease(parent, lease)

	assert.False(t, releaseSawCanceledContext)
}

type runtimeAlarmSchedulerFunc func(context.Context) error

func (f runtimeAlarmSchedulerFunc) Start(ctx context.Context) error {
	return f(ctx)
}
