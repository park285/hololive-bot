// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package alarmservice

import (
	"context"
	"log/slog"

	sharedlogging "github.com/park285/hololive-bot/shared-go/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/valkey-io/valkey-go"
)

func (as *AlarmService) GetNextStreamInfo(ctx context.Context, channelID string) (*domain.NextStreamInfo, error) {
	key := NextStreamKeyPrefix + channelID

	data, err := as.cache.HGetAll(ctx, key)
	if err != nil {
		return nil, sharedlogging.LogAndWrapError(ctx, as.logger, "get next stream info", err, slog.String("channel_id", channelID))
	}

	return as.parseNextStreamInfo(channelID, data), nil
}

func (as *AlarmService) getNextStreamInfosBatch(ctx context.Context, channelIDs []string) (map[string]*domain.NextStreamInfo, error) {
	if len(channelIDs) == 0 {
		return map[string]*domain.NextStreamInfo{}, nil
	}

	builder := as.cache.Builder()

	cmds := make([]valkey.Completed, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		cmds = append(cmds, builder.Hgetall().Key(NextStreamKeyPrefix+channelID).Build())
	}

	results := as.cache.DoMulti(ctx, cmds...)
	infos := make(map[string]*domain.NextStreamInfo, len(channelIDs))

	for i, result := range results {
		data, err := result.AsStrMap()
		if err != nil || len(data) == 0 {
			continue
		}

		if info := as.parseNextStreamInfo(channelIDs[i], data); info != nil {
			infos[channelIDs[i]] = info
		}
	}

	return infos, nil
}
