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
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
)

// Settings는 관리 화면이 바꾸는 알림 설정이다. 예전의 scraper proxy 토글(scraperProxyEnabled)은
// DEC-20260926-hololive-legacy-env-config-retirement로 지웠다. 기본 json/v2 decode는 모르는 멤버를 무시하므로 예전 파일에
// 남은 scraperProxyEnabled는 읽지 않고, 다음 Update가 파일을 두 값만으로 다시 쓴다(T18 2026-09-26: 운영 settings 파일 없음).
type Settings struct {
	AlarmAdvanceMinutes int   `json:"alarmAdvanceMinutes"`
	TargetMinutes       []int `json:"targetMinutes,omitempty"`
}

type Service struct {
	filePath string
	logger   *slog.Logger
	mu       sync.RWMutex
	cache    *Settings
}

type settingsDisk struct {
	AlarmAdvanceMinutes *int  `json:"alarmAdvanceMinutes,omitempty"`
	TargetMinutes       []int `json:"targetMinutes,omitempty"`
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
		return fmt.Errorf("failed to create settings directory: %w", err)
	}

	return nil
}

// NewSettingsService는 저장된 settings 파일을 읽어 기본값 위에 적용한다. 파일이 없으면 기본값을 쓰고, 읽기·decode 실패나
// 계약에 맞지 않는 파일은 기본값으로 대신하지 않고 오류로 돌려 기동을 멈춘다(ReadFile).
func NewSettingsService(filePath string, defaults Settings, logger *slog.Logger) (*Service, error) {
	if defaults.AlarmAdvanceMinutes <= 0 {
		defaults.AlarmAdvanceMinutes = 5
	}

	s := &Service{
		filePath: filePath,
		logger:   logger,
		cache: &Settings{
			AlarmAdvanceMinutes: defaults.AlarmAdvanceMinutes,
			TargetMinutes:       targetpolicy.NewTargetMinutePolicyFromConfigured(defaults.TargetMinutes).Clone(),
		},
	}

	if err := ensureParentDir(filePath); err != nil && s.logger != nil {
		s.logger.Warn("Failed to ensure settings directory", slog.Any("error", err))
	}

	stored, found, err := ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("new settings service: %w", err)
	}

	if found {
		s.cache = &stored
	}

	return s, nil
}

// ReadFile은 저장된 settings 파일의 유일한 파서다(hololive-api settings 서비스와 alarm-worker 기동이 함께 쓴다). 파일이 없으면
// found=false를 돌려준다. 읽기·decode 실패, 양수 alarmAdvanceMinutes가 없거나 양수 targetMinutes가 없는 파일은 오류다.
// 예전에 targetMinutes 없이 alarmAdvanceMinutes만 있는 구형 형식을 기본 알림 시점으로 해석하던 경로와, 정규화 결과가
// 저장값과 다를 때 파일을 다시 쓰던 자가 치유는 T18(2026-09-26)에서 운영 settings 파일이 없음을 확인해 지웠다(stack-audit
// 2026-09-26 T11 holo-settings-file-legacy-format-and-dual-reader). Update가 쓰는 파일은 항상 두 값을 담는다.
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

func (s *Service) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return Settings{
		AlarmAdvanceMinutes: s.cache.AlarmAdvanceMinutes,
		TargetMinutes:       cloneTargetMinutes(s.cache.TargetMinutes),
	}
}

func (s *Service) Update(newSettings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if newSettings.AlarmAdvanceMinutes <= 0 {
		return errors.New("alarmAdvanceMinutes must be greater than 0")
	}

	if err := ensureParentDir(s.filePath); err != nil {
		return fmt.Errorf("ensure parent dir: %w", err)
	}

	resolvedTargets := targetpolicy.NewTargetMinutePolicyFromConfigured(newSettings.TargetMinutes).Clone()

	candidate := Settings{
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
func (s *Service) persistSettings(candidate Settings) (err error) {
	tempPath, writeErr := s.writeSettingsTempFile(candidate)

	defer func() {
		if err == nil || tempPath == "" {
			return
		}

		if removeErr := os.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("failed to remove temp settings file: %w", removeErr))
		}
	}()

	if writeErr != nil {
		return fmt.Errorf("write settings temp file: %w", writeErr)
	}

	if err = os.Rename(tempPath, s.filePath); err != nil {
		return fmt.Errorf("failed to replace settings file: %w", err)
	}

	return nil
}

func (s *Service) writeSettingsTempFile(candidate Settings) (path string, err error) {
	temp, err := os.CreateTemp(filepath.Dir(s.filePath), filepath.Base(s.filePath)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp settings file: %w", err)
	}

	defer func() {
		if closeErr := temp.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close temp settings file: %w", closeErr)
		}
	}()

	if encodeErr := jsonv2.MarshalWrite(temp, candidate); encodeErr != nil {
		return temp.Name(), fmt.Errorf("failed to write settings: %w", encodeErr)
	}

	if syncErr := temp.Sync(); syncErr != nil {
		return temp.Name(), fmt.Errorf("failed to sync settings: %w", syncErr)
	}

	return temp.Name(), nil
}
