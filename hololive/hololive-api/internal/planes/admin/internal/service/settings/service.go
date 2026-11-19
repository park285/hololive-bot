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

type ReadWriter interface {
	Get() settingssvc.Settings
	Update(newSettings settingssvc.Settings) error
}

var _ ReadWriter = (*Service)(nil)

type Service struct {
	filePath string
	mu       sync.RWMutex
	cache    *settingssvc.Settings
}

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

	s.cache = &candidate

	return nil
}

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
