package sourceobservation

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

type channelReconcileSteps[E, S, D any] struct {
	name         string
	subject      func(E) string
	evidence     func(*Observation) (E, error)
	loadState    func(context.Context, dbx.Tx, string) (S, error)
	reduce       func(S, E) (D, error)
	persist      func(context.Context, dbx.Tx, *Observation, *D) error
	applications func(D) []Application
}

func (steps channelReconcileSteps[E, S, D]) reconcile(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (ReconcileResult, error) {
	evidence, err := steps.evidence(claimed)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("%s evidence from observation: %w", steps.name, err)
	}

	subject := steps.subject(evidence)
	if lockErr := lockLiveSubject(ctx, tx, steps.name+":"+subject); lockErr != nil {
		return ReconcileResult{}, fmt.Errorf("lock live subject: %w", lockErr)
	}

	state, err := steps.loadState(ctx, tx, subject)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("load %s state: %w", steps.name, err)
	}

	decision, err := steps.reduce(state, evidence)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("reduce: %w", err)
	}

	if persistErr := steps.persist(ctx, tx, claimed, &decision); persistErr != nil {
		return ReconcileResult{}, fmt.Errorf("persist %s decision: %w", steps.name, persistErr)
	}

	return ReconcileResult{Applications: steps.applications(decision)}, nil
}
