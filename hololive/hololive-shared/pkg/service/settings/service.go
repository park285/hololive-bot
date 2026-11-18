package settings

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"

	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
)

type Settings struct {
	AlarmAdvanceMinutes int   `json:"alarmAdvanceMinutes"`
	TargetMinutes       []int `json:"targetMinutes,omitempty"`
}

type settingsDisk struct {
	AlarmAdvanceMinutes *int  `json:"alarmAdvanceMinutes,omitempty"`
	TargetMinutes       []int `json:"targetMinutes,omitempty"`
}

func ReadFile(filePath string) (stored Settings, found bool, err error) {
	// #nosec G304 -- filePath는 운영 설정의 SettingsFilePath에서만 유도된다.
	file, err := os.Open(filePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Settings{}, false, nil
		}

		return Settings{}, false, fmt.Errorf("open settings file: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close settings file: %w", closeErr)
		}
	}()

	var disk settingsDisk

	if err := jsonv2.UnmarshalRead(file, &disk); err != nil {
		return Settings{}, false, fmt.Errorf("decode settings file: %w", err)
	}

	if disk.AlarmAdvanceMinutes == nil || *disk.AlarmAdvanceMinutes <= 0 {
		return Settings{}, false, errors.New("settings file has no positive alarmAdvanceMinutes")
	}

	if !slices.ContainsFunc(disk.TargetMinutes, func(minute int) bool { return minute > 0 }) {
		return Settings{}, false, errors.New("settings file has no positive targetMinutes; the alarmAdvanceMinutes-only format is unsupported")
	}

	stored = Settings{
		AlarmAdvanceMinutes: *disk.AlarmAdvanceMinutes,
		TargetMinutes:       targetpolicy.NewTargetMinutePolicy(disk.TargetMinutes).Clone(),
	}

	return stored, true, nil
}
