package dispatchoutbox

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
)

func (c *Consumer) envelopesFromRecords(ctx context.Context, records []*Record, events map[int64]EventRecord) ([]AlarmQueueEnvelope, error) {
	envelopes := make([]AlarmQueueEnvelope, 0, len(records))
	for _, record := range records {
		envelope, ok, err := c.envelopeFromRecord(ctx, record, events)
		if err != nil {
			return nil, fmt.Errorf("envelope from record: %w", err)
		}

		if !ok {
			continue
		}

		envelopes = append(envelopes, envelope)
	}

	return envelopes, nil
}

func (c *Consumer) envelopeFromRecord(ctx context.Context, record *Record, events map[int64]EventRecord) (AlarmQueueEnvelope, bool, error) {
	payload, ok, err := c.payloadForRecord(ctx, record, events)
	if err != nil {
		return AlarmQueueEnvelope{}, false, fmt.Errorf("payload for record: %w", err)
	}

	if !ok {
		return AlarmQueueEnvelope{}, false, nil
	}

	envelope, ok, err := c.decodeEnvelopePayload(ctx, record, payload)
	if err != nil {
		return AlarmQueueEnvelope{}, false, fmt.Errorf("decode envelope payload: %w", err)
	}

	if !ok {
		return AlarmQueueEnvelope{}, false, nil
	}

	ok, err = c.rehydrateEnvelope(ctx, record, &envelope)
	if err != nil {
		return AlarmQueueEnvelope{}, false, fmt.Errorf("rehydrate envelope: %w", err)
	}

	if !ok {
		return AlarmQueueEnvelope{}, false, nil
	}

	attachRecordMetadata(&envelope, record)

	return envelope, true, nil
}

func (c *Consumer) decodeEnvelopePayload(ctx context.Context, record *Record, payload []byte) (AlarmQueueEnvelope, bool, error) {
	var envelope AlarmQueueEnvelope

	if err := jsonv2.Unmarshal(payload, &envelope); err != nil {
		if dlqErr := c.moveRecordToDLQ(ctx, record, fmt.Sprintf("invalid payload: %v", err), "move invalid payload to dlq"); dlqErr != nil {
			return AlarmQueueEnvelope{}, false, fmt.Errorf("move record to DLQ: %w", dlqErr)
		}

		return AlarmQueueEnvelope{}, false, nil
	}

	return envelope, true, nil
}

func (c *Consumer) rehydrateEnvelope(ctx context.Context, record *Record, envelope *AlarmQueueEnvelope) (bool, error) {
	if err := rehydrateDeliveryContext(envelope, record); err != nil {
		if dlqErr := c.moveRecordToDLQ(ctx, record, fmt.Sprintf("invalid delivery context: %v", err), "move invalid delivery context to dlq"); dlqErr != nil {
			return false, fmt.Errorf("move record to DLQ: %w", dlqErr)
		}

		return false, nil
	}

	return true, nil
}

func attachRecordMetadata(envelope *AlarmQueueEnvelope, record *Record) {
	envelope.DispatchOutboxID = record.ID
	envelope.DispatchGroupKey = record.DispatchGroupKey
	envelope.SendUnitID = record.SendUnitID
	envelope.ClientRequestID = record.ClientRequestID
	envelope.RequestUnpinned = record.RequestUnpinned
	envelope.ClaimKeys = record.ClaimKeys

	if record.AttemptCount > 0 {
		envelope.Retry = &AlarmQueueRetryMetadata{
			Attempt:       record.AttemptCount,
			LastError:     record.Error,
			LastErrorCode: record.ErrorCode,
		}
	}
}

func (c *Consumer) payloadForRecord(ctx context.Context, record *Record, events map[int64]EventRecord) ([]byte, bool, error) {
	event, ok := events[record.EventID]
	if ok {
		return event.Payload, true, nil
	}

	if err := c.moveRecordToDLQ(ctx, record, "missing event payload", "move missing event to dlq"); err != nil {
		return nil, false, fmt.Errorf("move record to DLQ: %w", err)
	}

	return nil, false, nil
}

func (c *Consumer) moveRecordToDLQ(ctx context.Context, record *Record, terminalError, action string) error {
	update := TerminalUpdate{ID: record.ID, AttemptCount: record.AttemptCount, Error: sanitizeStoredError(terminalError), ErrorCode: ErrorCodePayload}
	if err := c.repository.MoveToDLQ(ctx, []TerminalUpdate{update}, c.workerID); err != nil {
		return fmt.Errorf("drain outbox batch: %s: %w", action, err)
	}

	// 이후 키 해제가 실패해도 확정된 terminal 행을 lease 반환 대상에서 제외합니다.
	record.Status = StatusDLQ

	observePGDLQ(1)

	// rows-affected를 포함한 worker fence 검증이 성공한 뒤에만 방별 dedup을 해제합니다.
	// Payload 복원에 실패해도 claim metadata는 PostgreSQL delivery row에 남아 있습니다.
	if err := c.ReleaseClaimKeys(ctx, record.ClaimKeys); err != nil {
		return fmt.Errorf("release rejected delivery claim keys: %w", err)
	}

	return nil
}

type deliveryContext struct {
	Users []string `json:"users,omitempty"`
}

func distinctEventIDs(records []*Record) []int64 {
	seen := make(map[int64]struct{}, len(records))
	ids := make([]int64, 0, len(records))

	for _, record := range records {
		if record == nil || record.EventID <= 0 {
			continue
		}

		if _, ok := seen[record.EventID]; ok {
			continue
		}

		seen[record.EventID] = struct{}{}
		ids = append(ids, record.EventID)
	}

	return ids
}

func rehydrateDeliveryContext(envelope *AlarmQueueEnvelope, record *Record) error {
	envelope.Notification.RoomID = record.RoomID
	if len(record.DeliveryContext) == 0 {
		return nil
	}

	var deliveryCtx deliveryContext

	if err := jsonv2.Unmarshal(record.DeliveryContext, &deliveryCtx); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	envelope.Notification.Users = deliveryCtx.Users

	return nil
}
