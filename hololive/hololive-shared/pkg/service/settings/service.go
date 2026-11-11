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
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"

	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
)

// Settings는 관리 화면에서 변경하는 알림 설정이자 settings.json의 디스크 형식이다. API admin writer가 이 타입을 그대로
// 기록하므로 형식을 바꾸면 ReadFile도 함께 바꿔야 한다. ReadFile은 알 수 없는 JSON 필드를 무시하므로 다시 저장할 때 그
// 필드는 남지 않는다.
type Settings struct {
	AlarmAdvanceMinutes int   `json:"alarmAdvanceMinutes"`
	TargetMinutes       []int `json:"targetMinutes,omitempty"`
}

type settingsDisk struct {
	AlarmAdvanceMinutes *int  `json:"alarmAdvanceMinutes,omitempty"`
	TargetMinutes       []int `json:"targetMinutes,omitempty"`
}

// ReadFile은 API와 worker가 공유하는 settings 파서다. 파일이 없으면 found=false를 반환한다.
// 읽기·decode 실패나 양수 alarmAdvanceMinutes·targetMinutes 누락은 오류다. 파일을 수정하지 않는다.
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
