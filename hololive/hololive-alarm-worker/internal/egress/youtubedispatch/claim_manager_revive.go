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

package youtubedispatch

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
)

// reviveStaleFailedOutbox는 ledger가 없고 모든 구성원이 적격이며 활성 잠금·발송 증거가 없을 때만 그룹을 복구한다.
// 하위 행이 없는 fanout 실패에도 같은 유효 기간을 적용한다.
func (d *ClaimManager) reviveStaleFailedOutbox(ctx context.Context, freshnessWindow time.Duration, batchSize int) (int64, error) {
	if d == nil || d.transition == nil {
		return 0, nil
	}

	if freshnessWindow <= 0 {
		return 0, nil
	}

	if batchSize <= 0 {
		batchSize = d.config.BatchSize
	}

	revived, err := d.reviveFailedLifecycleGroups(ctx, freshnessWindow, batchSize)
	if err != nil {
		return 0, fmt.Errorf("revive stale failed outbox: %w", err)
	}

	return revived, nil
}

func (d *ClaimManager) reviveFailedLifecycleGroups(
	ctx context.Context,
	freshnessWindow time.Duration,
	batchSize int,
) (int64, error) {
	deliveryResult, err := d.transition.ReviveFailedLogicalGroups(ctx, freshnessWindow, batchSize)
	observeLifecycleApply("revive_logical_group", deliveryResult.ApplyResult, err, max(deliveryResult.RevivedLogicalGroups, 1))

	if err != nil {
		observeOutboxReviveError()

		return 0, fmt.Errorf("revive failed lifecycle groups: %w", err)
	}

	for i := range deliveryResult.Blocked {
		d.logger.Error("Blocked failed logical delivery group revive",
			slog.String("logical_key_hash", deliveryResult.Blocked[i].KeyHash),
			slog.String("invariant_reason", string(deliveryResult.Blocked[i].Reason)))
	}

	if deliveryResult.Outcome != store.ApplyApplied {
		return 0, fmt.Errorf("revive failed lifecycle groups: outcome %s", deliveryResult.Outcome)
	}

	fanoutResult, fanoutCount, err := d.transition.ReviveFailedFanoutOutboxes(ctx, freshnessWindow, batchSize)
	observeLifecycleApply("revive_fanout", fanoutResult, err, max(fanoutCount, 1))

	if err != nil {
		observeOutboxReviveError()

		return 0, fmt.Errorf("revive failed fanout outboxes: %w", err)
	}

	if fanoutResult.Outcome != store.ApplyApplied {
		return 0, fmt.Errorf("revive failed fanout outboxes: outcome %s", fanoutResult.Outcome)
	}

	touched := slices.Concat(deliveryResult.TouchedOutboxIDs, fanoutResult.TouchedOutboxIDs)
	projector := d.projector

	if projector == nil {
		projector = newOutboxAggregateProjector(d.delivery)
	}

	if err := projector.Project(ctx, touched); err != nil {
		d.logger.Warn("Immediate aggregate projection failed after logical group revive", slog.Any("error", err))
	}

	revived := int64(deliveryResult.RevivedLogicalGroups + fanoutCount)
	if revived > 0 {
		observeOutboxRevived(revived)
	}

	return revived, nil
}
