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
	"strings"
	"sync"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
	"gorm.io/gorm"
)

// ACLMode: ACL 동작 모드 (화이트리스트/블랙리스트).
type ACLMode string

const (
	ACLModeWhitelist ACLMode = "whitelist"
	ACLModeBlacklist ACLMode = "blacklist"
)

// ParseACLMode: 문자열을 ACLMode로 파싱한다. 유효하지 않으면 whitelist로 폴백.
func ParseACLMode(s string) ACLMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(ACLModeBlacklist):
		return ACLModeBlacklist
	default:
		return ACLModeWhitelist
	}
}

const (
	// Valkey 캐시 키.
	aclSettingsKey       = "acl:settings"
	aclModeKey           = "acl:mode"
	aclWhitelistRoomsKey = "acl:rooms:whitelist"
	aclBlacklistRoomsKey = "acl:rooms:blacklist"

	// DB 설정 키.
	dbKeyEnabled = "enabled"
	dbKeyMode    = "mode"

	// DB list_type 값.
	listTypeWhitelist = "whitelist"
	listTypeBlacklist = "blacklist"
)

// Settings: ACL 설정을 저장하기 위한 GORM 모델 (key-value 형태).
type Settings struct {
	ID    uint   `gorm:"primaryKey"`
	Key   string `gorm:"uniqueIndex;size:64"`
	Value string `gorm:"type:text"`
}

// TableName: ACL 설정 테이블의 이름을 반환한다. ("acl_settings").
func (Settings) TableName() string {
	return "acl_settings"
}

// Room: ACL 목록에 포함된 방을 저장하기 위한 GORM 모델.
type Room struct {
	ID       uint   `gorm:"primaryKey"`
	RoomID   string `gorm:"uniqueIndex:idx_room_list;size:64"`
	ListType string `gorm:"uniqueIndex:idx_room_list;size:16;default:whitelist"`
}

// TableName: ACL 방 목록 테이블의 이름을 반환한다. ("acl_rooms").
func (Room) TableName() string {
	return "acl_rooms"
}

// Service: 접근 제어 목록(ACL)을 관리하는 서비스
// PostgreSQL을 영구 저장소로 사용하고, 성능을 위해 인메모리 및 Valkey 캐시를 활용한다.
type Service struct {
	db     *gorm.DB
	cache  cache.Client
	logger *slog.Logger

	// 메모리 캐시 (빠른 조회용)
	mu             sync.RWMutex
	enabled        bool
	mode           ACLMode
	whitelistRooms map[string]struct{}
	blacklistRooms map[string]struct{}
}

