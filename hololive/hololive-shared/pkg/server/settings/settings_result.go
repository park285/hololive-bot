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

func (r ScraperProxyApplyResult) AsMap() map[string]any {
	out := map[string]any{
		"requested": r.Requested,
	}
	if r.Applied != nil {
		out["applied"] = *r.Applied
	}
	if r.Reason != "" {
		out["reason"] = r.Reason
	}
	if r.YoutubeApplied != nil {
		out["youtube_applied"] = *r.YoutubeApplied
	}
	if r.YoutubeEnabled != nil {
		out["youtube_enabled"] = *r.YoutubeEnabled
	}
	if r.HolodexApplied != nil {
		out["holodex_applied"] = *r.HolodexApplied
	}
	if r.HolodexEnabled != nil {
		out["holodex_enabled"] = *r.HolodexEnabled
	}
	if r.SchedulerPollersApplied != nil {
		out["scheduler_pollers_applied"] = *r.SchedulerPollersApplied
	}
	if r.SchedulerEnabled != nil {
		out["scheduler_enabled"] = *r.SchedulerEnabled
	}
	if r.SchedulerKnown != nil {
		out["scheduler_known"] = *r.SchedulerKnown
	}
	return out
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

func (r ScraperProxyRuntimeStateResult) AsMap() map[string]any {
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
