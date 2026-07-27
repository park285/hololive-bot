package alarmcache

import (
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

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
