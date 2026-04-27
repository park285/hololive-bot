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
	"sort"
	"sync"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
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
	cacheSvc cache.Client,
	logger *slog.Logger,
	defaultEnabled bool,
	defaultMode ACLMode,
	defaultRooms []string,
) (*Service, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if postgres == nil {
		return nil, fmt.Errorf("postgres service is nil")
	}

	db := postgres.GetGormDB()
	if db == nil {
		return nil, fmt.Errorf("gorm db is nil")
	}

	if cacheSvc == nil {
		return nil, fmt.Errorf("cache service is nil")
	}

	normalizedMode, err := normalizeACLModeStrict(defaultMode)
	if err != nil {
		return nil, err
	}
	normalizedRooms := normalizeRoomList(defaultRooms)

	svc := &Service{
		db:             db,
		cache:          cacheSvc,
		logger:         logger,
		enabled:        defaultEnabled,
		mode:           normalizedMode,
		whitelistRooms: make(map[string]struct{}),
		blacklistRooms: make(map[string]struct{}),
	}

	// 시작 시 로드 (PostgreSQL → 메모리/Valkey)
	if err := svc.loadFromDatabase(ctx, defaultEnabled, normalizedMode, normalizedRooms); err != nil {
		logger.Warn("Failed to load ACL from database, using defaults", slog.Any("error", err))

		svc.enabled = defaultEnabled
		svc.mode = normalizedMode
		// 기존 기본 방 목록을 현재 모드의 목록에 추가
		targetRooms := svc.activeRoomsMap()

		for _, r := range normalizedRooms {
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

func (s *Service) roomsMapForMode(mode ACLMode) map[string]struct{} {
	if mode == ACLModeBlacklist {
		return s.blacklistRooms
	}

	return s.whitelistRooms
}

// loadFromDatabase PostgreSQL에서 ACL 설정 로드.
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

	return nil
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
		normalizedMode, err := normalizeACLModeStrict(defaultMode)
		if err != nil {
			return err
		}

		s.mode = normalizedMode
		if err := s.db.Create(&Settings{Key: dbKeyMode, Value: string(normalizedMode)}).Error; err != nil {
			return fmt.Errorf("failed to initialize ACL mode setting: %w", err)
		}
	case modeResult.Error != nil:
		return fmt.Errorf("failed to load ACL mode setting: %w", modeResult.Error)
	default:
		mode, err := parseACLModeStrict(modeSetting.Value)
		if err != nil {
			return err
		}
		s.mode = mode
	}

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
