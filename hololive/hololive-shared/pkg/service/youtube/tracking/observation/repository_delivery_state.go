package observation

import (
	"context"
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
		return fmt.Errorf("mark alarm sent batch: db is nil")
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
		return err
	}

	updatedAt := yttimestamp.Normalize(time.Now())
	var trackingUpdated int64
	var claimedStateFinalized int64
	var authorizationMismatches int64
	var existingStateUpdated int64
	var missingStateInserted int64
	if err := r.db.QueryRow(ctx, bulkApplyAlarmSentMarksSQL, inputs.kinds, inputs.contentIDs, inputs.canonicalContentIDs, inputs.rawContentIDs, inputs.alarmSentAts, inputs.authorizedAts, updatedAt).Scan(
		&trackingUpdated,
		&claimedStateFinalized,
		&authorizationMismatches,
		&existingStateUpdated,
		&missingStateInserted,
	); err != nil {
		return fmt.Errorf("bulk mark alarm sent: %w", err)
	}
	if authorizationMismatches > 0 {
		return fmt.Errorf("bulk mark alarm sent: claim authorization mismatch count=%d", authorizationMismatches)
	}

	return nil
}

func normalizeAlarmSentMarks(marks []AlarmSentMark) ([]AlarmSentMark, error) {
	normalized := make([]AlarmSentMark, 0, len(marks))
	indexByIdentity := make(map[string]int, len(marks))

	for i, mark := range marks {
		normalizedMark, identity, err := normalizeAlarmSentMark(i, mark)
		if err != nil {
			return nil, err
		}
		if existingIndex, ok := indexByIdentity[identity]; ok {
			if err := mergeAlarmSentMark(&normalized[existingIndex], normalizedMark, identity, i); err != nil {
				return nil, err
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

	normalizedAuthorizedAt, err := normalizeAlarmSentAuthorizedAt(index, mark.AuthorizedAt)
	if err != nil {
		return AlarmSentMark{}, "", err
	}
	normalizedMark := AlarmSentMark{
		Kind:         normalizedKind,
		ContentID:    normalizedContentID,
		AlarmSentAt:  yttimestamp.Normalize(mark.AlarmSentAt),
		AuthorizedAt: normalizedAuthorizedAt,
	}
	return normalizedMark, alarmSentMarkIdentity(normalizedKind, normalizedContentID), nil
}

func normalizeAlarmSentAuthorizedAt(index int, authorizedAt *time.Time) (*time.Time, error) {
	if authorizedAt == nil {
		return nil, nil
	}
	if authorizedAt.IsZero() {
		return nil, fmt.Errorf("normalize mark at index %d: authorized at is empty", index)
	}
	normalized := yttimestamp.Normalize(*authorizedAt)
	return &normalized, nil
}

func alarmSentMarkIdentity(kind domain.OutboxKind, contentID string) string {
	return string(kind) + "\x00" + contentID
}

func mergeAlarmSentMark(existing *AlarmSentMark, next AlarmSentMark, identity string, index int) error {
	if next.AlarmSentAt.Before(existing.AlarmSentAt) {
		existing.AlarmSentAt = next.AlarmSentAt
	}
	if existing.AuthorizedAt == nil {
		existing.AuthorizedAt = next.AuthorizedAt
		return nil
	}
	if next.AuthorizedAt == nil {
		return nil
	}
	if !existing.AuthorizedAt.Equal(*next.AuthorizedAt) {
		return fmt.Errorf("normalize mark at index %d: conflicting authorized_at for %s", index, identity)
	}
	return nil
}
