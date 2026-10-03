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

package botruntime

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sharedserver "github.com/kapu/hololive-api/internal/server/settings"
)

type trackingSettingsApplier struct {
	lastAlarmMinutes int
	alarmResult      sharedserver.AlarmAdvanceMinutesApplyResult
	runtimeResult    sharedserver.SettingsRuntimeStateResult
}

func (a *trackingSettingsApplier) ApplyAlarmAdvanceMinutes(_ context.Context, minutes int) (sharedserver.AlarmAdvanceMinutesApplyResult, error) {
	a.lastAlarmMinutes = minutes
	return a.alarmResult, nil
}

func (a *trackingSettingsApplier) ApplyMemberNewsWeeklyRunNow(context.Context) sharedserver.MemberNewsWeeklyRunNowResult {
	return sharedserver.MemberNewsWeeklyRunNowResult{
		Applied: false,
		Reason:  "not used in delegation test",
	}
}

func (a *trackingSettingsApplier) SettingsRuntimeState() sharedserver.SettingsRuntimeStateResult {
	return a.runtimeResult
}

type trackingMemberNewsRunNowTrigger struct {
	called int
	err    error
}

func (t *trackingMemberNewsRunNowTrigger) SendMemberNewsWeekly(context.Context) error {
	t.called++
	return t.err
}

func testAppLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestNewBotSettingsApplier_DefaultLogger(t *testing.T) {
	t.Parallel()

	base := &trackingSettingsApplier{}

	applier := newBotSettingsApplier(base, nil, nil)
	wrapped, ok := applier.(*botSettingsApplier)
	require.True(t, ok)
	require.NotNil(t, wrapped)
	assert.Same(t, base, wrapped.SettingsApplier)
	assert.NotNil(t, wrapped.logger)
}

func TestBotSettingsApplier_DelegatesToBase(t *testing.T) {
	t.Parallel()

	expectedAlarm := sharedserver.AlarmAdvanceMinutesApplyResult{
		AlarmRequestedAdvanceMinutes: 15,
		AlarmApplied:                 true,
		AlarmTargetMinutes:           []int{5, 15},
	}
	expectedRuntime := sharedserver.SettingsRuntimeStateResult{AlarmTargetMinutes: []int{5, 15}}
	base := &trackingSettingsApplier{
		alarmResult:   expectedAlarm,
		runtimeResult: expectedRuntime,
	}
	applier := &botSettingsApplier{SettingsApplier: base}

	alarmResult, err := applier.ApplyAlarmAdvanceMinutes(t.Context(), 15)
	require.NoError(t, err)
	assert.Equal(t, expectedAlarm, alarmResult)
	assert.Equal(t, 15, base.lastAlarmMinutes)

	assert.Equal(t, expectedRuntime, applier.SettingsRuntimeState())
}

func TestBotSettingsApplier_ApplyMemberNewsWeeklyRunNow(t *testing.T) {
	t.Parallel()

	t.Run("nil trigger", func(t *testing.T) {
		t.Parallel()

		applier := &botSettingsApplier{
			SettingsApplier:  nil,
			memberNewsRunNow: nil,
			logger:           testAppLogger(),
		}

		result := applier.ApplyMemberNewsWeeklyRunNow(t.Context())
		assert.False(t, result.Applied)
		assert.Equal(t, "member news trigger is not configured", result.Reason)
		assert.Empty(t, result.Error)
	})

	t.Run("trigger failure", func(t *testing.T) {
		t.Parallel()

		trigger := &trackingMemberNewsRunNowTrigger{err: errors.New("request failed")}
		applier := &botSettingsApplier{
			memberNewsRunNow: trigger,
			logger:           testAppLogger(),
		}

		result := applier.ApplyMemberNewsWeeklyRunNow(t.Context())

		assert.Equal(t, 1, trigger.called)
		assert.False(t, result.Applied)
		assert.Equal(t, "member news trigger failed", result.Reason)
		assert.Equal(t, "request failed", result.Error)
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		trigger := &trackingMemberNewsRunNowTrigger{}
		applier := &botSettingsApplier{
			memberNewsRunNow: trigger,
			logger:           testAppLogger(),
		}

		result := applier.ApplyMemberNewsWeeklyRunNow(t.Context())

		assert.Equal(t, 1, trigger.called)
		assert.True(t, result.Applied)
		assert.Equal(t, "member_news_trigger", result.Source)
		assert.Empty(t, result.Error)
	})
}

var (
	_ sharedserver.SettingsApplier  = (*trackingSettingsApplier)(nil)
	_ memberNewsWeeklyRunNowTrigger = (*trackingMemberNewsRunNowTrigger)(nil)
)
