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

type ScraperProxyApplyResult struct {
	Requested               bool   `json:"requested"`
	Applied                 *bool  `json:"applied,omitempty"`
	Reason                  string `json:"reason,omitempty"`
	YoutubeApplied          *bool  `json:"youtube_applied,omitempty"`
	YoutubeEnabled          *bool  `json:"youtube_enabled,omitempty"`
	HolodexApplied          *bool  `json:"holodex_applied,omitempty"`
	HolodexEnabled          *bool  `json:"holodex_enabled,omitempty"`
	SchedulerPollersApplied *int   `json:"scheduler_pollers_applied,omitempty"`
	SchedulerEnabled        *bool  `json:"scheduler_enabled,omitempty"`
	SchedulerKnown          *bool  `json:"scheduler_known,omitempty"`
}

func (r *ScraperProxyApplyResult) AsMap() map[string]any {
	if r == nil {
		return map[string]any{}
	}
	out := map[string]any{
		"requested": r.Requested,
	}
	putOptionalPtr(out, "applied", r.Applied)
	if r.Reason != "" {
		out["reason"] = r.Reason
	}
	putOptionalPtr(out, "youtube_applied", r.YoutubeApplied)
	putOptionalPtr(out, "youtube_enabled", r.YoutubeEnabled)
	putOptionalPtr(out, "holodex_applied", r.HolodexApplied)
	putOptionalPtr(out, "holodex_enabled", r.HolodexEnabled)
	putOptionalPtr(out, "scheduler_pollers_applied", r.SchedulerPollersApplied)
	putOptionalPtr(out, "scheduler_enabled", r.SchedulerEnabled)
	putOptionalPtr(out, "scheduler_known", r.SchedulerKnown)
	return out
}

func putOptionalPtr[T any](out map[string]any, key string, value *T) {
	if value != nil {
		out[key] = *value
	}
}

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

type ScraperProxyRuntimeStateResult struct {
	Requested          bool   `json:"requested"`
	Known              *bool  `json:"known,omitempty"`
	Reason             string `json:"reason,omitempty"`
	YoutubeEnabled     *bool  `json:"youtube_enabled,omitempty"`
	HolodexEnabled     *bool  `json:"holodex_enabled,omitempty"`
	SchedulerEnabled   *bool  `json:"scheduler_enabled,omitempty"`
	SchedulerKnown     *bool  `json:"scheduler_known,omitempty"`
	AlarmTargetMinutes []int  `json:"alarm_target_minutes,omitempty"`
}

func (r *ScraperProxyRuntimeStateResult) AsMap() map[string]any {
	if r == nil {
		return map[string]any{}
	}
	out := map[string]any{
		"requested": r.Requested,
	}
	if r.Known != nil {
		out["known"] = *r.Known
	}
	if r.Reason != "" {
		out["reason"] = r.Reason
	}
	if r.YoutubeEnabled != nil {
		out["youtube_enabled"] = *r.YoutubeEnabled
	}
	if r.HolodexEnabled != nil {
		out["holodex_enabled"] = *r.HolodexEnabled
	}
	if r.SchedulerEnabled != nil {
		out["scheduler_enabled"] = *r.SchedulerEnabled
	}
	if r.SchedulerKnown != nil {
		out["scheduler_known"] = *r.SchedulerKnown
	}
	if len(r.AlarmTargetMinutes) > 0 {
		out["alarm_target_minutes"] = r.AlarmTargetMinutes
	}
	return out
}
