package acl

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
)

func (s *Service) GetACLStatus() (enabled bool, mode ACLMode, rooms []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	activeRooms := s.activeRoomsMap()

	rooms = make([]string, 0, len(activeRooms))

	for room := range activeRooms {
		rooms = append(rooms, room)
	}

	slices.Sort(rooms)

	return s.enabled, s.mode, rooms
}

func (s *Service) SetEnabled(ctx context.Context, enabled bool) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	s.mu.RLock()

	current := s.enabled
	s.mu.RUnlock()

	if current == enabled {
		return nil
	}

	if err := s.store.UpsertSetting(ctx, dbKeyEnabled, fmt.Sprintf("%t", enabled)); err != nil {
		return fmt.Errorf("save ACL enabled setting: %w", err)
	}

	s.mu.Lock()

	s.enabled = enabled
	s.mu.Unlock()

	s.logger.Info("ACL enabled status updated",
		slog.Bool("enabled", enabled),
	)

	return nil
}

func (s *Service) SetMode(ctx context.Context, mode ACLMode) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	normalizedMode, err := normalizeACLModeStrict(mode)
	if err != nil {
		return fmt.Errorf("normalize ACL mode strict: %w", err)
	}

	s.mu.RLock()

	current := s.mode
	s.mu.RUnlock()

	if current == normalizedMode {
		return nil
	}

	if err := s.store.UpsertSetting(ctx, dbKeyMode, string(normalizedMode)); err != nil {
		return fmt.Errorf("save ACL mode setting: %w", err)
	}

	s.mu.Lock()

	s.mode = normalizedMode
	s.mu.Unlock()

	s.logger.Info("ACL mode updated",
		slog.String("mode", string(normalizedMode)),
	)

	return nil
}

func (s *Service) AddRoom(ctx context.Context, expectedMode ACLMode, room string) (bool, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	room = strings.TrimSpace(room)
	if err := validateRoomChatID(room); err != nil {
		return false, err
	}

	s.mu.Lock()

	mode := s.mode
	if mode != expectedMode {
		s.mu.Unlock()

		return false, ErrACLModeChanged
	}

	targetRooms := s.roomsMapForMode(mode)
	listType := string(mode)

	if _, exists := targetRooms[room]; exists {
		s.mu.Unlock()

		return false, nil
	}

	s.mu.Unlock()

	created, err := s.store.CreateRoom(ctx, room, listType)
	if err != nil {
		return false, fmt.Errorf("add room to database: %w", err)
	}

	s.mu.Lock()

	s.roomsMapForMode(mode)[room] = struct{}{}
	s.mu.Unlock()

	if !created {
		return false, nil
	}

	s.logger.Info("Room added to ACL list",
		slog.String("room", room),
		slog.String("list_type", listType),
	)

	return true, nil
}

func (s *Service) RemoveRoom(ctx context.Context, expectedMode ACLMode, room string) (bool, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	room = strings.TrimSpace(room)
	if err := validateRoomChatID(room); err != nil {
		return false, err
	}

	s.mu.RLock()

	mode := s.mode
	s.mu.RUnlock()

	if mode != expectedMode {
		return false, ErrACLModeChanged
	}

	listType := string(mode)

	deleted, err := s.store.DeleteRoom(ctx, room, listType)
	if err != nil {
		return false, fmt.Errorf("remove room from database: %w", err)
	}

	s.mu.Lock()

	targetRooms := s.roomsMapForMode(mode)
	_, inMemory := targetRooms[room]
	delete(targetRooms, room)
	s.mu.Unlock()

	if !inMemory && !deleted {
		return false, nil
	}

	s.logger.Info("Room removed from ACL list",
		slog.String("room", room),
		slog.String("list_type", listType),
	)

	return true, nil
}
