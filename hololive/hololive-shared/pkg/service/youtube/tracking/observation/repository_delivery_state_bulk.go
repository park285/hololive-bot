package observation

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

var bulkApplyAlarmSentMarksSQL = mustSQL("repository_delivery_state_bulk_0011_01.sql")

type bulkAlarmSentMarkInputs struct {
	kinds               []string
	contentIDs          []string
	canonicalContentIDs []string
	rawContentIDs       []string
	alarmSentAts        []time.Time
	authorizedAts       []pgtype.Timestamptz
}

func newBulkAlarmSentMarkInputs(marks []AlarmSentMark) (bulkAlarmSentMarkInputs, error) {
	inputs := bulkAlarmSentMarkInputs{
		kinds:               make([]string, 0, len(marks)),
		contentIDs:          make([]string, 0, len(marks)),
		canonicalContentIDs: make([]string, 0, len(marks)),
		rawContentIDs:       make([]string, 0, len(marks)),
		alarmSentAts:        make([]time.Time, 0, len(marks)),
		authorizedAts:       make([]pgtype.Timestamptz, 0, len(marks)),
	}

	for i, mark := range marks {
		if err := appendBulkAlarmSentMarkInput(&inputs, i, mark); err != nil {
			return bulkAlarmSentMarkInputs{}, fmt.Errorf("append bulk alarm sent mark input: %w", err)
		}
	}

	return inputs, nil
}

func appendBulkAlarmSentMarkInput(inputs *bulkAlarmSentMarkInputs, index int, mark AlarmSentMark) error {
	if mark.AlarmSentAt.IsZero() {
		return fmt.Errorf("bulk mark alarm sent: alarm sent at is empty at index %d", index)
	}

	canonicalContentID, err := canonicalTrackingIdentity(mark.Kind, mark.ContentID)
	if err != nil {
		return fmt.Errorf("bulk mark alarm sent: canonical content id at index %d: %w", index, err)
	}

	rawContentID, err := rawAlarmSentContentID(mark, canonicalContentID)
	if err != nil {
		return fmt.Errorf("bulk mark alarm sent: raw content id at index %d: %w", index, err)
	}

	inputs.kinds = append(inputs.kinds, string(mark.Kind))
	inputs.contentIDs = append(inputs.contentIDs, mark.ContentID)
	inputs.canonicalContentIDs = append(inputs.canonicalContentIDs, canonicalContentID)
	inputs.rawContentIDs = append(inputs.rawContentIDs, rawContentID)
	inputs.alarmSentAts = append(inputs.alarmSentAts, mark.AlarmSentAt)
	inputs.authorizedAts = append(inputs.authorizedAts, alarmSentAuthorizedAtValue(mark.AuthorizedAt))

	return nil
}

func rawAlarmSentContentID(mark AlarmSentMark, canonicalContentID string) (string, error) {
	candidates, err := trackingIdentityCandidates(mark.Kind, mark.ContentID)
	if err != nil {
		return "", fmt.Errorf("tracking identity candidates: %w", err)
	}

	for _, candidate := range candidates {
		if candidate != canonicalContentID {
			return candidate, nil
		}
	}

	return canonicalContentID, nil
}

func alarmSentAuthorizedAtValue(authorizedAt *time.Time) pgtype.Timestamptz {
	if authorizedAt == nil {
		return pgtype.Timestamptz{}
	}

	return pgtype.Timestamptz{Time: *authorizedAt, Valid: true}
}
