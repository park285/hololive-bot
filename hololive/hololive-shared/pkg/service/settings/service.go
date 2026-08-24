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

	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
)

type Settings struct {
	AlarmAdvanceMinutes int   `json:"alarmAdvanceMinutes"`
	ScraperProxyEnabled bool  `json:"scraperProxyEnabled"`
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
	ScraperProxyEnabled *bool `json:"scraperProxyEnabled,omitempty"`
	TargetMinutes       []int `json:"targetMinutes,omitempty"`
}

func cloneTargetMinutes(targetMinutes []int) []int {
	if len(targetMinutes) == 0 {
		return nil
	}

	return append([]int(nil), targetMinutes...)
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

func NewSettingsService(filePath string, defaults Settings, logger *slog.Logger) *Service {
	if defaults.AlarmAdvanceMinutes <= 0 {
		defaults.AlarmAdvanceMinutes = 5
	}

	s := &Service{
		filePath: filePath,
		logger:   logger,
		cache: &Settings{
			AlarmAdvanceMinutes: defaults.AlarmAdvanceMinutes,
			ScraperProxyEnabled: defaults.ScraperProxyEnabled,
			TargetMinutes:       sharedchecker.NewTargetMinutePolicyFromConfigured(defaults.TargetMinutes).Clone(),
		},
	}

	if err := ensureParentDir(filePath); err != nil && s.logger != nil {
		s.logger.Warn("Failed to ensure settings directory", slog.Any("error", err))
	}

	s.load()

	return s
}

func (s *Service) load() {
	f, err := os.Open(s.filePath)
	if err != nil {
		return // 파일이 없으면 기본값 사용함
	}

	defer func() {
		if closeErr := f.Close(); closeErr != nil && s.logger != nil {
			s.logger.Warn("Failed to close settings file", slog.String("error", closeErr.Error()))
		}
	}()

	var disk settingsDisk

	if err := jsonv2.UnmarshalRead(f, &disk); err != nil {
		if s.logger != nil {
			s.logger.Warn("Failed to decode settings file, using defaults", slog.String("error", err.Error()))
		}

		return
	}

	s.applyDiskSettings(disk)
}

func (s *Service) applyDiskSettings(disk settingsDisk) {
	if disk.AlarmAdvanceMinutes != nil && *disk.AlarmAdvanceMinutes > 0 {
		s.cache.AlarmAdvanceMinutes = *disk.AlarmAdvanceMinutes
	}

	if disk.ScraperProxyEnabled != nil {
		s.cache.ScraperProxyEnabled = *disk.ScraperProxyEnabled
	}

	s.applyDiskTargetMinutes(disk.TargetMinutes)
}

func (s *Service) applyDiskTargetMinutes(targetMinutes []int) {
	if len(targetMinutes) == 0 {
		return
	}

	resolved := sharedchecker.NewTargetMinutePolicyFromPersisted(s.cache.AlarmAdvanceMinutes, targetMinutes).Clone()

	s.cache.TargetMinutes = cloneTargetMinutes(resolved)

	if slices.Equal(resolved, targetMinutes) {
		return
	}

	s.logHealedTargetMinutes(targetMinutes, resolved)
	s.persistHealedSettings()
}

func (s *Service) logHealedTargetMinutes(from, to []int) {
	if s.logger != nil {
		s.logger.Info("Healing persisted target minutes", slog.Any("from", from), slog.Any("to", to))
	}
}

func (s *Service) persistHealedSettings() {
	if err := s.persistCache(); err != nil && s.logger != nil {
		s.logger.Warn("Failed to persist healed settings", slog.String("error", err.Error()))
	}
}

func (s *Service) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return Settings{
		AlarmAdvanceMinutes: s.cache.AlarmAdvanceMinutes,
		ScraperProxyEnabled: s.cache.ScraperProxyEnabled,
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

	resolvedTargets := sharedchecker.NewTargetMinutePolicyFromConfigured(newSettings.TargetMinutes).Clone()

	s.cache = &Settings{
		AlarmAdvanceMinutes: newSettings.AlarmAdvanceMinutes,
		ScraperProxyEnabled: newSettings.ScraperProxyEnabled,
		TargetMinutes:       resolvedTargets,
	}

	if err := s.persistCache(); err != nil {
		return fmt.Errorf("persist cache: %w", err)
	}

	return nil
}

// 같은 디렉터리의 temp 파일에 전량 기록한 뒤 rename으로 교체한다. 제자리 truncate+write는
// 중간에 실패하면 잘린 settings 파일을 남기고, 그 파일은 다음 기동에서 기본값으로 조용히 대체된다.
func (s *Service) persistCache() (err error) {
	tempPath, writeErr := s.writeSettingsTempFile()

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

func (s *Service) writeSettingsTempFile() (path string, err error) {
	temp, err := os.CreateTemp(filepath.Dir(s.filePath), filepath.Base(s.filePath)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp settings file: %w", err)
	}

	defer func() {
		if closeErr := temp.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close temp settings file: %w", closeErr)
		}
	}()

	if encodeErr := jsonv2.MarshalWrite(temp, s.cache); encodeErr != nil {
		return temp.Name(), fmt.Errorf("failed to write settings: %w", encodeErr)
	}

	if syncErr := temp.Sync(); syncErr != nil {
		return temp.Name(), fmt.Errorf("failed to sync settings: %w", syncErr)
	}

	return temp.Name(), nil
}
