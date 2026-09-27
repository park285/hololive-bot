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
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
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

// ParseACLModeStrict는 invalid 값을 whitelist 기본값으로 바꾸지 않는 검증 경계용 parser다.
func ParseACLModeStrict(s string) (ACLMode, error) {
	out, err := parseACLModeStrict(s)
	if err != nil {
		return out, fmt.Errorf("parse ACL mode strict: %w", err)
	}

	return out, nil
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

func parseACLEnabledStrict(s string) (bool, error) {
	switch stringutil.Normalize(s) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("unsupported acl enabled value: %q", s)
	}
}

// ErrInvalidRoomChatID는 새 ACL 등록값이 정확한 signed i64 chatID 문자열이 아님을 뜻한다.
// IsRoomAllowed는 chatID만 비교하므로 방 이름 같은 값은 어떤 방과도 매칭되지 않고, 그런 값이 들어가면
// whitelist는 조용히 모든 방을 막고 blacklist는 조용히 차단을 놓친다. 그래서 저장 전에 거절한다.
// 이미 저장된 비-chatID 값은 조회·제거만 할 수 있다(DEC-20260926-stack-hololive-room-acl-and-console-contract).
var ErrInvalidRoomChatID = errors.New("ACL room must be a canonical signed 64-bit chat ID")

// validateRoomChatID는 console roomRegistration(canonicalI64)과 같은 규칙으로 부호·앞자리 0·범위를 본다.
// 값이 방 이름일 수 있어 오류에는 원문을 넣지 않는다.
func validateRoomChatID(room string) error {
	id, err := strconv.ParseInt(room, 10, 64)
	if err != nil || strconv.FormatInt(id, 10) != room {
		return ErrInvalidRoomChatID
	}

	return nil
}

// validateDefaultRooms는 KAKAO_ROOMS seed 전체를 첫 초기화 여부와 무관하게 검증한다. 잘못된 seed는
// DB가 비는 순간(새 DB·빈 복원) 죽은 whitelist 행이 되므로 평소 기동에서 먼저 드러낸다.
func validateDefaultRooms(rooms []string) error {
	invalid := 0

	for _, room := range rooms {
		if validateRoomChatID(room) != nil {
			invalid++
		}
	}

	if invalid > 0 {
		return fmt.Errorf("%d of %d default rooms: %w", invalid, len(rooms), ErrInvalidRoomChatID)
	}

	return nil
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

	slices.Sort(rooms)

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

type Room struct {
	ID       uint   `db:"id"`
	RoomID   string `db:"room_id"`
	ListType string `db:"list_type"`
}

// PostgreSQL을 영구 저장소로 사용하고, 성능을 위해 인메모리 및 Valkey 캐시를 활용한다.
type Service struct {
	store  aclStore
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

	return s.store != nil && s.cache != nil && s.logger != nil &&
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

	store, err := aclStoreFromClient(postgres)
	if err != nil {
		return nil, fmt.Errorf("acl store from client: %w", err)
	}

	if cacheClient == nil {
		return nil, errors.New("cache service is nil")
	}

	normalizedMode, err := normalizeACLModeStrict(defaultMode)
	if err != nil {
		return nil, fmt.Errorf("normalize ACL mode strict: %w", err)
	}

	normalizedRooms := normalizeRoomList(defaultRooms)
	if err := validateDefaultRooms(normalizedRooms); err != nil {
		return nil, fmt.Errorf("validate default rooms: %w", err)
	}

	service := &Service{
		store:          store,
		cache:          cacheClient,
		logger:         logger,
		enabled:        defaultEnabled,
		mode:           normalizedMode,
		whitelistRooms: make(map[string]struct{}),
		blacklistRooms: make(map[string]struct{}),
	}

	// 시작 시 로드 (PostgreSQL → 메모리/Valkey)
	if err := service.loadFromDatabase(ctx, defaultEnabled, normalizedMode, normalizedRooms); err != nil {
		return nil, fmt.Errorf("load ACL from database: %w", err)
	}

	logger.Info("ACL service initialized",
		slog.Bool("enabled", service.enabled),
		slog.String("mode", string(service.mode)),
		slog.Int("whitelist_rooms", len(service.whitelistRooms)),
		slog.Int("blacklist_rooms", len(service.blacklistRooms)),
	)

	return service, nil
}

func aclStoreFromClient(postgres database.Client) (aclStore, error) {
	if postgres == nil {
		return nil, errors.New("postgres service is nil")
	}

	pool := postgres.GetPool()
	if pool == nil {
		return nil, errors.New("postgres pool is nil")
	}

	return newPgxACLStore(pool), nil
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

// IsRoomAllowed는 방 접근 허용 여부를 chatID 하나로 판정한다(빠른 메모리 조회).
// 방 이름은 같은 이름의 방을 모두 허용하는 비결정적 식별자라 매칭하지 않는다
// (DEC-20260926-stack-hololive-room-acl-and-console-contract).
func (s *Service) IsRoomAllowed(chatID string) bool {
	chatID = stringutil.TrimSpace(chatID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.enabled {
		return true
	}

	switch s.mode {
	case ACLModeBlacklist:
		// 블랙리스트: 목록에 있으면 차단, 없으면 허용
		return !isInRoomSet(s.blacklistRooms, chatID)
	case ACLModeWhitelist:
		// 화이트리스트: 목록에 있으면 허용, 없으면 차단
		return isInRoomSet(s.whitelistRooms, chatID)
	default:
		return false
	}
}

// isInRoomSet: 주어진 방 목록에 chatID가 존재하는지 확인한다.
func isInRoomSet(rooms map[string]struct{}, chatID string) bool {
	if chatID == "" {
		return false
	}

	_, ok := rooms[chatID]

	return ok
}
