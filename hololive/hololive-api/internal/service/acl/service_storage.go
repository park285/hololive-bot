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
	"fmt"

	"github.com/park285/shared-go/v2/pkg/stringutil"
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
		return false, fmt.Errorf("failed to load ACL enabled setting: %w", err)
	}

	isFirstInit := !found

	switch {
	case isFirstInit:
		s.enabled = defaultEnabled
		if err := s.store.CreateSetting(ctx, dbKeyEnabled, fmt.Sprintf("%t", defaultEnabled)); err != nil {
			return false, fmt.Errorf("failed to initialize ACL enabled setting: %w", err)
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
		return fmt.Errorf("failed to load ACL mode setting: %w", err)
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
		return fmt.Errorf("failed to initialize ACL mode setting: %w", err)
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
		return nil, fmt.Errorf("failed to load ACL rooms: %w", err)
	}

	if err := validateRoomListTypes(rooms); err != nil {
		return nil, fmt.Errorf("validate room list types: %w", err)
	}

	return rooms, nil
}

func validateRoomListTypes(rooms []Room) error {
	for _, room := range rooms {
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
	roomID := stringutil.TrimSpace(room.RoomID)
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
		if err := s.store.CreateRoom(ctx, room, listType); err != nil {
			return fmt.Errorf("failed to initialize ACL room %q: %w", room, err)
		}

		targetRooms[room] = struct{}{}
	}

	return nil
}
