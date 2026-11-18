package subscriptions

import (
	"context"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	targetpolicy "github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type alarmWriter interface {
	Add(ctx context.Context, alarm *domain.Alarm) error
	Remove(ctx context.Context, roomID, channelID string) error
	RemoveHost(ctx context.Context, roomID, channelID, hostID string) error
	ClearByRoom(ctx context.Context, roomID string) (int64, error)
}

type Cache interface {
	cache.DataCache
	cache.LowLevelCache
}

type AlarmService struct {
	cache           Cache
	memberData      domain.MemberDataProvider
	alarmRepository *alarmstore.Repository
	alarmWriter     alarmWriter
	logger          *slog.Logger
	targetPolicy    targetpolicy.TargetMinutePolicy
	targetMinutesMu sync.RWMutex
	cacheMutationMu sync.Mutex
}
