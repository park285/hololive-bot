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

	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type digestDispatchConfig struct {
	lockKey           string
	periodKey         string
	periodFieldName   string
	lockHeldMessage   string
	emptyRoomsMessage string
	resultMessage     string
	allFailedMessage  string
	processRoom       func(context.Context, string, string) delivery.SendResult
}

// processDigestForRoom: 단일 room의 다이제스트 생성 + outbox enqueue (weekly/monthly 공용).
func processDigestForRoom(
	ctx context.Context,
	svc model.DigestService,
	fmtr model.DigestFormatter,
	outbox outboxEnqueuer,
	logger *slog.Logger,
	period model.Period,
	kind domain.DeliveryOutboxKind,
	periodKey, roomID, emptyHeader string,
) delivery.SendResult {
	var result delivery.SendResult

	digest, err := svc.GenerateRoomDigest(ctx, roomID, period)
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

func runDigestDispatch(
	ctx context.Context,
	service model.DigestService,
	locker delivery.NotificationLocker,
	logger *slog.Logger,
	cfg digestDispatchConfig,
) error {
	token, acquired, err := locker.TryAcquire(ctx, cfg.lockKey, delivery.DefaultExecutionLockTTL)
	if err != nil {
		return fmt.Errorf("acquire digest execution lock: %w", err)
	}
	if !acquired {
		logger.Info(cfg.lockHeldMessage, slog.String(cfg.periodFieldName, cfg.periodKey))
		return nil
	}
	defer func() { _ = locker.Release(ctx, cfg.lockKey, token) }()

	rooms, err := service.ListSubscribedRooms(ctx)
	if err != nil {
		return fmt.Errorf("list subscribed rooms: %w", err)
	}
	if len(rooms) == 0 {
		logger.Info(cfg.emptyRoomsMessage)
		return nil
	}

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

			roomResult := cfg.processRoom(ctx, cfg.periodKey, roomID)
			mu.Lock()
			result.Merge(roomResult)
			mu.Unlock()
		}(rooms[i].RoomID)
	}
	wg.Wait()

	logger.Info(cfg.resultMessage,
		slog.String(cfg.periodFieldName, cfg.periodKey),
		slog.Int("attempted", result.Attempted),
		slog.Int("sent", result.Sent),
		slog.Int("skipped", result.Skipped),
		slog.Int("failed", result.Failed),
		slog.Any("failed_rooms", result.FailedRooms),
	)

	if result.Sent == 0 && result.Failed > 0 {
		return fmt.Errorf(cfg.allFailedMessage, result.Failed)
	}

	return nil
}

// renderDigestMessage: 다이제스트 메시지 포맷팅 (weekly/monthly 공용).
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
