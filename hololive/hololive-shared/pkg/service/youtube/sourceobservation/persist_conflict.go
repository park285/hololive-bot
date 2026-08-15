package sourceobservation

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

func persistReconcileConflict(
	ctx context.Context,
	tx dbx.Tx,
	observation Observation,
	entityKind, entityKey, fieldName, existingSHA, attemptedSHA, decision string,
) error {
	_, err := tx.Exec(
		ctx,
		mustSQL("repository_reconcile_conflict_insert_0061_61.sql"),
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
		decision,
	)
	return err
}
