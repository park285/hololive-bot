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
	"log/slog"
	"strings"

	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

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
