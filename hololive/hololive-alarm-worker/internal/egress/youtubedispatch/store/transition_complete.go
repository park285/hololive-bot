package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/tracking/observation"
)

func (s *TransitionStore) CompleteSent(
	ctx context.Context,
	operation StartedOperation,
	admission HandoffAdmission,
) (ApplyResult, error) {
	if !operation.Valid() {
		return newApplyResult(ApplyConflict, nil), errors.New("complete sent: invalid operation")
	}

	if err := admission.validate(); err != nil {
		return newApplyResult(ApplyConflict, nil), fmt.Errorf("complete sent: %w", err)
	}

	transitions, err := completeSentTransitions(operation)
	if err != nil {
		return newApplyResult(ApplyConflict, nil), fmt.Errorf("complete sent: build transitions: %w", err)
	}

	var priorAdjudication CommitAdjudication

	for attempt := 0; attempt <= transitionOperationRetryLimit; attempt++ {
		touched, applyErr := s.completeSentOnce(ctx, operation, transitions, admission)
		if applyErr == nil {
			result := newApplyResult(ApplyApplied, touched)

			result.CommitAdjudication = priorAdjudication

			return result, nil
		}

		decision := s.adjudicateRowApply(
			ctx,
			"complete sent",
			"delivery group contains mixed pre-state and SENT post-state",
			transitions,
			nil,
			applyErr,
			priorAdjudication,
			attempt,
		)
		if decision.retry {
			priorAdjudication = decision.prior

			continue
		}

		if decision.result.Outcome == ApplyApplied {
			result, err := s.confirmSentPostState(ctx, operation, admission, applyErr, decision.result)
			if err != nil {
				return result, fmt.Errorf("complete sent: confirm post-state: %w", err)
			}

			return result, nil
		}

		return decision.result, decision.err
	}

	return newApplyResult(ApplyIndeterminate, nil), errors.New("complete sent: retry loop exhausted")
}

func (s *TransitionStore) confirmSentPostState(
	ctx context.Context,
	operation StartedOperation,
	admission HandoffAdmission,
	applyErr error,
	result ApplyResult,
) (ApplyResult, error) {
	if err := s.validateSentPostState(ctx, operation, admission); err != nil {
		indeterminate := adjudicated(newApplyResult(ApplyIndeterminate, nil), applyErr, CommitMixed)

		return indeterminate, errors.Join(applyErr, err)
	}

	return result, nil
}

func completeSentTransitions(operation StartedOperation) ([]rowTransition, error) {
	groups := sortedStartedGroups(operation.groups)
	transitions := make([]rowTransition, 0, len(groups))
	seen := make(map[int64]struct{})

	appendSent := func(before transitionRow) error {
		if _, ok := seen[before.ID]; ok {
			return fmt.Errorf("complete sent: duplicate delivery %d", before.ID)
		}

		seen[before.ID] = struct{}{}
		if before.Status == lifecycle.StatusSent {
			return fmt.Errorf("complete sent: delivery %d is already SENT in the immutable envelope", before.ID)
		}

		after := before

		after.Status = lifecycle.StatusSent
		after.RowVersion++

		after.LockedAt = nil
		after.LockedBy = nil
		after.SentAt = nil
		after.Error = ""
		transitions = append(transitions, rowTransition{before: before, after: after, sentFromClock: true})

		return nil
	}

	for i := range groups {
		if groups[i].ownerAfter.Status != lifecycle.StatusSending || groups[i].ownerAfter.LockedAt == nil {
			return nil, fmt.Errorf("complete sent: owner %d has no send fence", groups[i].ownerAfter.ID)
		}

		if err := appendSent(groups[i].ownerAfter); err != nil {
			return nil, fmt.Errorf("complete sent: append owner transition: %w", err)
		}

		for j := range groups[i].followers {
			if err := appendSent(groups[i].followers[j]); err != nil {
				return nil, fmt.Errorf("complete sent: append follower transition: %w", err)
			}
		}
	}

	sortTransitionsByID(transitions)

	return transitions, nil
}

func (s *TransitionStore) completeSentOnce(
	ctx context.Context,
	operation StartedOperation,
	transitions []rowTransition,
	admission HandoffAdmission,
) ([]int64, error) {
	var touched []int64

	err := s.executeTx(ctx, "complete sent", func(tx dbx.Querier) error {
		var (
			sentAt time.Time
			err    error
		)

		if err = lockAdmissionRequest(ctx, tx, operation, admission); err != nil {
			return fmt.Errorf("complete sent: %w", err)
		}

		touched, sentAt, err = applyRowTransitions(ctx, tx, "complete sent", transitions)
		if err != nil {
			return fmt.Errorf("complete sent: apply rows: %w", err)
		}

		if err := recordSentTracking(ctx, tx, operation, sentAt); err != nil {
			return err
		}

		writes := make([]LedgerWrite, 0, len(operation.groups))
		for i := range operation.groups {
			writes = append(writes, LedgerWrite{
				Key: operation.groups[i].key, ObservedAt: sentAt,
				SourceDeliveryID: operation.groups[i].ownerAfter.ID,
			})
		}

		if err := RecordDeliveryLedgerWrites(ctx, tx, LedgerStatusSent, writes); err != nil {
			return fmt.Errorf("complete sent: record ledger: %w", err)
		}

		if err := recordAdmission(ctx, tx, admission, sentAt); err != nil {
			return fmt.Errorf("complete sent: %w", err)
		}

		return nil
	})
	if err != nil {
		return touched, fmt.Errorf("complete sent: execute transaction: %w", err)
	}

	return touched, nil
}

