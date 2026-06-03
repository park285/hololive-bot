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
	"time"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
)

// reviveStaleFailedOutbox는 전송 실패로 한 번도 발송 못 한 채 영구 FAILED된 알람을 PENDING으로 되살려
// 디스패처가 재전송하도록 한다. poll-persist의 ON CONFLICT rearm은 community/shorts 재발견에만 도달하고
// (videos_poller는 watermark를 무조건 전진시켜 video/live는 재발견 자체가 없음), 폴링과 무관하게 FAILED를
// 되살리는 경로는 이것뿐이다.
//
// 대상 선정 predicate:
//   - status='FAILED': aggregate FAILED는 pending room이 없음을 함의(resolveOutboxStatus) = 재시도 소진.
//   - sent_at IS NULL: aggregate가 한 번도 SENT에 도달 안 함 = 사용자에게 전달된 적 없음(중복 방지 가드).
//   - kind NOT IN (community_post, new_short): 이 sweep은 per-post "alarm-once" 게이트
//     (isCommunityShortsDeliveryAuditKind, dispatcher_claim_acquire의 alarm_sent_at 체크)를 우회하는
//     kind(NEW_VIDEO/LIVE_STREAM/MILESTONE)만 대상으로 한다. community/shorts는 한 room이라도 보내면
//     per-post로 alarm-once 처리되어, 되살려도 dispatch에서 재전달이 skip된다(무의미한 PENDING 플립).
//     community/shorts의 미발송 복구는 자체 ON CONFLICT 재발견 rearm + alarm-state 경로가 담당한다.
//   - created_at >= now-freshnessWindow: 콘텐츠가 아직 신선할 때만(철 지난 알림 스팸 방지). created_at은
//     poller가 콘텐츠를 감지한 시각 ≈ 발행 직후라 freshness proxy로 적합하다.
//   - locked_at 만료: 처리 중(in-flight) 행은 건드리지 않음.
//
// freshness window가 재시도 횟수를 자연 bound한다(window/sweep-interval). 영구 미전달 행은 window를
// 벗어나면 더는 되살아나지 않아 무한 재시도를 막는다 — 별도 카운터 컬럼이 불필요하다.
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
		return 0, err
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

	if err := resetFailedDeliveryRows(ctx, tx, ids, now); err != nil {
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
	if err := deliverysql.SelectDeliverySQL(ctx, tx, &ids, "revive: select stale failed outbox", `
		SELECT id
		FROM youtube_notification_outbox
		WHERE status = ?
		  AND sent_at IS NULL
		  AND kind NOT IN (?, ?)
		  AND created_at >= ?
		  AND (locked_at IS NULL OR locked_at < ?)
		ORDER BY id
		LIMIT ?
		FOR UPDATE SKIP LOCKED
	`, string(domain.OutboxStatusFailed), string(domain.OutboxKindCommunityPost), string(domain.OutboxKindNewShort), freshCutoff, lockCutoff, batchSize); err != nil {
		return nil, err
	}
	return ids, nil
}

// per-room dedup: FAILED delivery 행만 재시도 대상으로 리셋, SENT 행은 불변.
func resetFailedDeliveryRows(ctx context.Context, tx dbx.Querier, ids []int64, now time.Time) error {
	deliveryArgs := []any{now}
	deliveryArgs = deliverysql.AppendDeliveryInt64Args(deliveryArgs, ids)
	deliveryArgs = append(deliveryArgs, string(domain.OutboxStatusFailed))
	_, err := deliverysql.ExecDeliverySQL(ctx, tx, "revive: reset failed delivery rows", `
		UPDATE youtube_notification_delivery
		SET status = 'PENDING', attempt_count = 0, next_attempt_at = ?, locked_at = NULL, sent_at = NULL, error = ''
		WHERE `+deliverysql.DeliveryInClause("outbox_id", len(ids))+`
		  AND status = ?
	`, deliveryArgs...)
	return err
}

func resetFailedOutboxRows(ctx context.Context, tx dbx.Querier, ids []int64, now time.Time) (int64, error) {
	outboxArgs := []any{now}
	outboxArgs = deliverysql.AppendDeliveryInt64Args(outboxArgs, ids)
	outboxArgs = append(outboxArgs, string(domain.OutboxStatusFailed))
	return deliverysql.ExecDeliverySQL(ctx, tx, "revive: reset outbox rows", `
		UPDATE youtube_notification_outbox
		SET status = 'PENDING', attempt_count = 0, next_attempt_at = ?, locked_at = NULL, error = ''
		WHERE `+deliverysql.DeliveryInClause("id", len(ids))+`
		  AND status = ?
	`, outboxArgs...)
}
