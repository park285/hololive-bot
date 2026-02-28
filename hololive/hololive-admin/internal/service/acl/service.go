package acl

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

const (
	// Valkey 캐시 키
	aclSettingsKey = "acl:settings"
	aclRoomsKey    = "acl:rooms"
)

// Settings: ACL 설정을 저장하기 위한 GORM 모델 (key-value 형태)
type Settings struct {
	ID    uint   `gorm:"primaryKey"`
	Key   string `gorm:"uniqueIndex;size:64"`
	Value string `gorm:"type:text"`
}

// TableName: ACL 설정 테이블의 이름을 반환한다. ("acl_settings")
func (Settings) TableName() string {
	return "acl_settings"
}

// Room: ACL이 적용된 허용된 방 목록을 저장하기 위한 GORM 모델
type Room struct {
	ID     uint   `gorm:"primaryKey"`
	RoomID string `gorm:"uniqueIndex;size:64"`
}

// TableName: ACL 방 목록 테이블의 이름을 반환한다. ("acl_rooms")
func (Room) TableName() string {
	return "acl_rooms"
}

// Service: 접근 제어 목록(ACL)을 관리하는 서비스
// PostgreSQL을 영구 저장소로 사용하고, 성능을 위해 인메모리 및 Valkey 캐시를 활용한다.
type Service struct {
	db     *gorm.DB
	cache  *cache.Service
	logger *slog.Logger

	// 메모리 캐시 (빠른 조회용)
	mu      sync.RWMutex
	enabled bool
	rooms   map[string]struct{}
}

// NewACLService ACL 서비스 생성 및 초기화
func NewACLService(
	ctx context.Context,
	postgres *database.PostgresService,
	cacheSvc *cache.Service,
	logger *slog.Logger,
	defaultEnabled bool,
	defaultRooms []string,
) (*Service, error) {
	db := postgres.GetGormDB()

	svc := &Service{
		db:      db,
		cache:   cacheSvc,
		logger:  logger,
		enabled: defaultEnabled,
		rooms:   make(map[string]struct{}),
	}

	// 시작 시 로드 (PostgreSQL → 메모리/Valkey)
	if err := svc.loadFromDatabase(ctx, defaultEnabled, defaultRooms); err != nil {
		logger.Warn("Failed to load ACL from database, using defaults", slog.Any("error", err))
		svc.enabled = defaultEnabled
		for _, r := range defaultRooms {
			svc.rooms[r] = struct{}{}
		}
	}

	logger.Info("ACL service initialized",
		slog.Bool("enabled", svc.enabled),
		slog.Int("rooms", len(svc.rooms)),
	)

	return svc, nil
}

// loadFromDatabase PostgreSQL에서 ACL 설정 로드
func (s *Service) loadFromDatabase(ctx context.Context, defaultEnabled bool, defaultRooms []string) error {
	// 1. ACL enabled 상태 로드 및 초기화 여부 확인
	var settings Settings
	result := s.db.Where("key = ?", "enabled").First(&settings)
	isFirstInit := stdErrors.Is(result.Error, gorm.ErrRecordNotFound)

	if isFirstInit {
		// 첫 초기화: 기본값 저장
		s.enabled = defaultEnabled
		s.db.Create(&Settings{Key: "enabled", Value: fmt.Sprintf("%t", defaultEnabled)})
	} else if result.Error != nil {
		return fmt.Errorf("failed to load ACL enabled setting: %w", result.Error)
	} else {
		s.enabled = settings.Value == "true"
	}

	// 2. Rooms 로드
	var rooms []Room
	if err := s.db.Find(&rooms).Error; err != nil {
		return fmt.Errorf("failed to load ACL rooms: %w", err)
	}

	s.mu.Lock()
	s.rooms = make(map[string]struct{})
	if isFirstInit && len(rooms) == 0 {
		// 첫 초기화일 때만 기본 방 저장 (빈 리스트도 유효한 상태로 허용)
		for _, r := range defaultRooms {
			s.rooms[r] = struct{}{}
			s.db.Create(&Room{RoomID: r})
		}
	} else {
		// 기존 DB 상태 로드 (빈 리스트여도 그대로 유지)
		for _, r := range rooms {
			s.rooms[r.RoomID] = struct{}{}
		}
	}
	s.mu.Unlock()

	s.syncSettingsToValkey(ctx)
	s.syncRoomsToValkey(ctx)

	return nil
}

