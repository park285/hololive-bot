package notification

import (
	"log/slog"
	"sync"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

// 알람 키 상수 목록.
const (
	AlarmKeyPrefix              = "alarm:"
	AlarmRegistryKey            = "alarm:registry"
	AlarmChannelRegistryKey     = "alarm:channel_registry"
	ChannelSubscribersKeyPrefix = "alarm:channel_subscribers:"
	ChzzkChannelMapKey          = "alarm:chzzk_channels"
	TwitchLoginMapKey           = "alarm:twitch_logins"
	MemberNameKey               = "alarm:member_names"
	RoomNamesCacheKey           = "alarm:room_names"
	UserNamesCacheKey           = "alarm:user_names"
	NotifiedKeyPrefix           = "notified:"
	NotifyClaimKeyPrefix        = "notified:claim:"
	NotifyLogicalClaimKeyPrefix = "notified:claim:event:"
	UpcomingEventKeyPrefix      = "notified:upcoming:event:"
	ScheduleTransitionKeyPrefix = "notified:schedule:transition:"
	NextStreamKeyPrefix         = "alarm:next_stream:"

	// 타입별 구독자 키 접두사 (COMMUNITY, SHORTS용)
	ChannelSubscribersCommunityPrefix = "alarm:channel_subscribers:COMMUNITY:"
	ChannelSubscribersShortsPrefix    = "alarm:channel_subscribers:SHORTS:"

	// Chzzk 알림 키 접두사
	ChzzkLiveNotifiedKeyPrefix  = "notified:chzzk:live:"
	IntegratedNotifiedKeyPrefix = "notified:integrated:"
)

// NotifiedData: 알림 중복 발송 방지를 위해 기록하는 알림 이력 정보
// SentAt: 발송된 minutesUntil을 키로 기록 (예: {5: true, 3: true})
// 스케줄 변경 시 StartScheduled 불일치 → SentAt 맵 리셋
type NotifiedData struct {
	StartScheduled string       `json:"start_scheduled"`
	SentAt         map[int]bool `json:"sent_at"`
}

// UpcomingEventNotifiedData: 예정 알림 발송 시각을 이벤트 단위로 기록합니다.
type UpcomingEventNotifiedData struct {
	NotifiedAt string `json:"notified_at"`
}

// AlarmService: 방송 알림(Alarm)을 관리하는 서비스 (Rust 이관 후 CRUD/상태 관리만 담당)
type AlarmService struct {
	cache           *cache.Service
	holodex         *holodex.Service
	chzzk           *chzzk.Client
	twitch          *twitch.Client
	memberData      domain.MemberDataProvider
	alarmRepo       *alarm.Repository
	logger          *slog.Logger
	targetMinutes   []int
	targetMinutesMu sync.RWMutex
	cacheMutex      sync.RWMutex
	persistPool     *workerpool.Pool
	closeOnce       sync.Once
}
