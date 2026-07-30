package alarmservice

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (as *AlarmService) GetNextStreamInfo(ctx context.Context, channelID string) (*domain.NextStreamInfo, error) {
	return as.cacheState.GetNextStreamInfo(ctx, channelID)
}

func (as *AlarmService) getNextStreamInfosBatch(ctx context.Context, channelIDs []string) (map[string]*domain.NextStreamInfo, error) {
	return as.cacheState.GetNextStreamInfosBatch(ctx, channelIDs)
}
