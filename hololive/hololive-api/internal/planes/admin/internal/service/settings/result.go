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
