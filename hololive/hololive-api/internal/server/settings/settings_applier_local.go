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

	"github.com/kapu/hololive-shared/pkg/domain"
)

// localSettingsApplier: admin plane 프로세스 내 직접 설정 적용 (in-process).
type localSettingsApplier struct {
	alarm domain.AlarmCRUD
}

var _ SettingsApplier = (*localSettingsApplier)(nil)

func NewLocalSettingsApplier(alarm domain.AlarmCRUD) SettingsApplier {
	return &localSettingsApplier{alarm: alarm}
}

func (a *localSettingsApplier) ApplyAlarmAdvanceMinutes(ctx context.Context, minutes int) AlarmAdvanceMinutesApplyResult {
	runtime := AlarmAdvanceMinutesApplyResult{
		AlarmRequestedAdvanceMinutes: minutes,
	}

	if a.alarm == nil {
		runtime.AlarmApplied = false
		runtime.AlarmReason = "alarm service not configured"

		return runtime
	}

	targetMinutes := a.alarm.UpdateAlarmAdvanceMinutes(ctx, minutes)

	runtime.AlarmApplied = true
	runtime.AlarmTargetMinutes = targetMinutes

	return runtime
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
