package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/tracking/observation"
)

func (s *TransitionStore) CompleteSent(
	ctx context.Context,
	operation StartedOperation,
	claimTokens []dispatchstate.ClaimToken,
) (ApplyResult, error) {
	if !operation.Valid() {
		return newApplyResult(ApplyConflict, nil), errors.New("complete sent: invalid operation")
	}

	if err := s.ensureReady(ctx); err != nil {
		return newApplyResult(ApplyIndeterminate, nil), fmt.Errorf("complete sent: %w", err)
	}

	sentAt, err := lifecycle.CanonicalTime(time.Now())
	if err != nil {
		return newApplyResult(ApplyIndeterminate, nil), fmt.Errorf("complete sent: sent at: %w", err)
	}

	transitions, err := completeSentTransitions(operation, sentAt)
	if err != nil {
		return newApplyResult(ApplyConflict, nil), fmt.Errorf("complete sent: build transitions: %w", err)
	}

	var priorAdjudication CommitAdjudication

	for attempt := 0; attempt <= transitionOperationRetryLimit; attempt++ {
		touched, applyErr := s.completeSentOnce(ctx, operation, transitions, claimTokens, sentAt)
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
			result, err := s.confirmSentPostState(ctx, operation, applyErr, decision.result)
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
	applyErr error,
	result ApplyResult,
) (ApplyResult, error) {
	if err := s.validateSentPostState(ctx, operation); err != nil {
		indeterminate := adjudicated(newApplyResult(ApplyIndeterminate, nil), applyErr, CommitMixed)

		return indeterminate, errors.Join(applyErr, err)
	}

	return result, nil
}

func completeSentTransitions(operation StartedOperation, sentAt time.Time) ([]rowTransition, error) {
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
		after.SentAt = &sentAt
		after.Error = ""
		transitions = append(transitions, rowTransition{before: before, after: after})

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
	claimTokens []dispatchstate.ClaimToken,
	sentAt time.Time,
) ([]int64, error) {
	var touched []int64

	err := s.executeTx(ctx, "complete sent", func(tx dbx.Querier) error {
		if err := validateTrackingRequirements(ctx, tx, operation, claimTokens); err != nil {
			return fmt.Errorf("complete sent: validate tracking: %w", err)
		}

		var err error

		touched, err = applyRowTransitions(ctx, tx, "complete sent", transitions)
		if err != nil {
			return fmt.Errorf("complete sent: apply rows: %w", err)
		}

		ownerIDs := startedOwnerIDs(operation)

		marks, err := LoadAlarmSentMarksForDeliveryIDs(ctx, tx, ownerIDs, sentAt, claimTokens)
		if err != nil {
			return fmt.Errorf("complete sent: load tracking marks: %w", err)
		}

		if err := persistSentDeliveryTracking(ctx, tx, marks); err != nil {
			return fmt.Errorf("complete sent: persist tracking: %w", err)
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

		return nil
	})
	if err != nil {
		return touched, fmt.Errorf("complete sent: execute transaction: %w", err)
	}

	return touched, nil
}

func validateTrackingRequirements(
	ctx context.Context,
	tx dbx.Querier,
	operation StartedOperation,
	claimTokens []dispatchstate.ClaimToken,
) error {
	tokens, err := collectClaimTokensByIdentity(claimTokens)
	if err != nil {
		return fmt.Errorf("validate tracking requirements: tokens: %w", err)
	}

	repository := observation.NewRepositoryContext(ctx, tx)
	seen := make(map[string]struct{})

	for i := range operation.groups {
		owner := operation.groups[i].ownerAfter
		if err := validateTrackingOwner(ctx, repository, owner, tokens, seen); err != nil {
			return fmt.Errorf("validate tracking requirements: owner %d: %w", owner.ID, err)
		}
	}

	return nil
}

func validateTrackingOwner(
	ctx context.Context,
	repository *observation.PgxRepository,
	owner transitionRow,
	tokens map[string]time.Time,
	seen map[string]struct{},
) error {
	if !requiresTrackingValidation(owner.Kind) {
		return nil
	}

	canonicalPostID, err := CanonicalDeliveryPostID(owner.Kind, owner.ContentID)
	if err != nil {
		return fmt.Errorf("validate tracking requirements: owner %d identity: %w", owner.ID, err)
	}

	identity := DeliveryClaimIdentityKey(owner.Kind, canonicalPostID)
	if _, ok := seen[identity]; ok {
		return nil
	}

	seen[identity] = struct{}{}

	state, err := repository.FindAlarmStateByPostID(ctx, owner.Kind, canonicalPostID)
	if err != nil {
		return fmt.Errorf("validate tracking requirements: load alarm state: %w", err)
	}

	tracking, err := repository.FindByIdentity(ctx, owner.Kind, owner.ContentID)
	if err != nil {
		return fmt.Errorf("validate tracking requirements: load tracking: %w", err)
	}

	if communityShortsAlarmStateMarkedSent(state) || communityShortsTrackingMarkedSent(tracking) {
		return nil
	}

	if exactTrackingClaimPresent(tokens, identity, state) {
		return nil
	}

	return &transitionConflictError{
		operation: "complete sent", detail: fmt.Sprintf("exact tracking claim is absent for owner %d", owner.ID),
	}
}

func requiresTrackingValidation(kind domain.OutboxKind) bool {
	return kind == domain.OutboxKindCommunityPost || kind == domain.OutboxKindNewShort
}

func exactTrackingClaimPresent(
	tokens map[string]time.Time,
	identity string,
	state *domain.YouTubeCommunityShortsAlarmState,
) bool {
	authorizedAt, ok := tokens[identity]

	return ok && state != nil && state.AuthorizedAt != nil && state.AuthorizedAt.Equal(authorizedAt)
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

func (s *TransitionStore) validateSentPostState(ctx context.Context, operation StartedOperation) error {
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

	if err := validateTrackingRequirements(ctx, s.db, operation, nil); err != nil {
		if conflict, ok := errors.AsType[*transitionConflictError](err); ok {
			return &AtomicityBreachError{Operation: "complete sent", Detail: conflict.detail}
		}

		return fmt.Errorf("complete sent: validate tracking post-state: %w", err)
	}

	return nil
}
