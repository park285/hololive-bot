package alarmcache

import (
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	MemberNameKey       = sharedalarmkeys.MemberNameKey
	RoomNamesCacheKey   = sharedalarmkeys.RoomNamesCacheKey
	UserNamesCacheKey   = sharedalarmkeys.UserNamesCacheKey
	NextStreamKeyPrefix = sharedalarmkeys.NextStreamKeyPrefix

	NotifiedKeyPrefix           = "notified:"
	NotifyClaimKeyPrefix        = "notified:claim:"
	NotifyLogicalClaimKeyPrefix = "notified:claim:event:"
	UpcomingEventKeyPrefix      = "notified:upcoming:event:"
	ScheduleTransitionKeyPrefix = "notified:schedule:transition:"
)

type NotifiedData struct {
	StartScheduled string       `json:"start_scheduled"`
	SentAt         map[int]bool `json:"sent_at"`
}

type UpcomingEventNotifiedData struct {
	NotifiedAt string `json:"notified_at"`
}

type State struct {
	Cache            cache.Client
	MemberData       domain.MemberDataProvider
	Logger           *slog.Logger
	NotifiedLegacyMu sync.Mutex
}

func NewState(cacheClient cache.Client, memberData domain.MemberDataProvider, logger *slog.Logger) *State {
	return &State{
		Cache:      cacheClient,
		MemberData: memberData,
		Logger:     logger,
	}
}
