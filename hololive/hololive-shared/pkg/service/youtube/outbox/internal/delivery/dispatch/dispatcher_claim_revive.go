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

package dispatch

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
)

// reviveStaleFailedOutbox는 전송 실패로 한 번도 발송 못 한 채 영구 FAILED된 알람을 PENDING으로 되살려
// 디스패처가 재전송하도록 한다. poll-persist의 ON CONFLICT rearm은 재발견(re-poll)된 행에만 도달하지만
// NEW_VIDEO/LIVE_STREAM은 poll-persist rearm 대상이 아니므로, 폴링과 무관하게 FAILED를 되살리는
// 경로는 이것뿐이다(community/shorts 포함 전 kind).
//
// 대상 선정 predicate:
//   - status='FAILED': aggregate FAILED는 pending room이 없음을 함의(delivery_repository_aggregate_sync.sql) = 재시도 소진.
//   - sent_at IS NULL: aggregate가 한 번도 SENT에 도달 안 함 = 사용자에게 전달된 적 없음(중복 방지 가드).
//     community/shorts는 한 room이라도 보내면 alarm-once로 aggregate가 SENT가 되어 이 가드가 제외하며,
//     부분 전송 행이 남더라도 dispatch의 alarm-once 게이트(alarm_sent_at)가 재전달을 한 번 더 막는다.
//   - created_at >= now-freshnessWindow: 콘텐츠가 아직 신선할 때만(철 지난 알림 스팸 방지). created_at은
//     poller가 콘텐츠를 감지한 시각 ≈ 발행 직후라 freshness proxy로 적합하다.
//   - locked_at 만료: 처리 중(in-flight) 행은 건드리지 않음.
//   - 리셋 가능성: FAILED delivery가 1개 이상이거나(재전송 대상 존재) delivery가 전무할 때(fan-out 전 실패)만
//     선택한다. 전량 QUARANTINED로 FAILED가 0개인 outbox를 넣으면 resetFailedDeliveryRows가 리셋할 행이 없어
//     aggregate sync가 즉시 FAILED로 되돌리고 revive 메트릭만 오르는 무한 flap이 되므로 제외한다.
//
// freshness window가 재시도 횟수를 자연 bound한다.
//
// 중복 방지는 per-room 단위로도 보장한다: 되살릴 때 FAILED delivery 행만 PENDING으로 리셋하고 SENT 행은
// 그대로 둔다(부분 전달 시 이미 받은 room에 재발송하지 않음). 전체를 하나의 트랜잭션으로 처리하고
// FOR UPDATE SKIP LOCKED로 동시 sweeper(HA) 간 경합을 회피한다.
func (d *ClaimManager) reviveStaleFailedOutbox(ctx context.Context, freshnessWindow time.Duration, batchSize int) (int64, error) {
	if d == nil || d.db == nil {
		return 0, nil
	}
	if freshnessWindow <= 0 {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = d.config.BatchSize
	}

	now := time.Now().UTC()
	freshCutoff := now.Add(-freshnessWindow)
	lockCutoff := now.Add(-d.deliveryClaimTimeout())

	var revived int64
	err := deliverysql.InDeliveryTx(ctx, d.db, func(tx dbx.Querier) error {
		return reviveStaleFailedOutboxTx(ctx, tx, freshCutoff, lockCutoff, batchSize, now, &revived)
	})
	if err != nil {
		observeOutboxReviveError()
		return 0, fmt.Errorf("revive stale failed outbox: %w", err)
	}

	if revived > 0 {
		observeOutboxRevived(revived)
	}
	return revived, nil
}

func reviveStaleFailedOutboxTx(ctx context.Context, tx dbx.Querier, freshCutoff, lockCutoff time.Time, batchSize int, now time.Time, revived *int64) error {
	ids, err := selectStaleFailedOutboxIDs(ctx, tx, freshCutoff, lockCutoff, batchSize)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}

	err = resetFailedDeliveryRows(ctx, tx, ids, now)
	if err != nil {
		return err
	}

	affected, err := resetFailedOutboxRows(ctx, tx, ids, now)
	if err != nil {
		return err
	}
	*revived = affected
	return nil
}

func selectStaleFailedOutboxIDs(ctx context.Context, tx dbx.Querier, freshCutoff, lockCutoff time.Time, batchSize int) ([]int64, error) {
	var ids []int64
	if err := deliverysql.SelectDeliverySQL(ctx, tx, &ids, "revive: select stale failed outbox", mustSQL("dispatcher_claim_revive_0109_01.sql"), string(domain.OutboxStatusFailed), freshCutoff, lockCutoff, string(domain.OutboxStatusFailed), batchSize); err != nil {
		return nil, fmt.Errorf("revive: select stale failed outbox: %w", err)
	}
	return ids, nil
}

// per-room dedup: FAILED delivery 행만 재시도 대상으로 리셋, SENT 행은 불변.
func resetFailedDeliveryRows(ctx context.Context, tx dbx.Querier, ids []int64, now time.Time) error {
	deliveryArgs := []any{now}
	deliveryArgs = deliverysql.AppendDeliveryInt64Args(deliveryArgs, ids)
	deliveryArgs = append(deliveryArgs, string(domain.OutboxStatusFailed))
	if _, err := deliverysql.ExecDeliverySQL(ctx, tx, "revive: reset failed delivery rows", mustSQL("dispatcher_claim_revive_0141_02.sql")+deliverysql.DeliveryInClause("outbox_id", len(ids))+`
		  AND status = ?
	`, deliveryArgs...); err != nil {
		return fmt.Errorf("revive: reset failed delivery rows: %w", err)
	}
	return nil
}

func resetFailedOutboxRows(ctx context.Context, tx dbx.Querier, ids []int64, now time.Time) (int64, error) {
	outboxArgs := []any{now}
	outboxArgs = deliverysql.AppendDeliveryInt64Args(outboxArgs, ids)
	outboxArgs = append(outboxArgs, string(domain.OutboxStatusFailed))
	affected, err := deliverysql.ExecDeliverySQL(ctx, tx, "revive: reset outbox rows", mustSQL("dispatcher_claim_revive_0156_03.sql")+deliverysql.DeliveryInClause("id", len(ids))+`
		  AND status = ?
	`, outboxArgs...)
	if err != nil {
		return 0, fmt.Errorf("revive: reset outbox rows: %w", err)
	}
	return affected, nil
}
