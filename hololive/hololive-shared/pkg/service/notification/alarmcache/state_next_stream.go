package alarmcache

import (
	"context"
	"fmt"
	"log/slog"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

func (s *State) GetNextStreamInfo(ctx context.Context, channelID string) (*domain.NextStreamInfo, error) {
	key := sharedalarmkeys.NextStreamKeyPrefix + channelID

	data, err := s.Cache.HGetAll(ctx, key)
	if err != nil {
		if logErr := sharedlogging.LogAndWrapError(ctx, s.Logger, "get next stream info", err, slog.String("channel_id", channelID)); logErr != nil {
			return nil, fmt.Errorf("log and wrap error: %w", logErr)
		}

		data = nil
	}

	return s.ParseNextStreamInfo(channelID, data), nil
}

func (s *State) GetNextStreamInfosBatch(ctx context.Context, channelIDs []string) (map[string]*domain.NextStreamInfo, error) {
	if len(channelIDs) == 0 {
		return map[string]*domain.NextStreamInfo{}, nil
	}

	builder := s.Cache.Builder()

	cmds := make([]valkey.Completed, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		cmds = append(cmds, builder.Hgetall().Key(sharedalarmkeys.NextStreamKeyPrefix+channelID).Build())
	}

	results := s.Cache.DoMulti(ctx, cmds...)
	infos := make(map[string]*domain.NextStreamInfo, len(channelIDs))

	for i, result := range results {
		data, err := result.AsStrMap()
		if err != nil || len(data) == 0 {
			continue
		}

		if info := s.ParseNextStreamInfo(channelIDs[i], data); info != nil {
			infos[channelIDs[i]] = info
		}
	}

	return infos, nil
}
