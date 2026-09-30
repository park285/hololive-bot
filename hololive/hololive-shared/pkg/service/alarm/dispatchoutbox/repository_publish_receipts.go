package dispatchoutbox

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type publishReceiptQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type publishReceiptInput struct {
	Ordinal     int    `json:"ordinal"`
	EventKey    string `json:"event_key"`
	PayloadHash string `json:"payload_hash"`
	DedupeKey   string `json:"dedupe_key"`
}

func loadPublishReceipts(ctx context.Context, db publishReceiptQueryer, envelopes []domain.AlarmQueueEnvelope, insertedKeys []string) ([]PublishReceipt, error) {
	inputs := make([]publishReceiptInput, 0, len(envelopes))
	for i := range envelopes {
		event, delivery, err := buildLedgerRows(&envelopes[i])
		if err != nil {
			return nil, fmt.Errorf("build receipt identity: %w", err)
		}

		inputs = append(inputs, publishReceiptInput{Ordinal: i, EventKey: event.EventKey, PayloadHash: event.PayloadHash, DedupeKey: delivery.DedupeKey})
	}

	raw, err := jsonv2.Marshal(inputs)
	if err != nil {
		return nil, fmt.Errorf("marshal receipt identities: %w", err)
	}

	rows, err := db.Query(ctx, mustSQL("repository_publish_receipts.sql"), jsonbRecordsetParam(raw))
	if err != nil {
		return nil, fmt.Errorf("query receipt identities: %w", err)
	}
	defer rows.Close()

	inserted := make(map[string]bool, len(insertedKeys))
	for _, key := range insertedKeys {
		inserted[key] = true
	}

	receipts := make([]PublishReceipt, 0, len(inputs))

	for rows.Next() {
		var (
			receipt   PublishReceipt
			collision bool
		)

		if err := rows.Scan(&receipt.Ordinal, &receipt.DedupeKey, &collision, &receipt.Status); err != nil {
			return nil, fmt.Errorf("scan receipt identity: %w", err)
		}

		switch {
		case collision:
			receipt.Outcome = PublishRejectedCollision
		case receipt.Status == StatusSent:
			receipt.Outcome = PublishDuplicateSent
		case receipt.Status == StatusDLQ || receipt.Status == StatusQuarantined || receipt.Status == StatusCancelled:
			receipt.Outcome = PublishRejectedTerminal
		case inserted[receipt.DedupeKey]:
			receipt.Outcome = PublishInserted
			delete(inserted, receipt.DedupeKey)
		default:
			receipt.Outcome = PublishDuplicateActive
		}

		receipts = append(receipts, receipt)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read receipt identities: %w", err)
	}

	return receipts, nil
}

func countPublishReceiptDuplicates(result *PublishBatchResult) {
	result.DuplicateDeliveries = 0
	result.TerminalDuplicates = 0

	for _, receipt := range result.Receipts {
		switch receipt.Outcome {
		case PublishInserted, PublishRejectedCollision:
			continue
		case PublishDuplicateActive:
			result.DuplicateDeliveries++
		case PublishDuplicateSent, PublishRejectedTerminal:
			result.DuplicateDeliveries++

			result.TerminalDuplicates++
		}
	}
}

func (r *PgxRepository) reconcilePublishCommit(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, commitErr error) (PublishBatchResult, error) {
	// commit 응답 소실은 재발행하지 않고 안정적인 event/hash/delivery key를 한 번만 재조회한다.
	// 취소된 요청도 최대 5초의 읽기 예산 안에서만 확인하며, 미확인 항목은 receipt를 주지 않는다.
	verifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	receipts, err := loadPublishReceipts(verifyCtx, r.pool, envelopes, nil)
	result := PublishBatchResult{RequestedDeliveries: len(envelopes), Receipts: receipts}
	countPublishReceiptDuplicates(&result)

	return processedPublishBatchResult(&result), fmt.Errorf("insert dispatch ledger batch: commit: %w", errors.Join(commitErr, err))
}
