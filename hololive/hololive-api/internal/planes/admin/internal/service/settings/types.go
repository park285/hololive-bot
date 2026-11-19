package settings

import "context"

type SettingsApplier interface {
	ApplyAlarmAdvanceMinutes(ctx context.Context, minutes int) (AlarmAdvanceMinutesApplyResult, error)
	SettingsRuntimeState() SettingsRuntimeStateResult
}
