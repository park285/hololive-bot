package alarmservice

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (as *AlarmService) GetNextStreamInfo(ctx context.Context, channelID string) (*domain.NextStreamInfo, error) {
	out, err := as.cacheState.GetNextStreamInfo(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("get next stream info: %w", err)
	}

	return out, nil
}

func (as *AlarmService) getNextStreamInfosBatch(ctx context.Context, channelIDs []string) (map[string]*domain.NextStreamInfo, error) {
	out, err := as.cacheState.GetNextStreamInfosBatch(ctx, channelIDs)
	if err != nil {
		return nil, fmt.Errorf("get next stream infos batch: %w", err)
	}

	return out, nil
}
