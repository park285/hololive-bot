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
	"log/slog"
	"sort"
	"sync"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/park285/hololive-bot/shared-go/pkg/stringutil"
	"gorm.io/gorm"
)

type ACLMode string

const (
	ACLModeWhitelist ACLMode = "whitelist"
	ACLModeBlacklist ACLMode = "blacklist"
)

func ParseACLMode(s string) ACLMode {
	switch stringutil.Normalize(s) {
	case string(ACLModeBlacklist):
		return ACLModeBlacklist
	default:
		return ACLModeWhitelist
	}
}

func normalizeACLModeStrict(mode ACLMode) (ACLMode, error) {
	switch mode {
	case ACLModeWhitelist, ACLModeBlacklist:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported acl mode: %q", mode)
	}
}

func parseACLModeStrict(s string) (ACLMode, error) {
	normalized := stringutil.Normalize(s)
	switch normalized {
	case string(ACLModeWhitelist):
		return ACLModeWhitelist, nil
	case string(ACLModeBlacklist):
		return ACLModeBlacklist, nil
	default:
		return "", fmt.Errorf("unsupported acl mode: %q", s)
	}
}

func normalizeRoomList(input []string) []string {
	seen := make(map[string]struct{}, len(input))
	rooms := make([]string, 0, len(input))

	for _, roomID := range input {
		roomID = stringutil.TrimSpace(roomID)
		if roomID == "" {
			continue
		}

		if _, ok := seen[roomID]; ok {
			continue
		}

		seen[roomID] = struct{}{}
		rooms = append(rooms, roomID)
	}

	sort.Strings(rooms)
	return rooms
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

type Settings struct {
	ID    uint   `gorm:"primaryKey"`
	Key   string `gorm:"uniqueIndex;size:64"`
	Value string `gorm:"type:text"`
}

func (Settings) TableName() string {
	return "acl_settings"
}

type Room struct {
	ID       uint   `gorm:"primaryKey"`
	RoomID   string `gorm:"uniqueIndex:idx_room_list;size:64"`
	ListType string `gorm:"uniqueIndex:idx_room_list;size:16;default:whitelist"`
}

func (Room) TableName() string {
	return "acl_rooms"
}

// PostgreSQL을 영구 저장소로 사용하고, 성능을 위해 인메모리 및 Valkey 캐시를 활용한다.
type Service struct {
	db     *gorm.DB
	cache  cache.Client
	logger *slog.Logger

	renameRoomsKeyFunc func(ctx context.Context, tempKey, key string, rooms []string) error

	// 메모리 캐시 (빠른 조회용)
	mu             sync.RWMutex
	enabled        bool
	mode           ACLMode
	whitelistRooms map[string]struct{}
	blacklistRooms map[string]struct{}
}

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
	cacheClient cache.Client,
	logger *slog.Logger,
	defaultEnabled bool,
	defaultMode ACLMode,
	defaultRooms []string,
) (*Service, error) {
	if logger == nil {
		logger = slog.Default()
	}

	db, err := aclDatabase(postgres)
	if err != nil {
		return nil, err
	}
	if cacheClient == nil {
		return nil, fmt.Errorf("cache service is nil")
	}

	normalizedMode, err := normalizeACLModeStrict(defaultMode)
	if err != nil {
		return nil, err
	}
	normalizedRooms := normalizeRoomList(defaultRooms)

	service := &Service{
		db:             db,
		cache:          cacheClient,
		logger:         logger,
		enabled:        defaultEnabled,
		mode:           normalizedMode,
		whitelistRooms: make(map[string]struct{}),
		blacklistRooms: make(map[string]struct{}),
	}

	// 시작 시 로드 (PostgreSQL → 메모리/Valkey)
	if err := service.loadFromDatabase(ctx, defaultEnabled, normalizedMode, normalizedRooms); err != nil {
		logger.Warn("Failed to load ACL from database, using defaults", slog.Any("error", err))

		service.enabled = defaultEnabled
		service.mode = normalizedMode
		service.addDefaultRooms(normalizedRooms)
	}

	logger.Info("ACL service initialized",
		slog.Bool("enabled", service.enabled),
		slog.String("mode", string(service.mode)),
		slog.Int("whitelist_rooms", len(service.whitelistRooms)),
		slog.Int("blacklist_rooms", len(service.blacklistRooms)),
	)

	return service, nil
}

func aclDatabase(postgres database.Client) (*gorm.DB, error) {
	if postgres == nil {
		return nil, fmt.Errorf("postgres service is nil")
	}
	db := postgres.GetGormDB()
	if db == nil {
		return nil, fmt.Errorf("gorm db is nil")
	}
	return db, nil
}

func (s *Service) addDefaultRooms(rooms []string) {
	targetRooms := s.activeRoomsMap()
	for _, r := range rooms {
		targetRooms[r] = struct{}{}
	}
}

// activeRoomsMap: 현재 활성 모드의 방 목록 맵을 반환한다 (잠금 없음, 호출자가 관리).
func (s *Service) activeRoomsMap() map[string]struct{} {
	if s.mode == ACLModeBlacklist {
		return s.blacklistRooms
	}

	return s.whitelistRooms
}

func (s *Service) roomsMapForMode(mode ACLMode) map[string]struct{} {
	if mode == ACLModeBlacklist {
		return s.blacklistRooms
	}

	return s.whitelistRooms
}

// loadFromDatabase PostgreSQL에서 ACL 설정 로드.
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