// IsReady: ACL 서비스의 필수 의존성이 초기화되었는지 확인합니다.
func (s *Service) IsReady() bool {
	if s == nil {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.db != nil && s.cache != nil && s.logger != nil &&
		s.whitelistRooms != nil && s.blacklistRooms != nil
}

// NewACLService ACL 서비스 생성 및 초기화.
func NewACLService(
	ctx context.Context,
	postgres database.Client,
	cacheSvc cache.Client,
	logger *slog.Logger,
	defaultEnabled bool,
	defaultMode ACLMode,
	defaultRooms []string,
) (*Service, error) {
	db := postgres.GetGormDB()

	svc := &Service{
		db:             db,
		cache:          cacheSvc,
		logger:         logger,
		enabled:        defaultEnabled,
		mode:           defaultMode,
		whitelistRooms: make(map[string]struct{}),
		blacklistRooms: make(map[string]struct{}),
	}

	// 시작 시 로드 (PostgreSQL → 메모리/Valkey)
	if err := svc.loadFromDatabase(ctx, defaultEnabled, defaultMode, defaultRooms); err != nil {
		logger.Warn("Failed to load ACL from database, using defaults", slog.Any("error", err))

		svc.enabled = defaultEnabled
		svc.mode = defaultMode
		// 기존 기본 방 목록을 현재 모드의 목록에 추가
		targetRooms := svc.activeRoomsMap()

		for _, r := range defaultRooms {
			targetRooms[r] = struct{}{}
		}
	}

	logger.Info("ACL service initialized",
		slog.Bool("enabled", svc.enabled),
		slog.String("mode", string(svc.mode)),
		slog.Int("whitelist_rooms", len(svc.whitelistRooms)),
		slog.Int("blacklist_rooms", len(svc.blacklistRooms)),
	)

	return svc, nil
}

// activeRoomsMap: 현재 활성 모드의 방 목록 맵을 반환한다 (잠금 없음, 호출자가 관리).
func (s *Service) activeRoomsMap() map[string]struct{} {
	if s.mode == ACLModeBlacklist {
		return s.blacklistRooms
	}

	return s.whitelistRooms
}

// loadFromDatabase PostgreSQL에서 ACL 설정 로드.
func (s *Service) loadFromDatabase(ctx context.Context, defaultEnabled bool, defaultMode ACLMode, defaultRooms []string) error {
	// 1. ACL enabled 상태 로드
	var settings Settings

	result := s.db.Where("key = ?", dbKeyEnabled).First(&settings)
	isFirstInit := stdErrors.Is(result.Error, gorm.ErrRecordNotFound)

	if isFirstInit {
		s.enabled = defaultEnabled
		s.db.Create(&Settings{Key: dbKeyEnabled, Value: fmt.Sprintf("%t", defaultEnabled)})
	} else if result.Error != nil {
		return fmt.Errorf("failed to load ACL enabled setting: %w", result.Error)
	} else {
		s.enabled = settings.Value == "true"
	}

	// 2. ACL mode 로드
	var modeSetting Settings

	modeResult := s.db.Where("key = ?", dbKeyMode).First(&modeSetting)
	modeFirstInit := stdErrors.Is(modeResult.Error, gorm.ErrRecordNotFound)

	if modeFirstInit {
		s.mode = defaultMode
		s.db.Create(&Settings{Key: dbKeyMode, Value: string(defaultMode)})
	} else if modeResult.Error != nil {
		return fmt.Errorf("failed to load ACL mode setting: %w", modeResult.Error)
	} else {
		s.mode = ParseACLMode(modeSetting.Value)
	}

	// 3. Rooms 로드 (list_type별 분리)
	var rooms []Room
	if err := s.db.Find(&rooms).Error; err != nil {
		return fmt.Errorf("failed to load ACL rooms: %w", err)
	}

	s.mu.Lock()
	s.whitelistRooms = make(map[string]struct{})
	s.blacklistRooms = make(map[string]struct{})

	if isFirstInit && len(rooms) == 0 {
		// 첫 초기화: 기본 방 목록을 현재 모드의 목록으로 저장
		targetRooms := s.activeRoomsMap()
		lt := string(s.mode)

		for _, r := range defaultRooms {
			targetRooms[r] = struct{}{}
			s.db.Create(&Room{RoomID: r, ListType: lt})
		}
	} else {
		// 기존 DB 상태 로드 (list_type별 분리)
		for _, r := range rooms {
			switch r.ListType {
			case listTypeBlacklist:
				s.blacklistRooms[r.RoomID] = struct{}{}
			default:
				// list_type이 비어있거나 "whitelist"인 경우 (하위호환)
				s.whitelistRooms[r.RoomID] = struct{}{}
			}
		}
	}

	s.mu.Unlock()

	s.syncSettingsToValkey(ctx)
	s.syncModeToValkey(ctx)
	s.syncRoomsToValkey(ctx, ACLModeWhitelist)
	s.syncRoomsToValkey(ctx, ACLModeBlacklist)

	return nil
}

// syncRoomsToValkey: 메모리 → Valkey SET 동기화 (전체 교체).
func (s *Service) syncRoomsToValkey(ctx context.Context, mode ACLMode) {
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

	_ = s.cache.Del(ctx, key)

	if len(rooms) > 0 {
		_, _ = s.cache.SAdd(ctx, key, rooms)
	}
}

// syncSettingsToValkey: ACL enabled 상태를 Valkey에 동기화합니다.
func (s *Service) syncSettingsToValkey(ctx context.Context) {
	s.mu.RLock()

	enabled := s.enabled
	s.mu.RUnlock()

	_ = s.cache.Set(ctx, aclSettingsKey, fmt.Sprintf("%t", enabled), 0)
}

// syncModeToValkey: ACL mode를 Valkey에 동기화합니다.
func (s *Service) syncModeToValkey(ctx context.Context) {
	s.mu.RLock()

	mode := s.mode
	s.mu.RUnlock()

	_ = s.cache.Set(ctx, aclModeKey, string(mode), 0)
}

// IsRoomAllowed 방 접근 허용 여부 확인 (빠른 메모리 조회).
func (s *Service) IsRoomAllowed(roomName, chatID string) bool {
	roomName = stringutil.TrimSpace(roomName)
	chatID = stringutil.TrimSpace(chatID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.enabled {
		return true
	}

	switch s.mode {
	case ACLModeBlacklist:
		// 블랙리스트: 목록에 있으면 차단, 없으면 허용
		return !isInRoomSet(s.blacklistRooms, roomName, chatID)
	default:
		// 화이트리스트: 목록에 있으면 허용, 없으면 차단
		return isInRoomSet(s.whitelistRooms, roomName, chatID)
	}
}

// isInRoomSet: 주어진 방 목록에 roomName 또는 chatID가 존재하는지 확인한다.
func isInRoomSet(rooms map[string]struct{}, roomName, chatID string) bool {
	if chatID != "" {
		if _, ok := rooms[chatID]; ok {
			return true
		}
	}

	if roomName != "" {
		if _, ok := rooms[roomName]; ok {
			return true
		}
	}

	return false
}

// GetACLStatus 현재 ACL 상태 반환.
func (s *Service) GetACLStatus() (enabled bool, mode ACLMode, rooms []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	activeRooms := s.activeRoomsMap()

	rooms = make([]string, 0, len(activeRooms))
	for r := range activeRooms {
		rooms = append(rooms, r)
	}

	return s.enabled, s.mode, rooms
}

// SetEnabled ACL 활성화/비활성화.
func (s *Service) SetEnabled(ctx context.Context, enabled bool) error {
	s.mu.Lock()
	s.enabled = enabled
	s.mu.Unlock()

	// PostgreSQL 저장
	result := s.db.Where("key = ?", dbKeyEnabled).Assign(Settings{Value: fmt.Sprintf("%t", enabled)}).FirstOrCreate(&Settings{Key: dbKeyEnabled})
	if result.Error != nil {
		return fmt.Errorf("failed to save ACL enabled setting: %w", result.Error)
	}

	s.syncSettingsToValkey(ctx)

	s.logger.Info("ACL enabled status updated",
		slog.Bool("enabled", enabled),
	)

	return nil
}

// SetMode ACL 모드 변경 (whitelist ↔ blacklist).
func (s *Service) SetMode(ctx context.Context, mode ACLMode) error {
	s.mu.Lock()
	s.mode = mode
	s.mu.Unlock()

	// PostgreSQL 저장
	result := s.db.Where("key = ?", dbKeyMode).Assign(Settings{Value: string(mode)}).FirstOrCreate(&Settings{Key: dbKeyMode})
	if result.Error != nil {
		return fmt.Errorf("failed to save ACL mode setting: %w", result.Error)
	}

	s.syncModeToValkey(ctx)

	s.logger.Info("ACL mode updated",
		slog.String("mode", string(mode)),
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
	targetRooms := s.activeRoomsMap()
	lt := string(s.mode)

	if _, exists := targetRooms[room]; exists {
		s.mu.Unlock()
		return false, nil // 이미 존재
	}

	targetRooms[room] = struct{}{}
	s.mu.Unlock()

	// PostgreSQL 저장
	result := s.db.Create(&Room{RoomID: room, ListType: lt})
	if result.Error != nil {
		s.mu.Lock()
		delete(s.activeRoomsMap(), room)
		s.mu.Unlock()

		return false, fmt.Errorf("failed to add room to database: %w", result.Error)
	}

	valkeyKey := s.valkeyKeyForMode(s.currentMode())

	_, _ = s.cache.SAdd(ctx, valkeyKey, []string{room})

	s.logger.Info("Room added to ACL list",
		slog.String("room", room),
		slog.String("list_type", lt),
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
	targetRooms := s.activeRoomsMap()
	lt := string(s.mode)

	if _, exists := targetRooms[room]; !exists {
		s.mu.Unlock()
		return false, nil // 존재하지 않음
	}

	delete(targetRooms, room)
	s.mu.Unlock()

	// PostgreSQL 삭제
	result := s.db.Where("room_id = ? AND list_type = ?", room, lt).Delete(&Room{})
	if result.Error != nil {
		s.mu.Lock()
		s.activeRoomsMap()[room] = struct{}{}
		s.mu.Unlock()

		return false, fmt.Errorf("failed to remove room from database: %w", result.Error)
	}

	valkeyKey := s.valkeyKeyForMode(s.currentMode())

	_, _ = s.cache.SRem(ctx, valkeyKey, []string{room})

	s.logger.Info("Room removed from ACL list",
		slog.String("room", room),
		slog.String("list_type", lt),
	)

	return true, nil
}

// currentMode: 현재 모드를 thread-safe하게 반환한다.
func (s *Service) currentMode() ACLMode {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.mode
}

// valkeyKeyForMode: 모드에 대응하는 Valkey SET 키를 반환한다.
func (s *Service) valkeyKeyForMode(mode ACLMode) string {
	if mode == ACLModeBlacklist {
		return aclBlacklistRoomsKey
	}

	return aclWhitelistRoomsKey
}
