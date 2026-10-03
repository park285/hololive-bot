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
	"fmt"
	"slices"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// localSettingsApplier: admin plane 프로세스 내 직접 설정 적용 (in-process).
type localSettingsApplier struct {
	alarm AlarmAdvanceService
}

// AlarmAdvanceService는 설정 적용과 마지막으로 확인한 target 조회만 요구한다.
type AlarmAdvanceService interface {
	UpdateAlarmAdvanceMinutes(context.Context, int) (domain.AdvanceMinutesResult, error)
	GetTargetMinutes() []int
}

var _ SettingsApplier = (*localSettingsApplier)(nil)

func NewLocalSettingsApplier(alarm AlarmAdvanceService) SettingsApplier {
	return &localSettingsApplier{alarm: alarm}
}

func (a *localSettingsApplier) ApplyAlarmAdvanceMinutes(ctx context.Context, minutes int) (AlarmAdvanceMinutesApplyResult, error) {
	runtime := AlarmAdvanceMinutesApplyResult{
		AlarmRequestedAdvanceMinutes: minutes,
		AlarmReason:                  "alarm worker outcome_unknown: alarm advance minutes application could not be confirmed",
	}

	if a.alarm == nil {
		runtime.AlarmApplied = false
		runtime.AlarmReason = "alarm service not configured"

		return runtime, errors.New("apply alarm advance minutes: alarm service not configured")
	}

	result, err := a.alarm.UpdateAlarmAdvanceMinutes(ctx, minutes)
	switch result.Outcome {
	case domain.ApplyRejected:
		runtime.AlarmReason = "alarm worker did not apply alarm advance minutes"
	case domain.ApplyConfirmed:
		if err == nil && len(result.TargetMinutes) > 0 {
			runtime.AlarmApplied = true
			runtime.AlarmReason = ""
			runtime.AlarmTargetMinutes = slices.Clone(result.TargetMinutes)
		}
	case domain.ApplyUnknown:
		runtime.AlarmReason = "alarm worker outcome_unknown: alarm advance minutes application could not be confirmed"
	}

	if err != nil {
		return runtime, fmt.Errorf("apply alarm advance minutes: %w", err)
	}

	if !runtime.AlarmApplied {
		if runtime.AlarmReason == "" {
			runtime.AlarmReason = "alarm worker outcome_unknown: alarm advance minutes application could not be confirmed"
		}

		return runtime, errors.New("apply alarm advance minutes: application was not confirmed")
	}

	return runtime, nil
}

func (a *localSettingsApplier) ApplyMemberNewsWeeklyRunNow(_ context.Context) MemberNewsWeeklyRunNowResult {
	return MemberNewsWeeklyRunNowResult{
		Applied: false,
		Reason:  "llm scheduler settings are not available in local mode",
	}
}

func (a *localSettingsApplier) SettingsRuntimeState() SettingsRuntimeStateResult {
	if a.alarm == nil {
		return SettingsRuntimeStateResult{}
	}

	return SettingsRuntimeStateResult{AlarmTargetMinutes: a.alarm.GetTargetMinutes()}
}
