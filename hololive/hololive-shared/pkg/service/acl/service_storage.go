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

package acl

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
	"gorm.io/gorm"
)

func (s *Service) loadFromDatabase(ctx context.Context, defaultEnabled bool, defaultMode ACLMode, defaultRooms []string) error {
	isFirstInit, loadErr := s.loadEnabledSetting(defaultEnabled)
	if loadErr != nil {
		return fmt.Errorf("load enabled setting: %w", loadErr)
	}

	if loadErr = s.loadModeSetting(defaultMode); loadErr != nil {
		return fmt.Errorf("load mode setting: %w", loadErr)
	}

	rooms, loadErr := s.loadRoomsFromDatabase()
	if loadErr != nil {
		return fmt.Errorf("load rooms: %w", loadErr)
	}

	s.mu.Lock()
	s.resetRoomMaps()
	s.populateRoomsFromRecords(rooms)
	s.mu.Unlock()

	if isFirstInit && len(rooms) == 0 {
		if initErr := s.initializeDefaultRooms(defaultRooms); initErr != nil {
			return fmt.Errorf("initialize default rooms: %w", initErr)
		}
	}

	s.syncLoadedACLToValkey(ctx)

	return nil
}

func (s *Service) syncLoadedACLToValkey(ctx context.Context) {
	if err := s.syncSettingsToValkey(ctx); err != nil {
		s.logger.Warn("Failed to sync ACL settings to cache", slog.Any("error", err))
	}

	if err := s.syncModeToValkey(ctx); err != nil {
		s.logger.Warn("Failed to sync ACL mode to cache", slog.Any("error", err))
	}

	if err := s.syncRoomsToValkey(ctx, ACLModeWhitelist); err != nil {
		s.logger.Warn("Failed to sync ACL rooms to cache", slog.String("mode", string(ACLModeWhitelist)), slog.Any("error", err))
	}

	if err := s.syncRoomsToValkey(ctx, ACLModeBlacklist); err != nil {
		s.logger.Warn("Failed to sync ACL rooms to cache", slog.String("mode", string(ACLModeBlacklist)), slog.Any("error", err))
	}
}

func (s *Service) loadEnabledSetting(defaultEnabled bool) (bool, error) {
	var settings Settings

	result := s.db.Where("key = ?", dbKeyEnabled).First(&settings)
	isFirstInit := stdErrors.Is(result.Error, gorm.ErrRecordNotFound)

	switch {
	case isFirstInit:
		s.enabled = defaultEnabled
		if err := s.db.Create(&Settings{Key: dbKeyEnabled, Value: fmt.Sprintf("%t", defaultEnabled)}).Error; err != nil {
			return false, fmt.Errorf("failed to initialize ACL enabled setting: %w", err)
		}
	case result.Error != nil:
		return false, fmt.Errorf("failed to load ACL enabled setting: %w", result.Error)
	default:
		s.enabled = settings.Value == "true"
	}

	return isFirstInit, nil
}

func (s *Service) loadModeSetting(defaultMode ACLMode) error {
	var modeSetting Settings

	modeResult := s.db.Where("key = ?", dbKeyMode).First(&modeSetting)
	modeFirstInit := stdErrors.Is(modeResult.Error, gorm.ErrRecordNotFound)

	switch {
	case modeFirstInit:
		return s.initializeModeSetting(defaultMode)
	case modeResult.Error != nil:
		return fmt.Errorf("failed to load ACL mode setting: %w", modeResult.Error)
	default:
		return s.applyModeSetting(modeSetting.Value)
	}
}

func (s *Service) initializeModeSetting(defaultMode ACLMode) error {
	normalizedMode, err := normalizeACLModeStrict(defaultMode)
	if err != nil {
		return err
	}

	s.mode = normalizedMode
	if err := s.db.Create(&Settings{Key: dbKeyMode, Value: string(normalizedMode)}).Error; err != nil {
		return fmt.Errorf("failed to initialize ACL mode setting: %w", err)
	}

	return nil
}

func (s *Service) applyModeSetting(value string) error {
	mode, err := parseACLModeStrict(value)
	if err != nil {
		return err
	}

	s.mode = mode
	return nil
}

func (s *Service) loadRoomsFromDatabase() ([]Room, error) {
	var rooms []Room
	if err := s.db.Find(&rooms).Error; err != nil {
		return nil, fmt.Errorf("failed to load ACL rooms: %w", err)
	}

	return rooms, nil
}

func (s *Service) resetRoomMaps() {
	s.whitelistRooms = make(map[string]struct{})
	s.blacklistRooms = make(map[string]struct{})
}

func (s *Service) populateRoomsFromRecords(rooms []Room) {
	for _, room := range rooms {
		roomID := stringutil.TrimSpace(room.RoomID)
		if roomID == "" {
			continue
		}

		if room.ListType == listTypeBlacklist {
			s.blacklistRooms[roomID] = struct{}{}
			continue
		}

		s.whitelistRooms[roomID] = struct{}{}
	}
}

func (s *Service) initializeDefaultRooms(defaultRooms []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	targetRooms := s.activeRoomsMap()
	listType := string(s.mode)

	for _, room := range normalizeRoomList(defaultRooms) {
		if err := s.db.Create(&Room{RoomID: room, ListType: listType}).Error; err != nil {
			return fmt.Errorf("failed to initialize ACL room %q: %w", room, err)
		}

		targetRooms[room] = struct{}{}
	}

	return nil
}

// syncRoomsToValkey: 메모리 → Valkey SET 동기화 (전체 교체).
func (s *Service) syncRoomsToValkey(ctx context.Context, mode ACLMode) error {
	s.mu.RLock()

	var (
		source map[string]struct{}
		key    string
	)

	if mode == ACLModeBlacklist {
		source = s.blacklistRooms
		key = aclBlacklistRoomsKey
	} else {
		source = s.whitelistRooms
		key = aclWhitelistRoomsKey
	}

	rooms := make([]string, 0, len(source))
	for r := range source {
		rooms = append(rooms, r)
	}

	s.mu.RUnlock()

	if err := s.syncRoomsToValkeyAtomic(ctx, key, rooms); err != nil {
		return fmt.Errorf("sync rooms to cache for mode %s: %w", mode, err)
	}

	return nil
}

// syncSettingsToValkey: ACL enabled 상태를 Valkey에 동기화합니다.
func (s *Service) syncSettingsToValkey(ctx context.Context) error {
	s.mu.RLock()

	enabled := s.enabled
	s.mu.RUnlock()

	if err := s.cache.Set(ctx, aclSettingsKey, fmt.Sprintf("%t", enabled), 0); err != nil {
		return fmt.Errorf("set %s: %w", aclSettingsKey, err)
	}

	return nil
}

// syncModeToValkey: ACL mode를 Valkey에 동기화합니다.
func (s *Service) syncModeToValkey(ctx context.Context) error {
	s.mu.RLock()

	mode := s.mode
	s.mu.RUnlock()

	if err := s.cache.Set(ctx, aclModeKey, string(mode), 0); err != nil {
		return fmt.Errorf("set %s: %w", aclModeKey, err)
	}

	return nil
}
