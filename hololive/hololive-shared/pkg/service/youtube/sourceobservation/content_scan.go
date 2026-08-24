package sourceobservation

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/content"
)

func scanContentClock(rows pgx.Rows, kind contract.ObservationKind) (content.EntityState, error) {
	var (
		state      content.EntityState
		coverage   []byte
		lastNegAt  *time.Time
		lastNegRec *time.Time
		firstAbs   *time.Time
		secondAbs  *time.Time
		lastAbsID  *int64
		missing    *time.Time
		withdrawn  *time.Time
	)

	err := rows.Scan(
		&state.VideoID,
		&state.FirstPositiveEffectiveAt,
		&state.Clock.LastPositiveEffectiveAt,
		&state.Clock.LastPositiveReceivedAt,
		&state.LastPositiveValueSHA256,
		&state.LastPositiveScopeSHA256,
		&coverage,
		&lastNegAt,
		&lastNegRec,
		&firstAbs,
		&secondAbs,
		&lastAbsID,
		&missing,
		&state.ConsecutiveAbsenceSlots,
		&withdrawn,
	)
	if err != nil {
		return content.EntityState{}, fmt.Errorf("scan content clock: %w", err)
	}

	decoded, err := content.ParseCoverage(kind, coverage)
	if err != nil {
		return content.EntityState{}, fmt.Errorf("parse coverage: %w", err)
	}

	state.LastPositiveCoverage = decoded
	state.Clock.LastNegativeEffectiveAt = lastNegAt
	state.LastNegativeReceivedAt = lastNegRec
	state.FirstAbsenceScheduledFor = firstAbs
	state.SecondAbsenceScheduledFor = secondAbs
	state.Clock.MissingSinceEffectiveAt = missing
	state.WithdrawnAt = withdrawn

	if lastAbsID != nil {
		state.LastAbsenceObservationID = *lastAbsID
	}

	return state, nil
}

func scanAbsenceSlot(rows pgx.Rows, kind contract.ObservationKind) (content.AbsenceSlot, error) {
	var (
		slot          content.AbsenceSlot
		coverage      []byte
		observationID *int64
	)

	if err := rows.Scan(
		&slot.ScheduledFor,
		&observationID,
		&slot.EvidenceSHA256,
		&slot.EffectiveAt,
		&slot.ReceivedAt,
		&slot.ScopeSHA256,
		&coverage,
	); err != nil {
		return content.AbsenceSlot{}, fmt.Errorf("scan content absence slot: %w", err)
	}

	decoded, err := content.ParseCoverage(kind, coverage)
	if err != nil {
		return content.AbsenceSlot{}, fmt.Errorf("parse coverage: %w", err)
	}

	slot.Coverage = decoded

	if observationID != nil {
		slot.ObservationID = *observationID
	}

	return slot, nil
}
