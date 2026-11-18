package settings

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type advanceResultAlarm struct {
	result domain.AdvanceMinutesResult
	err    error
}

func (a advanceResultAlarm) GetTargetMinutes() []int {
	return nil
}

func (a advanceResultAlarm) UpdateAlarmAdvanceMinutes(context.Context, int) (domain.AdvanceMinutesResult, error) {
	return a.result, a.err
}

func TestSettingsApplierPreservesAdvanceOutcomeAndError(t *testing.T) {
	t.Parallel()

	applyErr := errors.New("worker response lost")

	for _, outcome := range []domain.ApplyOutcome{domain.ApplyUnknown, domain.ApplyRejected} {
		t.Run(string(outcome), func(t *testing.T) {
			t.Parallel()

			applier := NewSettingsApplier(advanceResultAlarm{
				result: domain.AdvanceMinutesResult{RequestedMinutes: 15, Outcome: outcome, TargetMinutes: []int{5, 3, 1}},
				err:    applyErr,
			})
			result, err := applier.ApplyAlarmAdvanceMinutes(t.Context(), 15)
			require.ErrorIs(t, err, applyErr)
			assert.False(t, result.AlarmApplied)
			assert.Equal(t, 15, result.AlarmRequestedAdvanceMinutes)
			assert.NotEmpty(t, result.AlarmReason)

			if outcome == domain.ApplyUnknown {
				assert.Contains(t, result.AlarmReason, "outcome_unknown")
			} else {
				assert.NotContains(t, result.AlarmReason, "outcome_unknown")
			}

			assert.NotContains(t, result.AsMap(), "alarm_target_minutes")
		})
	}
}

func TestSettingsApplier_SettingsRuntimeState_NilAlarm(t *testing.T) {
	t.Parallel()

	runtime := NewSettingsApplier(nil).SettingsRuntimeState()

	if got := runtime.AsMap(); len(got) != 0 {
		t.Fatalf("SettingsRuntimeState().AsMap() = %#v, want empty without alarm service", got)
	}
}
