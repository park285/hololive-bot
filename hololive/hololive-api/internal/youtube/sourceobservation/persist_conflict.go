package sourceobservation

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

func persistReconcileConflict(
	ctx context.Context,
	tx dbx.Tx,
	observation *Observation,
	entityKind, entityKey, fieldName, existingSHA, attemptedSHA string,
) error {
	_, err := tx.Exec(
		ctx,
		persistReconcileConflictSQL,
		observation.ID,
		observation.Provider,
		observation.ObservationKind,
		observation.SubjectKey,
		observation.ObservationKey,
		observation.EvidenceSHA256,
		entityKind,
		entityKey,
		fieldName,
		observation.EffectiveAt,
		existingSHA,
		attemptedSHA,
		"KEEP_EXISTING",
	)
	if err != nil {
		return fmt.Errorf("insert reconcile conflict: %w", err)
	}

	return nil
}
