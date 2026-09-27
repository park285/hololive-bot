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

type AlarmAdvanceMinutesApplyResult struct {
	AlarmRequestedAdvanceMinutes int    `json:"alarm_requested_advance_minutes"`
	AlarmApplied                 bool   `json:"alarm_applied"`
	AlarmReason                  string `json:"alarm_reason,omitempty"`
	AlarmTargetMinutes           []int  `json:"alarm_target_minutes,omitempty"`
}

func (r AlarmAdvanceMinutesApplyResult) AsMap() map[string]any {
	out := map[string]any{
		"alarm_requested_advance_minutes": r.AlarmRequestedAdvanceMinutes,
		"alarm_applied":                   r.AlarmApplied,
	}
	if r.AlarmReason != "" {
		out["alarm_reason"] = r.AlarmReason
	}

	if len(r.AlarmTargetMinutes) > 0 {
		out["alarm_target_minutes"] = r.AlarmTargetMinutes
	}

	return out
}

type MemberNewsWeeklyRunNowResult struct {
	Applied bool   `json:"applied"`
	Reason  string `json:"reason,omitempty"`
	Error   string `json:"error,omitempty"`
	Source  string `json:"source,omitempty"`
}

func (r MemberNewsWeeklyRunNowResult) AsMap() map[string]any {
	out := map[string]any{
		"applied": r.Applied,
	}
	if r.Reason != "" {
		out["reason"] = r.Reason
	}

	if r.Error != "" {
		out["error"] = r.Error
	}

	if r.Source != "" {
		out["source"] = r.Source
	}

	return out
}

// SettingsRuntimeStateResult는 GET 설정 응답의 runtime 구획이다. 적용 중인 알림 시점만 담는다.
type SettingsRuntimeStateResult struct {
	AlarmTargetMinutes []int `json:"alarm_target_minutes,omitempty"`
}

func (r SettingsRuntimeStateResult) AsMap() map[string]any {
	out := map[string]any{}

	if len(r.AlarmTargetMinutes) > 0 {
		out["alarm_target_minutes"] = r.AlarmTargetMinutes
	}

	return out
}
