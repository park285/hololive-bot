package observation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *deliveryStateRepository) MarkAlarmSentBatch(ctx context.Context, marks []AlarmSentMark) error {
	if len(marks) == 0 {
		return nil
	}

	if r == nil || r.db == nil {
		return errors.New("mark alarm sent batch: db is nil")
	}

	normalized, err := normalizeAlarmSentMarks(marks)
	if err != nil {
		return fmt.Errorf("mark alarm sent batch: %w", err)
	}

	if err := inPgxTx(ctx, r.db, func(tx trackingDB) error {
		txRepo := NewRepositoryContext(ctx, tx)
		return txRepo.delivery.applyAlarmSentMarks(ctx, normalized)
	}); err != nil {
		return fmt.Errorf("mark alarm sent batch transaction: %w", err)
	}

	return nil
}

func (r *deliveryStateRepository) applyAlarmSentMarks(ctx context.Context, marks []AlarmSentMark) error {
	if len(marks) == 0 {
		return nil
	}

	inputs, err := newBulkAlarmSentMarkInputs(marks)
	if err != nil {
		return fmt.Errorf("bulk alarm sent mark inputs: %w", err)
	}

	updatedAt := yttimestamp.Normalize(time.Now())

	var (
		trackingUpdated      int64
		existingStateUpdated int64
		missingStateInserted int64
	)

	if err := r.db.QueryRow(ctx, bulkApplyAlarmSentMarksSQL, inputs.kinds, inputs.canonicalContentIDs, inputs.alarmSentAts, updatedAt, alarmLatencyExceededThresholdMillis).Scan(
		&trackingUpdated,
		&existingStateUpdated,
		&missingStateInserted,
	); err != nil {
		return fmt.Errorf("bulk mark alarm sent: %w", err)
	}

	return nil
}

// normalizeAlarmSentMarks는 같은 정본 식별자의 표시를 하나로 합치고 가장 이른 발송 시각을 남긴다.
func normalizeAlarmSentMarks(marks []AlarmSentMark) ([]AlarmSentMark, error) {
	normalized := make([]AlarmSentMark, 0, len(marks))
	indexByIdentity := make(map[string]int, len(marks))

	for i, mark := range marks {
		normalizedMark, identity, err := normalizeAlarmSentMark(i, mark)
		if err != nil {
			return nil, fmt.Errorf("normalize alarm sent mark: %w", err)
		}

		if existingIndex, ok := indexByIdentity[identity]; ok {
			if normalizedMark.AlarmSentAt.Before(normalized[existingIndex].AlarmSentAt) {
				normalized[existingIndex].AlarmSentAt = normalizedMark.AlarmSentAt
			}

			continue
		}

		indexByIdentity[identity] = len(normalized)
		normalized = append(normalized, normalizedMark)
	}

	return normalized, nil
}

func normalizeAlarmSentMark(index int, mark AlarmSentMark) (AlarmSentMark, string, error) {
	normalizedKind, normalizedContentID, err := normalizeIdentity(mark.Kind, mark.ContentID)
	if err != nil {
		return AlarmSentMark{}, "", fmt.Errorf("normalize mark at index %d: %w", index, err)
	}

	if mark.AlarmSentAt.IsZero() {
		return AlarmSentMark{}, "", fmt.Errorf("normalize mark at index %d: alarm sent at is empty", index)
	}

	normalizedMark := AlarmSentMark{
		Kind:        normalizedKind,
		ContentID:   normalizedContentID,
		AlarmSentAt: yttimestamp.Normalize(mark.AlarmSentAt),
	}

	canonicalContentID, err := canonicalTrackingIdentity(normalizedKind, normalizedContentID)
	if err != nil {
		return AlarmSentMark{}, "", fmt.Errorf("canonical tracking identity: %w", err)
	}

	return normalizedMark, alarmSentMarkIdentity(normalizedKind, canonicalContentID), nil
}

func alarmSentMarkIdentity(kind domain.OutboxKind, contentID string) string {
	return string(kind) + "\x00" + contentID
}