// syncRoomsToValkey: 메모리 → Valkey SET 동기화 (전체 교체)
// 서비스 시작 시 또는 전체 리프레시가 필요할 때 호출됩니다.
func (s *Service) syncRoomsToValkey(ctx context.Context) {
	s.mu.RLock()
	rooms := make([]string, 0, len(s.rooms))
	for r := range s.rooms {
		rooms = append(rooms, r)
	}
	s.mu.RUnlock()

	// 기존 SET 삭제 후 새로 추가 (DEL + SADD)
	_ = s.cache.Del(ctx, aclRoomsKey)
	if len(rooms) > 0 {
		_, _ = s.cache.SAdd(ctx, aclRoomsKey, rooms)
	}
}

// syncSettingsToValkey: ACL enabled 상태를 Valkey에 동기화합니다.
func (s *Service) syncSettingsToValkey(ctx context.Context) {
	s.mu.RLock()
	enabled := s.enabled
	s.mu.RUnlock()

	_ = s.cache.Set(ctx, aclSettingsKey, fmt.Sprintf("%t", enabled), 0)
}

// IsRoomAllowed 방 접근 허용 여부 확인 (빠른 메모리 조회)
func (s *Service) IsRoomAllowed(roomName, chatID string) bool {
	roomName = stringutil.TrimSpace(roomName)
	chatID = stringutil.TrimSpace(chatID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.enabled {
		return true
	}

	if chatID != "" {
		if _, ok := s.rooms[chatID]; ok {
			return true
		}
	}
	if roomName != "" {
		if _, ok := s.rooms[roomName]; ok {
			return true
		}
	}

	return false
}

// GetACLStatus 현재 ACL 상태 반환
func (s *Service) GetACLStatus() (enabled bool, rooms []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rooms = make([]string, 0, len(s.rooms))
	for r := range s.rooms {
		rooms = append(rooms, r)
	}
	return s.enabled, rooms
}

// SetEnabled ACL 활성화/비활성화
func (s *Service) SetEnabled(ctx context.Context, enabled bool) error {
	s.mu.Lock()
	s.enabled = enabled
	s.mu.Unlock()

	// PostgreSQL 저장
	result := s.db.Where("key = ?", "enabled").Assign(Settings{Value: fmt.Sprintf("%t", enabled)}).FirstOrCreate(&Settings{Key: "enabled"})
	if result.Error != nil {
		return fmt.Errorf("failed to save ACL enabled setting: %w", result.Error)
	}

	s.syncSettingsToValkey(ctx)

	s.logger.Info("ACL enabled status updated",
		slog.Bool("enabled", enabled),
	)

	return nil
}

// AddRoom 방 추가
func (s *Service) AddRoom(ctx context.Context, room string) (bool, error) {
	room = stringutil.TrimSpace(room)
	if room == "" {
		return false, nil
	}

	s.mu.Lock()
	if _, exists := s.rooms[room]; exists {
		s.mu.Unlock()
		return false, nil // 이미 존재
	}
	s.rooms[room] = struct{}{}
	s.mu.Unlock()

	// PostgreSQL 저장
	result := s.db.Create(&Room{RoomID: room})
	if result.Error != nil {
		s.mu.Lock()
		delete(s.rooms, room)
		s.mu.Unlock()
		return false, fmt.Errorf("failed to add room to database: %w", result.Error)
	}

	_, _ = s.cache.SAdd(ctx, aclRoomsKey, []string{room})

	s.logger.Info("Room added to ACL whitelist",
		slog.String("room", room),
	)

	return true, nil
}

// RemoveRoom 방 제거
func (s *Service) RemoveRoom(ctx context.Context, room string) (bool, error) {
	room = stringutil.TrimSpace(room)
	if room == "" {
		return false, nil
	}

	s.mu.Lock()
	if _, exists := s.rooms[room]; !exists {
		s.mu.Unlock()
		return false, nil // 존재하지 않음
	}
	delete(s.rooms, room)
	s.mu.Unlock()

	// PostgreSQL 삭제
	result := s.db.Where("room_id = ?", room).Delete(&Room{})
	if result.Error != nil {
		s.mu.Lock()
		s.rooms[room] = struct{}{}
		s.mu.Unlock()
		return false, fmt.Errorf("failed to remove room from database: %w", result.Error)
	}

	_, _ = s.cache.SRem(ctx, aclRoomsKey, []string{room})

	s.logger.Info("Room removed from ACL whitelist",
		slog.String("room", room),
	)

	return true, nil
}
