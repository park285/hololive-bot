package alarmcache

import (
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	MemberNameKey       = sharedalarmkeys.MemberNameKey
	RoomNamesCacheKey   = sharedalarmkeys.RoomNamesCacheKey
	UserNamesCacheKey   = sharedalarmkeys.UserNamesCacheKey
	NextStreamKeyPrefix = sharedalarmkeys.NextStreamKeyPrefix

	NotifiedKeyPrefix           = sharedalarmkeys.NotifiedKeyPrefix
	NotifyClaimKeyPrefix        = sharedalarmkeys.NotifyClaimKeyPrefix
	NotifyLogicalClaimKeyPrefix = sharedalarmkeys.NotifyLogicalClaimKeyPrefix
	UpcomingEventKeyPrefix      = sharedalarmkeys.UpcomingEventKeyPrefix
	ScheduleTransitionKeyPrefix = sharedalarmkeys.ScheduleTransitionKeyPrefix
)

type NotifiedData = dedup.NotifiedData

type UpcomingEventNotifiedData = dedup.UpcomingEventNotifiedData

type MemberDataFunc func() domain.MemberDataProvider

type State struct {
	Cache            cache.Client
	memberDataFn     MemberDataFunc
	Logger           *slog.Logger
	NotifiedLegacyMu sync.Mutex
}

func NewState(cacheClient cache.Client, memberDataFn MemberDataFunc, logger *slog.Logger) *State {
	return &State{
		Cache:        cacheClient,
		memberDataFn: memberDataFn,
		Logger:       logger,
	}
}

func (s *State) memberData() domain.MemberDataProvider {
	if s.memberDataFn == nil {
		return nil
	}
	return s.memberDataFn()
}
