package acl

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/park285/shared-go/v2/pkg/stringutil"
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

	slices.Sort(rooms)

	return s.enabled, s.mode, rooms
}

// SetEnabled ACL 활성화/비활성화. 같은 값이면 PG에 쓰지 않는다.
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

// SetMode ACL 모드 변경 (whitelist ↔ blacklist). 같은 값이면 PG에 쓰지 않는다.
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

// AddRoom은 호출자가 본 모드(expectedMode)의 목록에 방을 추가한다. 새 등록값은 chatID만 받는다(ErrInvalidRoomChatID).
// 현재 모드가 expectedMode와 다르면 PG에 쓰지 않고 ErrACLModeChanged를 돌려준다. 화면을 읽은 뒤 모드가 바뀌면
// 허용하려던 방이 차단 목록에 들어가는 반대 효과가 성공으로 보고되기 때문이다. SetMode도 mutationMu를 잡으므로
// 비교와 저장 사이에 모드가 바뀌지 않는다. 이미 있으면 PG에 쓰지 않고 false를 돌려준다.
func (s *Service) AddRoom(ctx context.Context, expectedMode ACLMode, room string) (bool, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	room = stringutil.TrimSpace(room)
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

	if err := s.store.CreateRoom(ctx, room, listType); err != nil {
		return false, fmt.Errorf("add room to database: %w", err)
	}

	s.mu.Lock()

	s.roomsMapForMode(mode)[room] = struct{}{}
	s.mu.Unlock()

	s.logger.Info("Room added to ACL list",
		slog.String("room", room),
		slog.String("list_type", listType),
	)

	return true, nil
}

// RemoveRoom은 호출자가 본 모드(expectedMode)의 목록에서 방을 제거한다. 저장 행은 migration 346 이후 모두 chatID이므로
// AddRoom과 같은 형식을 요구한다. 현재 모드가 다르면 PG에 쓰지 않고 ErrACLModeChanged를 돌려준다.
// 없으면 PG에 쓰지 않고 false를 돌려준다.
func (s *Service) RemoveRoom(ctx context.Context, expectedMode ACLMode, room string) (bool, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	room = stringutil.TrimSpace(room)
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

	_, exists := targetRooms[room]
	s.mu.Unlock()

	// 과거 경합으로 메모리에서만 사라진 방도 PG의 권한을 철회할 수 있어야 한다.
	if !exists {
		rooms, err := s.loadRoomsFromDatabase(ctx)
		if err != nil {
			return false, fmt.Errorf("check persisted ACL room: %w", err)
		}

		exists = slices.ContainsFunc(rooms, func(stored Room) bool {
			return stored.RoomID == room && stored.ListType == listType
		})
		if !exists {
			return false, nil
		}
	}

	if err := s.store.DeleteRoom(ctx, room, listType); err != nil {
		return false, fmt.Errorf("remove room from database: %w", err)
	}

	s.mu.Lock()
	delete(s.roomsMapForMode(mode), room)
	s.mu.Unlock()

	s.logger.Info("Room removed from ACL list",
		slog.String("room", room),
		slog.String("list_type", listType),
	)

	return true, nil
}
