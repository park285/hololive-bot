package subscriptions

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func newDiscardAlarmLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestTargetMinutesUpdatesReturnConfirmedResult(t *testing.T) {
	t.Parallel()

	as := &AlarmService{logger: newDiscardAlarmLogger()}
	assert.Equal(t, []int{5, 3, 1}, as.GetTargetMinutes())

	updated, err := as.UpdateAlarmAdvanceMinutes(t.Context(), 10)
	require.NoError(t, err)
	assert.Equal(t, domain.ApplyConfirmed, updated.Outcome)
	assert.Equal(t, 10, updated.RequestedMinutes)
	assert.Equal(t, []int{10, 3, 1}, updated.TargetMinutes)
	assert.Equal(t, []int{10, 3, 1}, as.GetTargetMinutes())

	updated, err = as.UpdateAlarmAdvanceMinutes(t.Context(), 1)
	require.NoError(t, err)
	assert.Equal(t, domain.ApplyConfirmed, updated.Outcome)
	assert.Equal(t, 1, updated.RequestedMinutes)
	assert.Equal(t, []int{1}, updated.TargetMinutes)
	assert.Equal(t, []int{1}, as.GetTargetMinutes())
}
