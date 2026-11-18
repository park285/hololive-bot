package acl

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/park285/shared-go/core/stringutil"

	"github.com/kapu/hololive-shared/pkg/service/database"
)

type ACLMode string

const (
	ACLModeWhitelist ACLMode = "whitelist"
	ACLModeBlacklist ACLMode = "blacklist"
)

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

var ErrInvalidRoomChatID = errors.New("ACL room must be a canonical signed 64-bit chat ID")

var ErrACLModeChanged = errors.New("ACL mode differs from the mode the room change expected")

func validateRoomChatID(room string) error {
	id, err := strconv.ParseInt(room, 10, 64)
	if err != nil || strconv.FormatInt(id, 10) != room {
		return ErrInvalidRoomChatID
	}

	return nil
}

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
		roomID = strings.TrimSpace(roomID)
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
	dbKeyEnabled = "enabled"
	dbKeyMode    = "mode"

	listTypeWhitelist = "whitelist"
	listTypeBlacklist = "blacklist"
)

type Room struct {
	RoomID   string `db:"room_id"`
	ListType string `db:"list_type"`
}

type Service struct {
	store  aclStore
	logger *slog.Logger

	mutationMu sync.Mutex

	mu             sync.RWMutex
	enabled        bool
	mode           ACLMode
	whitelistRooms map[string]struct{}
	blacklistRooms map[string]struct{}
}

func NewACLService(
	ctx context.Context,
	postgres database.Client,
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
		logger:         logger,
		enabled:        defaultEnabled,
		mode:           normalizedMode,
		whitelistRooms: make(map[string]struct{}),
		blacklistRooms: make(map[string]struct{}),
	}

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

func (s *Service) IsRoomAllowed(chatID string) bool {
	chatID = strings.TrimSpace(chatID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.enabled {
		return true
	}

	switch s.mode {
	case ACLModeBlacklist:
		return !isInRoomSet(s.blacklistRooms, chatID)
	case ACLModeWhitelist:
		return isInRoomSet(s.whitelistRooms, chatID)
	default:
		return false
	}
}

func isInRoomSet(rooms map[string]struct{}, chatID string) bool {
	if chatID == "" {
		return false
	}

	_, ok := rooms[chatID]

	return ok
}
