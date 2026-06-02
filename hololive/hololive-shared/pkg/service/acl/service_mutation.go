package acl

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/park285/shared-go/pkg/stringutil"
)

// GetACLStatus 현재 ACL 상태 반환.
func (s *Service) GetACLStatus() (enabled bool, mode ACLMode, rooms []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	activeRooms := s.activeRoomsMap()
	rooms = make([]string, 0, len(activeRooms))
	for room := range activeRooms {
		rooms = append(rooms, room)
	}

	sort.Strings(rooms)

	return s.enabled, s.mode, rooms
}

// SetEnabled ACL 활성화/비활성화.
func (s *Service) SetEnabled(ctx context.Context, enabled bool) error {
	s.mu.RLock()
	current := s.enabled
	s.mu.RUnlock()

	if current == enabled {
		return nil
	}

	if err := s.store.UpsertSetting(ctx, dbKeyEnabled, fmt.Sprintf("%t", enabled)); err != nil {
		return fmt.Errorf("failed to save ACL enabled setting: %w", err)
	}

	s.mu.Lock()
	s.enabled = enabled
	s.mu.Unlock()

	if err := s.syncSettingsToValkey(ctx); err != nil {
		rollbackErr := s.rollbackEnabledState(ctx, current)
		return stdErrors.Join(
			fmt.Errorf("sync acl settings to cache: %w", err),
			wrapACLRollbackError("rollback acl enabled", rollbackErr),
		)
	}

	s.logger.Info("ACL enabled status updated",
		slog.Bool("enabled", enabled),
	)

	return nil
}

// SetMode ACL 모드 변경 (whitelist ↔ blacklist).
func (s *Service) SetMode(ctx context.Context, mode ACLMode) error {
	normalizedMode, err := normalizeACLModeStrict(mode)
	if err != nil {
		return err
	}

	s.mu.RLock()
	current := s.mode
	s.mu.RUnlock()

	if current == normalizedMode {
		return nil
	}

	if err := s.store.UpsertSetting(ctx, dbKeyMode, string(normalizedMode)); err != nil {
		return fmt.Errorf("failed to save ACL mode setting: %w", err)
	}

	s.mu.Lock()
	s.mode = mode
	s.mu.Unlock()

	if err := s.syncModeToValkey(ctx); err != nil {
		rollbackErr := s.rollbackModeState(ctx, current)
		return stdErrors.Join(
			fmt.Errorf("sync acl mode to cache: %w", err),
			wrapACLRollbackError("rollback acl mode", rollbackErr),
		)
	}

	s.logger.Info("ACL mode updated",
		slog.String("mode", string(normalizedMode)),
	)

	return nil
}

// AddRoom 현재 활성 모드의 목록에 방 추가.
func (s *Service) AddRoom(ctx context.Context, room string) (bool, error) {
	room = stringutil.TrimSpace(room)
	if room == "" {
		return false, nil
	}

	s.mu.Lock()
	mode := s.mode
	targetRooms := s.roomsMapForMode(mode)
	listType := string(mode)

	if _, exists := targetRooms[room]; exists {
		s.mu.Unlock()
		return false, nil
	}

	targetRooms[room] = struct{}{}
	s.mu.Unlock()

	if err := s.store.CreateRoom(ctx, room, listType); err != nil {
		s.mu.Lock()
		delete(s.roomsMapForMode(mode), room)
		s.mu.Unlock()

		return false, fmt.Errorf("failed to add room to database: %w", err)
	}

	if _, err := s.cache.SAdd(ctx, s.valkeyKeyForMode(mode), []string{room}); err != nil {
		rollbackErr := s.rollbackAddedRoom(ctx, mode, room, listType)
		return false, stdErrors.Join(
			fmt.Errorf("sync acl room add to cache: %w", err),
			wrapACLRollbackError("rollback acl room add", rollbackErr),
		)
	}

	s.logger.Info("Room added to ACL list",
		slog.String("room", room),
		slog.String("list_type", listType),
	)

	return true, nil
}

// RemoveRoom 현재 활성 모드의 목록에서 방 제거.
func (s *Service) RemoveRoom(ctx context.Context, room string) (bool, error) {
	room = stringutil.TrimSpace(room)
	if room == "" {
		return false, nil
	}

	s.mu.Lock()
	mode := s.mode
	targetRooms := s.roomsMapForMode(mode)
	listType := string(mode)

	if _, exists := targetRooms[room]; !exists {
		s.mu.Unlock()
		return false, nil
	}

	delete(targetRooms, room)
	s.mu.Unlock()

	if err := s.store.DeleteRoom(ctx, room, listType); err != nil {
		s.mu.Lock()
		s.roomsMapForMode(mode)[room] = struct{}{}
		s.mu.Unlock()

		return false, fmt.Errorf("failed to remove room from database: %w", err)
	}

	if _, err := s.cache.SRem(ctx, s.valkeyKeyForMode(mode), []string{room}); err != nil {
		rollbackErr := s.rollbackRemovedRoom(ctx, mode, room, listType)
		return false, stdErrors.Join(
			fmt.Errorf("sync acl room removal to cache: %w", err),
			wrapACLRollbackError("rollback acl room removal", rollbackErr),
		)
	}

	s.logger.Info("Room removed from ACL list",
		slog.String("room", room),
		slog.String("list_type", listType),
	)

	return true, nil
}

func (s *Service) valkeyKeyForMode(mode ACLMode) string {
	if mode == ACLModeBlacklist {
		return aclBlacklistRoomsKey
	}

	return aclWhitelistRoomsKey
}

func (s *Service) rollbackEnabledState(ctx context.Context, enabled bool) error {
	if err := s.store.UpsertSetting(ctx, dbKeyEnabled, fmt.Sprintf("%t", enabled)); err != nil {
		return fmt.Errorf("restore enabled setting: %w", err)
	}

	s.mu.Lock()
	s.enabled = enabled
	s.mu.Unlock()

	return nil
}

func (s *Service) rollbackModeState(ctx context.Context, mode ACLMode) error {
	if err := s.store.UpsertSetting(ctx, dbKeyMode, string(mode)); err != nil {
		return fmt.Errorf("restore mode setting: %w", err)
	}

	s.mu.Lock()
	s.mode = mode
	s.mu.Unlock()

	return nil
}

func (s *Service) rollbackAddedRoom(ctx context.Context, mode ACLMode, room, listType string) error {
	if err := s.store.DeleteRoom(ctx, room, listType); err != nil {
		return fmt.Errorf("delete added room from database: %w", err)
	}

	s.mu.Lock()
	delete(s.roomsMapForMode(mode), room)
	s.mu.Unlock()

	return nil
}

func (s *Service) rollbackRemovedRoom(ctx context.Context, mode ACLMode, room, listType string) error {
	if err := s.store.CreateRoom(ctx, room, listType); err != nil {
		return fmt.Errorf("recreate removed room in database: %w", err)
	}

	s.mu.Lock()
	s.roomsMapForMode(mode)[room] = struct{}{}
	s.mu.Unlock()

	return nil
}

func wrapACLRollbackError(action string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", action, err)
}
