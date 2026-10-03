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

func TestLocalSettingsApplierPreservesAdvanceOutcomeAndError(t *testing.T) {
	t.Parallel()

	applyErr := errors.New("worker response lost")

	for _, outcome := range []domain.ApplyOutcome{domain.ApplyUnknown, domain.ApplyRejected} {
		t.Run(string(outcome), func(t *testing.T) {
			t.Parallel()

			applier := NewLocalSettingsApplier(advanceResultAlarm{
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

func TestLocalSettingsApplier_SettingsRuntimeState_NilAlarm(t *testing.T) {
	t.Parallel()

	runtime := NewLocalSettingsApplier(nil).SettingsRuntimeState()

	if got := runtime.AsMap(); len(got) != 0 {
		t.Fatalf("SettingsRuntimeState().AsMap() = %#v, want empty without alarm service", got)
	}
}
