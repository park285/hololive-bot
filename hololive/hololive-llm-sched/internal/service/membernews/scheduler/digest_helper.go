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

package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/kapu/hololive-llm-sched/internal/schedulerkit"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type digestDispatchConfig struct {
	periodKey        string
	periodFieldName  string
	resultMessage    string
	allFailedMessage string
	lockKey          string
	skipMessage      string
	lockSkipMessage  string
	processRoom      func(context.Context, string, string) delivery.SendResult
}

func processDigestForRoom(
	ctx context.Context,
	service model.DigestService,
	fmtr model.DigestFormatter,
	outbox outboxEnqueuer,
	logger *slog.Logger,
	period model.Period,
	kind domain.DeliveryOutboxKind,
	periodKey, roomID, emptyHeader string,
) delivery.SendResult {
	var result delivery.SendResult

	digest, err := service.GenerateRoomDigest(ctx, roomID, period)
	if err != nil {
		if errors.Is(err, model.ErrNoSubscribedMembers) {
			logger.Info("Member news skip: room has no alarm members",
				slog.String("room_id", roomID),
				slog.String("period", string(period)),
			)
			result.Skipped = 1
			return result
		}

		logger.Error("Member news digest generation failed",
			slog.String("room_id", roomID),
			slog.String("period", string(period)),
			slog.String("error", err.Error()))
		result.Attempted = 1
		result.Failed = 1
		result.FailedRooms = append(result.FailedRooms, roomID)
		return result
	}

	result.Attempted = 1
	message := renderDigestMessage(ctx, fmtr, digest, emptyHeader)

	if err := outbox.Enqueue(ctx, kind, periodKey, roomID, message); err != nil {
		logger.Error("Failed to enqueue member news",
			slog.String("room_id", roomID),
			slog.String("period", string(period)),
			slog.String("error", err.Error()))
		result.Failed = 1
		result.FailedRooms = append(result.FailedRooms, roomID)
		return result
	}

	result.Sent = 1
	return result
}

func dispatchDigestRooms(ctx context.Context, rooms []model.SubscribedRoom, config digestDispatchConfig) delivery.SendResult {
	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		result delivery.SendResult
		sem    = make(chan struct{}, maxConcurrentDigests)
	)

	for i := range rooms {
		sem <- struct{}{}
		wg.Add(1)
		go func(roomID string) {
			defer wg.Done()
			defer func() { <-sem }()

			roomResult := config.processRoom(ctx, config.periodKey, roomID)
			mu.Lock()
			result.Merge(roomResult)
			mu.Unlock()
		}(rooms[i].RoomID)
	}
	wg.Wait()

	return result
}

func runMemberNewsDigest(
	ctx context.Context,
	digest *schedulerkit.DigestScheduler,
	service model.DigestService,
	processRoom func(context.Context, string, string) delivery.SendResult,
	config digestDispatchConfig,
) error {
	config.processRoom = processRoom
	return schedulerkit.RunDigest(ctx, digest, buildDigestOp(digest, service, config))
}

func buildDigestOp(
	digest *schedulerkit.DigestScheduler,
	service model.DigestService,
	config digestDispatchConfig,
) schedulerkit.DigestOp[[]model.SubscribedRoom] {
	return schedulerkit.DigestOp[[]model.SubscribedRoom]{
		LockKey: config.lockKey,
		OnLockNotAcquired: func() error {
			digest.Logger.Info(config.lockSkipMessage, slog.String(config.periodFieldName, config.periodKey))
			return nil
		},
		Collect: collectSubscribedRooms(service, digest.Logger, config.skipMessage),
		Execute: executeDigestDispatch(digest.Logger, config),
	}
}

func collectSubscribedRooms(service model.DigestService, logger *slog.Logger, skipMsg string) func(context.Context) ([]model.SubscribedRoom, bool, error) {
	return func(ctx context.Context) ([]model.SubscribedRoom, bool, error) {
		rooms, err := service.ListSubscribedRooms(ctx)
		if err != nil {
			return nil, false, fmt.Errorf("list subscribed rooms: %w", err)
		}
		if len(rooms) == 0 {
			logger.Info(skipMsg)
			return nil, false, nil
		}
		return rooms, true, nil
	}
}

func executeDigestDispatch(logger *slog.Logger, config digestDispatchConfig) func(context.Context, []model.SubscribedRoom) error {
	return func(ctx context.Context, rooms []model.SubscribedRoom) error {
		result := dispatchDigestRooms(ctx, rooms, config)
		logDigestResult(logger, config, result)
		if result.Sent == 0 && result.Failed > 0 {
			return fmt.Errorf(config.allFailedMessage, result.Failed)
		}
		return nil
	}
}

func logDigestResult(logger *slog.Logger, config digestDispatchConfig, result delivery.SendResult) {
	logger.Info(config.resultMessage,
		slog.String(config.periodFieldName, config.periodKey),
		slog.Int("attempted", result.Attempted),
		slog.Int("sent", result.Sent),
		slog.Int("skipped", result.Skipped),
		slog.Int("failed", result.Failed),
		slog.Any("failed_rooms", result.FailedRooms),
	)
}

func renderDigestMessage(ctx context.Context, fmtr model.DigestFormatter, digest *model.Digest, emptyHeader string) string {
	if digest == nil {
		return emptyHeader + "\n- 표시할 항목이 없습니다."
	}

	if fmtr != nil {
		formatted := fmtr.FormatMemberNewsDigest(ctx, digest)
		if strings.TrimSpace(formatted) != "" {
			return formatted
		}
	}

	if len(digest.TopItems) == 0 {
		return digest.Headline + "\n- 표시할 항목이 없습니다."
	}

	lines := make([]string, 0, 2+len(digest.TopItems))
	lines = append(lines, digest.Headline)
	for _, item := range digest.TopItems {
		lines = append(lines, item.Summary)
	}
	if digest.MoreSummary != "" {
		lines = append(lines, digest.MoreSummary)
	}
	return strings.Join(lines, "\n")
}
