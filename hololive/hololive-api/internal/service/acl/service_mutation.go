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

// SetEnabled ACL 활성화/비활성화.
// 같은 값이면 PG에 쓰지 않고 추종 인스턴스만 재동기화한다 — 앞선 요청이 ErrACLPropagation으로
// 끝났을 때 같은 요청 재시도로 수렴시키기 위해서다. 추종 실패는 ErrACLPropagation으로 돌려준다.
func (s *Service) SetEnabled(ctx context.Context, enabled bool) error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	s.mu.RLock()

	current := s.enabled
	s.mu.RUnlock()

	if current == enabled {
		return s.notifyChange(ctx)
	}

	if err := s.store.UpsertSetting(ctx, dbKeyEnabled, fmt.Sprintf("%t", enabled)); err != nil {
		return fmt.Errorf("failed to save ACL enabled setting: %w", err)
	}

	s.mu.Lock()

	s.enabled = enabled
	s.mu.Unlock()

	s.logger.Info("ACL enabled status updated",
		slog.Bool("enabled", enabled),
	)

	return s.notifyChange(ctx)
}

// SetMode ACL 모드 변경 (whitelist ↔ blacklist). 같은 값 재요청과 추종 실패는 SetEnabled와 같다.
func (s *Service) SetMode(ctx context.Context, mode ACLMode) error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	normalizedMode, err := normalizeACLModeStrict(mode)
	if err != nil {
		return fmt.Errorf("normalize ACL mode strict: %w", err)
	}

	s.mu.RLock()

	current := s.mode
	s.mu.RUnlock()

	if current == normalizedMode {
		return s.notifyChange(ctx)
	}

	if err := s.store.UpsertSetting(ctx, dbKeyMode, string(normalizedMode)); err != nil {
		return fmt.Errorf("failed to save ACL mode setting: %w", err)
	}

	s.mu.Lock()

	s.mode = normalizedMode
	s.mu.Unlock()

	s.logger.Info("ACL mode updated",
		slog.String("mode", string(normalizedMode)),
	)

	return s.notifyChange(ctx)
}

// AddRoom 현재 활성 모드의 목록에 방 추가. 새 등록값은 chatID만 받는다(ErrInvalidRoomChatID).
// 이미 있으면 PG에 쓰지 않고 추종 인스턴스만 재동기화한 뒤 false를 돌려준다(재시도 수렴 경로).
// 추종 실패는 added 값과 함께 ErrACLPropagation으로 돌려준다 — added=true면 PG에는 이미 커밋됐다.
func (s *Service) AddRoom(ctx context.Context, room string) (bool, error) {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	room = stringutil.TrimSpace(room)
	if room == "" {
		return false, nil
	}

	if err := validateRoomChatID(room); err != nil {
		return false, err
	}

	s.mu.Lock()

	mode := s.mode
	targetRooms := s.roomsMapForMode(mode)
	listType := string(mode)

	if _, exists := targetRooms[room]; exists {
		s.mu.Unlock()

		return false, s.notifyChange(ctx)
	}

	s.mu.Unlock()

	if err := s.store.CreateRoom(ctx, room, listType); err != nil {
		return false, fmt.Errorf("failed to add room to database: %w", err)
	}

	s.mu.Lock()

	s.roomsMapForMode(mode)[room] = struct{}{}
	s.mu.Unlock()

	s.logger.Info("Room added to ACL list",
		slog.String("room", room),
		slog.String("list_type", listType),
	)

	return true, s.notifyChange(ctx)
}

// RemoveRoom 현재 활성 모드의 목록에서 방 제거. 이미 저장된 비-chatID 값도 치울 수 있어야 하므로 형식은 보지 않는다.
// 없으면 PG에 쓰지 않고 추종 인스턴스만 재동기화한 뒤 false를 돌려준다. 추종 실패는 AddRoom과 같다.
func (s *Service) RemoveRoom(ctx context.Context, room string) (bool, error) {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	room = stringutil.TrimSpace(room)
	if room == "" {
		return false, nil
	}

	s.mu.Lock()

	mode := s.mode
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
			return false, s.notifyChange(ctx)
		}
	}

	if err := s.store.DeleteRoom(ctx, room, listType); err != nil {
		return false, fmt.Errorf("failed to remove room from database: %w", err)
	}

	s.mu.Lock()
	delete(s.roomsMapForMode(mode), room)
	s.mu.Unlock()

	s.logger.Info("Room removed from ACL list",
		slog.String("room", room),
		slog.String("list_type", listType),
	)

	return true, s.notifyChange(ctx)
}
