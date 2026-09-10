package holo

import (
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"net/http"

	"github.com/kapu/admin-dashboard/internal/contract"
)

// SettingsUpdateRequest는 저장할 알람 값을 명시하며 0도 유효한 값입니다.
type SettingsUpdateRequest struct {
	AlarmAdvanceMinutes *int `json:"alarmAdvanceMinutes"`
}

// Validate는 upstream의 0~1440 범위와 필드 존재를 검사합니다.
func (r SettingsUpdateRequest) Validate() error {
	if r.AlarmAdvanceMinutes == nil || *r.AlarmAdvanceMinutes < 0 || *r.AlarmAdvanceMinutes > 1440 {
		return contract.BadRequest("alarmAdvanceMinutes must be between 0 and 1440")
	}

	return nil
}

// SettingsRuntimeResult는 제공된 적용·전파 결과만 보존하며 부재를 false로 만들지 않습니다.
type SettingsRuntimeResult struct {
	AlarmApplied                          *bool   `json:"alarm_applied,omitempty"`
	AlarmRequestedAdvanceMinutes          *int    `json:"alarm_requested_advance_minutes,omitempty"`
	AlarmReason                           *string `json:"alarm_reason,omitempty"`
	AlarmTargetMinutes                    *[]*int `json:"alarm_target_minutes,omitzero"`
	ConfigPublishAlarmAdvanceMinutes      *bool   `json:"config_publish_alarm_advance_minutes,omitempty"`
	ConfigPublishAlarmAdvanceMinutesError *string `json:"config_publish_alarm_advance_minutes_error,omitempty"`
}

// SettingsUpdateResponse는 저장 확인과 별개의 runtime 적용·전파 결과를 반환합니다.
type SettingsUpdateResponse struct {
	Status   string                `json:"status"`
	Message  string                `json:"message"`
	Settings Settings              `json:"settings"`
	Runtime  SettingsRuntimeResult `json:"runtime"`
}

// UpdateSettings는 설정을 한 번 저장하며 이미 확인된 부분 효과와 전파 실패를 보존합니다.
func (c *Client) UpdateSettings(ctx context.Context, input SettingsUpdateRequest) (SettingsUpdateResponse, error) {
	if err := input.Validate(); err != nil {
		return SettingsUpdateResponse{}, err
	}

	body, err := jsonv2.Marshal(input)
	if err != nil {
		return SettingsUpdateResponse{}, contract.BadRequest("invalid settings request")
	}

	var wire struct {
		Status   string         `json:"status"`
		Settings *Settings      `json:"settings"`
		Runtime  jsontext.Value `json:"runtime"`
	}

	if decodeErr := c.request(ctx, http.MethodPost, "/api/holo/settings", nil, body, http.StatusOK, &wire); decodeErr != nil {
		return SettingsUpdateResponse{}, decodeErr
	}

	if wire.Status != "ok" || wire.Settings == nil || !wire.Settings.valid() || *wire.Settings.AlarmAdvanceMinutes != *input.AlarmAdvanceMinutes {
		return SettingsUpdateResponse{}, invalidResponse()
	}

	runtime, err := projectSettingsRuntime(wire.Runtime)
	if err != nil {
		return SettingsUpdateResponse{}, err
	}

	if runtime.AlarmRequestedAdvanceMinutes != nil && *runtime.AlarmRequestedAdvanceMinutes != *input.AlarmAdvanceMinutes {
		return SettingsUpdateResponse{}, invalidResponse()
	}

	return SettingsUpdateResponse{Status: "ok", Message: "Settings updated", Settings: *wire.Settings, Runtime: runtime}, nil
}

func projectSettingsRuntime(data jsontext.Value) (SettingsRuntimeResult, error) {
	var raw map[string]jsontext.Value

	if err := jsonv2.Unmarshal(data, &raw); err != nil || raw == nil {
		return SettingsRuntimeResult{}, invalidResponse()
	}

	fields := []string{"alarm_applied", "alarm_requested_advance_minutes", "alarm_reason", "alarm_target_minutes", "config_publish_alarm_advance_minutes", "config_publish_alarm_advance_minutes_error"}
	owned := make(map[string]jsontext.Value)

	for _, field := range fields {
		if value, present := raw[field]; present {
			if value.Kind() == 'n' {
				return SettingsRuntimeResult{}, invalidResponse()
			}

			owned[field] = value
		}
	}

	body, err := jsonv2.Marshal(owned)
	if err != nil {
		return SettingsRuntimeResult{}, invalidResponse()
	}

	var out SettingsRuntimeResult

	if err := jsonv2.Unmarshal(body, &out); err != nil {
		return SettingsRuntimeResult{}, invalidResponse()
	}

	if !validSettingsRuntime(out) {
		return SettingsRuntimeResult{}, invalidResponse()
	}

	if out.ConfigPublishAlarmAdvanceMinutesError != nil {
		// settings_handler.go는 fmt.Sprint(err)를 넣으므로 원문 오류를 외부에 전달하지 않습니다.
		message := "Settings propagation failed"

		out.ConfigPublishAlarmAdvanceMinutesError = &message
	}

	if out.AlarmReason != nil && *out.AlarmReason != "alarm service not configured" {
		message := "Runtime application was not confirmed"

		out.AlarmReason = &message
	}

	return out, nil
}

func validSettingsRuntime(out SettingsRuntimeResult) bool {
	if out.ConfigPublishAlarmAdvanceMinutes != nil && *out.ConfigPublishAlarmAdvanceMinutes && out.ConfigPublishAlarmAdvanceMinutesError != nil {
		return false
	}

	if out.AlarmRequestedAdvanceMinutes != nil && (*out.AlarmRequestedAdvanceMinutes < 0 || *out.AlarmRequestedAdvanceMinutes > 1440) {
		return false
	}

	if out.AlarmTargetMinutes != nil {
		for _, minute := range *out.AlarmTargetMinutes {
			if minute == nil || *minute < 0 || *minute > 1440 {
				return false
			}
		}
	}

	return true
}