func recordSentTracking(
	ctx context.Context,
	tx dbx.Querier,
	operation StartedOperation,
	sentAt time.Time,
) error {
	marks, err := LoadAlarmSentMarksForDeliveryIDs(ctx, tx, startedOwnerIDs(operation), sentAt)
	if err != nil {
		return fmt.Errorf("complete sent: load tracking marks: %w", err)
	}

	if err := markSentDeliveryTracking(ctx, tx, marks); err != nil {
		return fmt.Errorf("complete sent: persist tracking: %w", err)
	}

	return nil
}

func validateSentTracking(ctx context.Context, db dbx.Querier, operation StartedOperation) error {
	repository := observation.NewRepository(db)
	seen := make(map[string]struct{})

	for i := range operation.groups {
		owner := operation.groups[i].ownerAfter
		if err := validateSentTrackingOwner(ctx, repository, owner, seen); err != nil {
			return fmt.Errorf("validate sent tracking: owner %d: %w", owner.ID, err)
		}
	}

	return nil
}

func validateSentTrackingOwner(
	ctx context.Context,
	repository *observation.PgxRepository,
	owner transitionRow,
	seen map[string]struct{},
) error {
	if owner.Kind != domain.OutboxKindCommunityPost && owner.Kind != domain.OutboxKindNewShort {
		return nil
	}

	canonicalPostID, err := CanonicalDeliveryPostID(owner.Kind, owner.ContentID)
	if err != nil {
		return fmt.Errorf("owner %d identity: %w", owner.ID, err)
	}

	identity := string(owner.Kind) + "\x00" + canonicalPostID
	if _, ok := seen[identity]; ok {
		return nil
	}

	seen[identity] = struct{}{}

	state, err := repository.FindAlarmStateByPostID(ctx, owner.Kind, canonicalPostID)
	if err != nil {
		return fmt.Errorf("load alarm state: %w", err)
	}

	tracking, err := repository.FindByIdentity(ctx, owner.Kind, owner.ContentID)
	if err != nil {
		return fmt.Errorf("load tracking: %w", err)
	}

	if communityShortsAlarmStateMarkedSent(state) || communityShortsTrackingMarkedSent(tracking) {
		return nil
	}

	return &AtomicityBreachError{
		Operation: "complete sent", Detail: fmt.Sprintf("post-level sent mark is absent for owner %d", owner.ID),
	}
}

func communityShortsAlarmStateMarkedSent(state *domain.YouTubeCommunityShortsAlarmState) bool {
	return state != nil && state.AlarmSentAt != nil && !state.AlarmSentAt.IsZero()
}

func communityShortsTrackingMarkedSent(tracking *domain.YouTubeContentAlarmTracking) bool {
	return tracking != nil && tracking.AlarmSentAt != nil && !tracking.AlarmSentAt.IsZero()
}

func startedOwnerIDs(operation StartedOperation) []int64 {
	ids := make([]int64, 0, len(operation.groups))
	for i := range operation.groups {
		ids = append(ids, operation.groups[i].ownerAfter.ID)
	}

	return uniqueSortedInt64s(ids)
}

func (s *TransitionStore) validateSentPostState(ctx context.Context, operation StartedOperation, admission HandoffAdmission) error {
	keys := make([]ytcontentid.LogicalKey, 0, len(operation.groups))
	for i := range operation.groups {
		keys = append(keys, operation.groups[i].key)
	}

	ledger, _, err := loadTransitionLedger(ctx, s.db, keys)
	if err != nil {
		return fmt.Errorf("complete sent: read-back ledger: %w", err)
	}

	for i := range operation.groups {
		record, ok := ledger[operation.groups[i].key]
		if !ok || record.Status != LedgerStatusSent || record.SentAt == nil {
			return &AtomicityBreachError{
				Operation: "complete sent", Detail: fmt.Sprintf("SENT ledger is absent for key %s", operation.groups[i].key.Hash()),
			}
		}
	}

	if err := validateSentTracking(ctx, s.db, operation); err != nil {
		return fmt.Errorf("complete sent: validate tracking post-state: %w", err)
	}

	if err := validateAdmissionPostState(ctx, s.db, admission); err != nil {
		return fmt.Errorf("complete sent: validate handoff admission post-state: %w", err)
	}

	return nil
}
