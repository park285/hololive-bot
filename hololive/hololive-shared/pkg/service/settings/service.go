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
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
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
			TargetMinutes:       cloneTargetMinutes(defaults.TargetMinutes),
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
		_ = f.Close()
	}()

	var disk settingsDisk
	if err := json.NewDecoder(f).Decode(&disk); err != nil {
		if s.logger != nil {
			s.logger.Warn("Failed to decode settings file, using defaults", slog.String("error", err.Error()))
		}
		return
	}

	if disk.AlarmAdvanceMinutes != nil && *disk.AlarmAdvanceMinutes > 0 {
		s.cache.AlarmAdvanceMinutes = *disk.AlarmAdvanceMinutes
	}
	if disk.ScraperProxyEnabled != nil {
		s.cache.ScraperProxyEnabled = *disk.ScraperProxyEnabled
	}
	if len(disk.TargetMinutes) > 0 {
		s.cache.TargetMinutes = cloneTargetMinutes(disk.TargetMinutes)
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
		return fmt.Errorf("alarmAdvanceMinutes must be greater than 0")
	}

	if err := ensureParentDir(s.filePath); err != nil {
		return err
	}

	s.cache = &Settings{
		AlarmAdvanceMinutes: newSettings.AlarmAdvanceMinutes,
		ScraperProxyEnabled: newSettings.ScraperProxyEnabled,
		TargetMinutes:       cloneTargetMinutes(newSettings.TargetMinutes),
	}

	f, err := os.Create(s.filePath)
	if err != nil {
		return fmt.Errorf("failed to create settings file: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	if err := json.NewEncoder(f).Encode(s.cache); err != nil {
		return fmt.Errorf("failed to write settings: %w", err)
	}
	return nil
}
