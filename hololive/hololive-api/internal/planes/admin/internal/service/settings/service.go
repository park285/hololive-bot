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
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

// ReadWriter는 settings 조회와 저장이 모두 필요한 admin handler가 쓰는 interface다.
type ReadWriter interface {
	Get() settingssvc.Settings
	Update(newSettings settingssvc.Settings) error
}

var _ ReadWriter = (*Service)(nil)

// Service는 admin plane이 소유하는 settings.json 정본이다. 디스크에는 settingssvc.Settings를 그대로 기록하므로
// alarm-worker가 기동 때 쓰는 settingssvc.ReadFile과 형식이 갈라지지 않는다.
type Service struct {
	filePath string
	mu       sync.RWMutex
	cache    *settingssvc.Settings
}

// BuildSettingsService는 admin plane이 쓰는 settings 서비스를 만든다. 파일 정본은 admin plane이 소유하므로 기동 검증도 admin plane 하나가 맡는다. 저장 파일을 읽지 못하거나 계약에 맞지
// 않으면 기동을 실패시킨다.
func BuildSettingsService(settingsPath string, targetMinutes []int, logger *slog.Logger) (ReadWriter, error) {
	if logger != nil {
		logger.Info("Using settings file path", slog.String("path", settingsPath))
	}

	policy := targetpolicy.NewTargetMinutePolicyFromConfigured(targetMinutes)

	service, err := NewSettingsService(settingsPath, settingssvc.Settings{
		AlarmAdvanceMinutes: policy.PrimaryAdvanceMinute(),
		TargetMinutes:       policy.Clone(),
	})
	if err != nil {
		return nil, fmt.Errorf("build settings service: %w", err)
	}

	return service, nil
}

func cloneTargetMinutes(targetMinutes []int) []int {
	if len(targetMinutes) == 0 {
		return nil
	}

	return slices.Clone(targetMinutes)
}

func ensureParentDir(filePath string) error {
	dir := filepath.Dir(filePath)
	if dir == "" || dir == "." {
		return nil
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}

	return nil
}

// NewSettingsService는 저장된 settings 파일이 있으면 그 값을, 없으면 기본값을 쓴다. 읽기·decode 실패나 계약에 맞지 않는
// 파일은 기본값으로 대신하지 않고 오류로 돌려 기동을 멈춘다(settingssvc.ReadFile). 디렉터리를 만들지 않으며 Update가
// 저장 직전에 만든다.
func NewSettingsService(filePath string, defaults settingssvc.Settings) (*Service, error) {
	s := &Service{
		filePath: filePath,
		cache: &settingssvc.Settings{
			AlarmAdvanceMinutes: defaults.AlarmAdvanceMinutes,
			TargetMinutes:       targetpolicy.NewTargetMinutePolicyFromConfigured(defaults.TargetMinutes).Clone(),
		},
	}

	stored, found, err := settingssvc.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("new settings service: %w", err)
	}

	if found {
		s.cache = &stored
	}

	return s, nil
}

func (s *Service) Get() settingssvc.Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return settingssvc.Settings{
		AlarmAdvanceMinutes: s.cache.AlarmAdvanceMinutes,
		TargetMinutes:       cloneTargetMinutes(s.cache.TargetMinutes),
	}
}

func (s *Service) Update(newSettings settingssvc.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if newSettings.AlarmAdvanceMinutes <= 0 {
		return errors.New("alarmAdvanceMinutes must be greater than 0")
	}

	if err := ensureParentDir(s.filePath); err != nil {
		return fmt.Errorf("ensure parent dir: %w", err)
	}

	resolvedTargets := targetpolicy.NewTargetMinutePolicyFromConfigured(newSettings.TargetMinutes).Clone()

	candidate := settingssvc.Settings{
		AlarmAdvanceMinutes: newSettings.AlarmAdvanceMinutes,
		TargetMinutes:       resolvedTargets,
	}

	if err := s.persistSettings(candidate); err != nil {
		return fmt.Errorf("persist cache: %w", err)
	}

	// 저장이 성공한 값만 공개하여 실패 응답 뒤 Get과 파일 값이 갈라지지 않게 한다.
	s.cache = &candidate

	return nil
}

// 같은 디렉터리의 temp 파일에 전량 기록한 뒤 rename으로 교체한다. 제자리 truncate+write는
// 중간에 실패하면 잘린 settings 파일을 남겨 다음 기동의 읽기 계약을 깨뜨린다.
func (s *Service) persistSettings(candidate settingssvc.Settings) (err error) {
	tempPath, writeErr := s.writeSettingsTempFile(candidate)

	defer func() {
		if err == nil || tempPath == "" {
			return
		}

		if removeErr := os.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove temp settings file: %w", removeErr))
		}
	}()

	if writeErr != nil {
		return fmt.Errorf("write settings temp file: %w", writeErr)
	}

	if err = os.Rename(tempPath, s.filePath); err != nil {
		return fmt.Errorf("replace settings file: %w", err)
	}

	return nil
}

func (s *Service) writeSettingsTempFile(candidate settingssvc.Settings) (path string, err error) {
	temp, err := os.CreateTemp(filepath.Dir(s.filePath), filepath.Base(s.filePath)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create temp settings file: %w", err)
	}

	defer func() {
		if closeErr := temp.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close temp settings file: %w", closeErr)
		}
	}()

	if encodeErr := jsonv2.MarshalWrite(temp, candidate); encodeErr != nil {
		return temp.Name(), fmt.Errorf("write settings: %w", encodeErr)
	}

	if syncErr := temp.Sync(); syncErr != nil {
		return temp.Name(), fmt.Errorf("sync settings: %w", syncErr)
	}

	return temp.Name(), nil
}
