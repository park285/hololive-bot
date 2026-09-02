package collectorruntime

import (
	"fmt"
	"sync"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type BoundedCount struct {
	Value  int
	Capped bool
}

type handoffStatus struct {
	ObservationID int64
	Status        *contract.Status
}

type handoffTracker struct {
	completed  bool
	candidates []int64
}

type readinessTracker struct {
	mu                sync.Mutex
	collectionSuccess bool
	handoff           handoffTracker
}

type readinessTrackerSnapshot struct {
	collectionSuccess bool
	handoffCompleted  bool
	candidateIDs      []int64
}

func (t *readinessTracker) ObserveCollectionSuccess() {
	if t == nil {
		return
	}

	t.mu.Lock()

	t.collectionSuccess = true
	t.mu.Unlock()
}

func (t *readinessTracker) AddHandoffCandidates(ids ...int64) {
	if t == nil {
		return
	}

	t.mu.Lock()
	t.handoff.Add(ids...)
	t.mu.Unlock()
}

func (t *readinessTracker) Snapshot() readinessTrackerSnapshot {
	if t == nil {
		return readinessTrackerSnapshot{}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	completed, ids := t.handoff.Snapshot()

	return readinessTrackerSnapshot{
		collectionSuccess: t.collectionSuccess,
		handoffCompleted:  completed,
		candidateIDs:      ids,
	}
}

func (t *readinessTracker) ApplyHandoff(snap readinessTrackerSnapshot, statuses []handoffStatus) (HandoffState, error) {
	if t == nil {
		return HandoffNone, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "apply observation handoff: tracker is nil")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	out, err := t.handoff.ApplySnapshot(snap.candidateIDs, statuses)
	if err != nil {
		return out, fmt.Errorf("apply snapshot: %w", err)
	}

	return out, nil
}

func (t *handoffTracker) Add(ids ...int64) {
	if t.completed {
		return
	}

	seen := make(map[int64]struct{}, len(t.candidates)+len(ids))
	for _, id := range t.candidates {
		seen[id] = struct{}{}
	}

	for _, id := range ids {
		if id <= 0 {
			continue
		}

		if _, exists := seen[id]; exists {
			continue
		}

		seen[id] = struct{}{}
		t.candidates = append(t.candidates, id)
	}

	if extra := len(t.candidates) - maxHandoffCandidates; extra > 0 {
		t.candidates = append([]int64(nil), t.candidates[extra:]...)
	}
}

func (t *handoffTracker) Snapshot() (completed bool, ids []int64) {
	if len(t.candidates) > 0 {
		ids = append([]int64(nil), t.candidates...)
	}

	return t.completed, ids
}

func (t *handoffTracker) ApplySnapshot(ids []int64, statuses []handoffStatus) (HandoffState, error) {
	if t.completed {
		return HandoffProcessed, nil
	}

	if err := requireHandoffShape(ids, statuses); err != nil {
		return HandoffNone, fmt.Errorf("require handoff shape: %w", err)
	}

	aggregate, processed, keep, err := classifyHandoffStatuses(statuses)
	if err != nil {
		return HandoffNone, fmt.Errorf("classify handoff statuses: %w", err)
	}

	if processed {
		t.completed = true
		t.candidates = nil

		return HandoffProcessed, nil
	}

	t.retainHandoffCandidates(keep)

	return aggregate, nil
}

func classifyHandoffStatuses(statuses []handoffStatus) (
	state HandoffState,
	processed bool,
	keep map[int64]bool,
	err error,
) {
	keep = make(map[int64]bool, len(statuses))

	aggregate := HandoffNone

	for _, item := range statuses {
		state, drop, err := classifyHandoffRow(item)
		if err != nil {
			return HandoffNone, false, nil, fmt.Errorf("classify handoff row: %w", err)
		}

		if state == HandoffProcessed {
			processed = true
			continue
		}

		keep[item.ObservationID] = !drop
		aggregate = higherHandoff(aggregate, state)
	}

	return aggregate, processed, keep, nil
}

func (t *handoffTracker) retainHandoffCandidates(keep map[int64]bool) {
	kept := t.candidates[:0]
	for _, id := range t.candidates {
		if retain, observed := keep[id]; observed && !retain {
			continue
		}

		kept = append(kept, id)
	}

	t.candidates = kept
}

func requireHandoffShape(ids []int64, statuses []handoffStatus) error {
	if len(statuses) != len(ids) {
		return collecterr.New(collecterr.Internal, collecterr.ClassInternal, "observation handoff status row count is invalid")
	}

	for i, item := range statuses {
		if item.ObservationID != ids[i] {
			return collecterr.New(collecterr.Internal, collecterr.ClassInternal, "observation handoff status order is invalid")
		}
	}

	return nil
}

func classifyHandoffRow(item handoffStatus) (HandoffState, bool, error) {
	if item.Status == nil {
		return HandoffNone, true, nil
	}

	status := *item.Status
	if status == contract.StatusProcessed {
		return HandoffProcessed, false, nil
	}

	if status == contract.StatusDeadLetter {
		return HandoffNone, true, nil
	}

	if status == contract.StatusPending {
		return HandoffPending, false, nil
	}

	if status == contract.StatusProcessing {
		return HandoffProcessing, false, nil
	}

	return HandoffNone, false, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "observation handoff status is unknown")
}

func higherHandoff(current, next HandoffState) HandoffState {
	if handoffRank(next) > handoffRank(current) {
		return next
	}

	return current
}

func handoffRank(state HandoffState) int {
	if state == HandoffProcessed {
		return 4
	}

	if state == HandoffProcessing {
		return 3
	}

	if state == HandoffPending {
		return 2
	}

	return 1
}
