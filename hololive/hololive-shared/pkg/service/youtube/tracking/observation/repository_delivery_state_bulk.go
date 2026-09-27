package observation

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

var bulkApplyAlarmSentMarksSQL = mustSQL("repository_delivery_state_bulk_0011_01.sql")

// bulkAlarmSentMarkInputs는 tracking 행을 기본 키 (kind, canonical_content_id)로만 찾는다. 요청 content_id나 raw
// content_id로도 찾던 OR 분기는 T18(2026-09-26)에서 content_id와 canonical_content_id가 다른 행이 0건임을 확인해
// 지웠다(stack-audit T11 holo-tracking-raw-content-id-candidates).
type bulkAlarmSentMarkInputs struct {
	kinds               []string
	canonicalContentIDs []string
	alarmSentAts        []time.Time
	authorizedAts       []pgtype.Timestamptz
}

func newBulkAlarmSentMarkInputs(marks []AlarmSentMark) (bulkAlarmSentMarkInputs, error) {
	inputs := bulkAlarmSentMarkInputs{
		kinds:               make([]string, 0, len(marks)),
		canonicalContentIDs: make([]string, 0, len(marks)),
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

	inputs.kinds = append(inputs.kinds, string(mark.Kind))
	inputs.canonicalContentIDs = append(inputs.canonicalContentIDs, canonicalContentID)
	inputs.alarmSentAts = append(inputs.alarmSentAts, mark.AlarmSentAt)
	inputs.authorizedAts = append(inputs.authorizedAts, alarmSentAuthorizedAtValue(mark.AuthorizedAt))

	return nil
}

func alarmSentAuthorizedAtValue(authorizedAt *time.Time) pgtype.Timestamptz {
	if authorizedAt == nil {
		return pgtype.Timestamptz{}
	}

	return pgtype.Timestamptz{Time: *authorizedAt, Valid: true}
}
