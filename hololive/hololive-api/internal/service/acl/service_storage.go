package acl

import (
	"context"
	"fmt"
	"strings"
)

func (s *Service) loadFromDatabase(ctx context.Context, defaultEnabled bool, defaultMode ACLMode, defaultRooms []string) error {
	isFirstInit, loadErr := s.loadEnabledSetting(ctx, defaultEnabled)
	if loadErr != nil {
		return fmt.Errorf("load enabled setting: %w", loadErr)
	}

	if loadErr = s.loadModeSetting(ctx, defaultMode); loadErr != nil {
		return fmt.Errorf("load mode setting: %w", loadErr)
	}

	rooms, loadErr := s.loadRoomsFromDatabase(ctx)
	if loadErr != nil {
		return fmt.Errorf("load rooms: %w", loadErr)
	}

	s.mu.Lock()
	s.resetRoomMaps()
	s.populateRoomsFromRecords(rooms)
	s.mu.Unlock()

	if isFirstInit && len(rooms) == 0 {
		if initErr := s.initializeDefaultRooms(ctx, defaultRooms); initErr != nil {
			return fmt.Errorf("initialize default rooms: %w", initErr)
		}
	}

	return nil
}

func (s *Service) loadEnabledSetting(ctx context.Context, defaultEnabled bool) (bool, error) {
	value, found, err := s.store.GetSetting(ctx, dbKeyEnabled)
	if err != nil {
		return false, fmt.Errorf("load ACL enabled setting: %w", err)
	}

	isFirstInit := !found

	switch {
	case isFirstInit:
		s.enabled = defaultEnabled
		if err := s.store.CreateSetting(ctx, dbKeyEnabled, fmt.Sprintf("%t", defaultEnabled)); err != nil {
			return false, fmt.Errorf("initialize ACL enabled setting: %w", err)
		}
	default:
		enabled, parseErr := parseACLEnabledStrict(value)
		if parseErr != nil {
			return false, fmt.Errorf("invalid ACL enabled setting: %w", parseErr)
		}

		s.enabled = enabled
	}

	return isFirstInit, nil
}

func (s *Service) loadModeSetting(ctx context.Context, defaultMode ACLMode) error {
	value, found, err := s.store.GetSetting(ctx, dbKeyMode)
	if err != nil {
		return fmt.Errorf("load ACL mode setting: %w", err)
	}

	if !found {
		if err := s.initializeModeSetting(ctx, defaultMode); err != nil {
			return fmt.Errorf("initialize mode setting: %w", err)
		}

		return nil
	}

	if err := s.applyModeSetting(value); err != nil {
		return fmt.Errorf("apply mode setting: %w", err)
	}

	return nil
}

func (s *Service) initializeModeSetting(ctx context.Context, defaultMode ACLMode) error {
	normalizedMode, err := normalizeACLModeStrict(defaultMode)
	if err != nil {
		return fmt.Errorf("normalize ACL mode strict: %w", err)
	}

	s.mode = normalizedMode
	if err := s.store.CreateSetting(ctx, dbKeyMode, string(normalizedMode)); err != nil {
		return fmt.Errorf("initialize ACL mode setting: %w", err)
	}

	return nil
}

func (s *Service) applyModeSetting(value string) error {
	mode, err := parseACLModeStrict(value)
	if err != nil {
		return fmt.Errorf("parse ACL mode strict: %w", err)
	}

	s.mode = mode

	return nil
}

func (s *Service) loadRoomsFromDatabase(ctx context.Context) ([]Room, error) {
	rooms, err := s.store.ListRooms(ctx)
	if err != nil {
		return nil, fmt.Errorf("load ACL rooms: %w", err)
	}

	if err := validateStoredRooms(rooms); err != nil {
		return nil, fmt.Errorf("validate stored rooms: %w", err)
	}

	return rooms, nil
}

func validateStoredRooms(rooms []Room) error {
	for _, room := range rooms {
		if err := validateRoomChatID(room.RoomID); err != nil {
			return fmt.Errorf("invalid stored ACL room (list_type %q): %w", room.ListType, err)
		}

		switch room.ListType {
		case listTypeWhitelist, listTypeBlacklist:
		default:
			return fmt.Errorf("invalid ACL room %q list_type: %q", room.RoomID, room.ListType)
		}
	}

	return nil
}

func (s *Service) resetRoomMaps() {
	s.whitelistRooms = make(map[string]struct{})
	s.blacklistRooms = make(map[string]struct{})
}

func (s *Service) populateRoomsFromRecords(rooms []Room) {
	for _, room := range rooms {
		s.populateRoomFromRecord(room)
	}
}

func (s *Service) populateRoomFromRecord(room Room) {
	roomID := strings.TrimSpace(room.RoomID)
	if roomID == "" {
		return
	}

	switch room.ListType {
	case listTypeBlacklist:
		s.blacklistRooms[roomID] = struct{}{}
	case listTypeWhitelist:
		s.whitelistRooms[roomID] = struct{}{}
	}
}

func (s *Service) initializeDefaultRooms(ctx context.Context, defaultRooms []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	targetRooms := s.activeRoomsMap()
	listType := string(s.mode)

	for _, room := range normalizeRoomList(defaultRooms) {
		if _, err := s.store.CreateRoom(ctx, room, listType); err != nil {
			return fmt.Errorf("initialize ACL room %q: %w", room, err)
		}

		targetRooms[room] = struct{}{}
	}

	return nil
}
