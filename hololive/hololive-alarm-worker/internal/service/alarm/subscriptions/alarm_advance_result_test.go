package subscriptions

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestAdvanceMinutesCanceledDoesNotChangeTargets(t *testing.T) {
	t.Parallel()

	service := newTestAlarmService(t)
	previous := service.GetTargetMinutes()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	result, err := service.UpdateAlarmAdvanceMinutes(ctx, 20)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, domain.ApplyRejected, result.Outcome)
	assert.Equal(t, 20, result.RequestedMinutes)
	assert.Empty(t, result.TargetMinutes)
	assert.Equal(t, previous, service.GetTargetMinutes())
}

func TestAdvanceMinutesResultOwnsTargetsAndPreservesZeroNormalization(t *testing.T) {
	t.Parallel()

	service := newTestAlarmService(t)
	result, err := service.UpdateAlarmAdvanceMinutes(t.Context(), 20)
	require.NoError(t, err)
	assert.Equal(t, domain.ApplyConfirmed, result.Outcome)

	result.TargetMinutes[0] = 999

	assert.Equal(t, []int{20, 3, 1}, service.GetTargetMinutes())

	// 공개 0분 의미 결정과 분리하여 기존 local normalization을 보존한다.
	result, err = service.UpdateAlarmAdvanceMinutes(t.Context(), 0)
	require.NoError(t, err)
	assert.Equal(t, 0, result.RequestedMinutes)
	assert.Equal(t, domain.ApplyConfirmed, result.Outcome)
	assert.Equal(t, targetpolicy.BuildRuntimeTargetMinutes(0), result.TargetMinutes)
	assert.Equal(t, result.TargetMinutes, service.GetTargetMinutes())
}
