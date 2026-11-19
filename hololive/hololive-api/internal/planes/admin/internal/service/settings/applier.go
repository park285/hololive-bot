package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type settingsApplier struct {
	alarm AlarmAdvanceService
}

type AlarmAdvanceService interface {
	UpdateAlarmAdvanceMinutes(context.Context, int) (domain.AdvanceMinutesResult, error)
	GetTargetMinutes() []int
}

var _ SettingsApplier = (*settingsApplier)(nil)

func NewSettingsApplier(alarm AlarmAdvanceService) SettingsApplier {
	return &settingsApplier{alarm: alarm}
}

func (a *settingsApplier) ApplyAlarmAdvanceMinutes(ctx context.Context, minutes int) (AlarmAdvanceMinutesApplyResult, error) {
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

func (a *settingsApplier) SettingsRuntimeState() SettingsRuntimeStateResult {
	if a.alarm == nil {
		return SettingsRuntimeStateResult{}
	}

	return SettingsRuntimeStateResult{AlarmTargetMinutes: a.alarm.GetTargetMinutes()}
}
