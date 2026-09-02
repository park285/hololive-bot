package sourceobservation

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/content"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func upsertContentClock(ctx context.Context, tx dbx.Tx, _ contract.ObservationKind, clock *content.EntityState) error {
	coverage, err := content.MarshalCoverage(clock.LastPositiveCoverage)
	if err != nil {
		return fmt.Errorf("upsert content evidence clock: %w", err)
	}

	var lastAbs any

	if clock.LastAbsenceObservationID != 0 {
		lastAbs = clock.LastAbsenceObservationID
	}

	if _, execErr := tx.Exec(
		ctx,
		mustSQL("repository_content_clock_upsert_0037_37.sql"),
		clock.VideoID,
		clock.FirstPositiveEffectiveAt,
		clock.Clock.LastPositiveEffectiveAt,
		clock.Clock.LastPositiveReceivedAt,
		clock.LastPositiveValueSHA256,
		clock.LastPositiveScopeSHA256,
		coverage,
		clock.Clock.LastNegativeEffectiveAt,
		clock.LastNegativeReceivedAt,
		clock.FirstAbsenceScheduledFor,
		clock.SecondAbsenceScheduledFor,
		lastAbs,
		clock.Clock.MissingSinceEffectiveAt,
		clock.ConsecutiveAbsenceSlots,
		clock.WithdrawnAt,
	); execErr != nil {
		return fmt.Errorf("upsert content evidence clock: %w", execErr)
	}

	return nil
}
