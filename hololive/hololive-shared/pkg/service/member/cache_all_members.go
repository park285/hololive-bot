package member

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/park285/shared-go/v2/pkg/panicguard"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicscope"
)

const (
	allMembersSnapshotTTL         = 5 * time.Minute
	allMembersSnapshotLoadTimeout = 10 * time.Second
	allMembersSnapshotRetryDelay  = time.Minute
)

type allMembersState struct {
	index         *memberSnapshotIndex
	members       []*domain.Member
	loadedAt      time.Time
	retryAfter    time.Time
	loadErr       error
	generation    uint64
	hasSuccessful bool
}

func newAllMembersState(members []*domain.Member, generation uint64, loadedAt time.Time) *allMembersState {
	snapshot, index := newMemberSnapshotIndex(members)

	return &allMembersState{
		index:         index,
		members:       snapshot,
		loadedAt:      loadedAt,
		generation:    generation,
		hasSuccessful: true,
	}
}

var errAllMembersGenerationChanged = errors.New("member snapshot generation changed")

func (c *Cache) LoadAllMembers(ctx context.Context) ([]*domain.Member, error) {
	snap, err := c.membersSnapshot(ctx)
	if err != nil {
		return nil, err
	}

	return cloneMemberSlice(snap.members), nil
}

func (c *Cache) membersSnapshot(ctx context.Context) (*allMembersState, error) {
	if c == nil {
		return nil, errors.New("member cache is nil")
	}

	for {
		if c.cacheBypassRequired("all_members") {
			return c.loadAllMembersBypass(ctx)
		}

		snap, generation := c.allMembersView()
		if outcome, ok := c.cachedSnapshotAt(snap, time.Now()); ok {
			return outcome.result()
		}

		loaded, err := c.loadAllMembersResult(ctx, snap, generation)
		if errors.Is(err, errAllMembersGenerationChanged) {
			continue
		}

		if err != nil {
			return nil, err
		}

		return loaded, nil
	}
}

func (c *Cache) loadAllMembersBypass(ctx context.Context) (*allMembersState, error) {
	loader, err := c.allMembersLoader()
	if err != nil {
		return nil, err
	}

	loadCtx, cancel := context.WithTimeout(ctx, allMembersSnapshotLoadTimeout)
	defer cancel()

	members, err := loader(loadCtx)
	if err != nil {
		return nil, fmt.Errorf("load all members from repository while cache bypassed: %w", err)
	}

	return newAllMembersState(members, 0, time.Now()), nil
}

func (c *Cache) loadAllMembersResult(
	ctx context.Context,
	snap *allMembersState,
	generation uint64,
) (*allMembersState, error) {
	loaded, err := c.loadAllMembersSnapshot(ctx, generation)
	if err == nil {
		return loaded, nil
	}

	if errors.Is(err, errAllMembersGenerationChanged) || ctx.Err() != nil || errors.Is(err, errAllMembersLoadPanicked) {
		return nil, err
	}

	return c.staleAllMembersResult(snap, generation, err)
}

func (c *Cache) staleAllMembersResult(snap *allMembersState, generation uint64, loadErr error) (*allMembersState, error) {
	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()

	if c.snapshotGeneration.Load() != generation {
		return nil, errAllMembersGenerationChanged
	}

	if !snapshotSuccessful(snap) {
		return nil, loadErr
	}

	return snap, nil
}

type snapshotOutcome struct {
	snap *allMembersState
	err  error
}

func (o snapshotOutcome) result() (*allMembersState, error) {
	if o.err != nil {
		return nil, o.err
	}

	if o.snap == nil {
		return nil, errors.New("member snapshot outcome has neither snapshot nor error")
	}

	return o.snap, nil
}

func (c *Cache) cachedSnapshotAt(snap *allMembersState, now time.Time) (snapshotOutcome, bool) {
	if c.snapshotFreshAt(snap, now) {
		return snapshotOutcome{snap: snap}, true
	}

	if !c.snapshotReloadDeferred(snap, now) {
		return snapshotOutcome{}, false
	}

	if snapshotSuccessful(snap) {
		return snapshotOutcome{snap: snap}, true
	}

	return snapshotOutcome{err: snap.loadErr}, true
}

func (c *Cache) snapshotFreshAt(snap *allMembersState, now time.Time) bool {
	if !snapshotSuccessful(snap) {
		return false
	}

	if c.snapshotTTL <= 0 {
		return true
	}

	return now.Sub(snap.loadedAt) < c.snapshotTTL
}

func (*Cache) snapshotReloadDeferred(snap *allMembersState, now time.Time) bool {
	return snap != nil && !snap.retryAfter.IsZero() && now.Before(snap.retryAfter)
}

func (c *Cache) loadAllMembersSnapshot(ctx context.Context, generation uint64) (*allMembersState, error) {
	loader, err := c.allMembersLoader()
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("wait for member snapshot: %w", err)
	}

	groupKey := allMembersSnapshotKey + ":" + strconv.FormatUint(generation, 10)
	sharedCtx := context.WithoutCancel(ctx)

	results := c.allMembersGroup.DoChan(groupKey, func() (any, error) {
		return c.guardedReloadAllMembersSnapshot(sharedCtx, loader, generation)
	})

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for member snapshot: %w", ctx.Err())
	case result := <-results:
		if result.Err != nil {
			return nil, result.Err
		}

		snap, ok := result.Val.(*allMembersState)
		if !ok || snap == nil {
			return nil, fmt.Errorf("unexpected all members result type %T", result.Val)
		}

		return snap, nil
	}
}

var errAllMembersLoadPanicked = errors.New("member snapshot load panicked")

func (c *Cache) guardedReloadAllMembersSnapshot(
	ctx context.Context,
	loader func(context.Context) ([]*domain.Member, error),
	generation uint64,
) (*allMembersState, error) {
	var (
		loaded  *allMembersState
		loadErr error
	)

	if panicErr := panicguard.RunE(c.logger, panicscope.BackgroundTask, "member-snapshot-load", func() error {
		loaded, loadErr = c.reloadAllMembersSnapshot(ctx, loader, generation)

		return nil
	}); panicErr != nil {
		return nil, fmt.Errorf("%w: %w", errAllMembersLoadPanicked, panicErr)
	}

	if loadErr != nil {
		return nil, loadErr
	}

	return loaded, nil
}

func (c *Cache) allMembersLoader() (func(context.Context) ([]*domain.Member, error), error) {
	if c.loadAllMembers != nil {
		return c.loadAllMembers, nil
	}

	if c.repository == nil {
		return nil, errors.New("member repository is nil")
	}

	return c.repository.GetAllMembers, nil
}

func (c *Cache) reloadAllMembersSnapshot(
	ctx context.Context,
	loader func(context.Context) ([]*domain.Member, error),
	generation uint64,
) (*allMembersState, error) {
	current, currentGeneration := c.allMembersView()
	if currentGeneration != generation {
		return nil, errAllMembersGenerationChanged
	}

	if outcome, ok := c.cachedSnapshotAt(current, time.Now()); ok {
		return outcome.result()
	}

	loadCtx, cancel := context.WithTimeout(ctx, allMembersSnapshotLoadTimeout)
	defer cancel()

	members, err := loader(loadCtx)
	if err != nil {
		return nil, c.handleAllMembersLoadFailure(loadCtx, current, generation, err)
	}

	if err := c.confirmEpochAfterLoad(loadCtx, generation); err != nil {
		return nil, fmt.Errorf("confirm epoch after load: %w", err)
	}

	published := c.storeAllMembersSnapshot(current, generation, members)
	if published == nil {
		return nil, errAllMembersGenerationChanged
	}

	c.logAllMembersSnapshotRecovery(current, len(published.members))

	return published, nil
}

func (c *Cache) handleAllMembersLoadFailure(
	ctx context.Context,
	current *allMembersState,
	generation uint64,
	loadFailure error,
) error {
	loadErr := fmt.Errorf("load all members from repository: %w", loadFailure)

	if err := c.confirmEpochAfterLoad(ctx, generation); err != nil {
		return fmt.Errorf("confirm epoch after load: %w", err)
	}

	if !c.deferAllMembersSnapshotReload(current, generation, loadErr) {
		return errAllMembersGenerationChanged
	}

	return loadErr
}

func (c *Cache) deferAllMembersSnapshotReload(snap *allMembersState, generation uint64, err error) bool {
	retryAfter := time.Now().Add(allMembersSnapshotRetryDelay)
	deferred := &allMembersState{
		retryAfter: retryAfter,
		loadErr:    err,
		generation: generation,
	}

	if snapshotSuccessful(snap) {
		deferred.index = snap.index
		deferred.members = snap.members
		deferred.loadedAt = snap.loadedAt
		deferred.hasSuccessful = true
	}

	c.snapshotMu.Lock()

	retryScheduled := c.snapshotGeneration.Load() == generation && c.allMembersSnapshot.Load() == snap
	if retryScheduled {
		c.allMembersSnapshot.Store(deferred)
	}

	c.snapshotMu.Unlock()

	if c.logger != nil {
		c.logger.Warn("member_snapshot_reload_failed",
			slog.Bool("stale_available", snapshotSuccessful(deferred)),
			slog.Bool("retry_scheduled", retryScheduled),
			slog.Time("retry_after", retryAfter),
			slog.Any("error", err),
		)
	}

	return retryScheduled
}

func (c *Cache) logAllMembersSnapshotRecovery(snap *allMembersState, memberCount int) {
	if snap == nil || snap.retryAfter.IsZero() || c.logger == nil {
		return
	}

	c.logger.Info("member_snapshot_reload_recovered", slog.Int("member_count", memberCount))
}

func (c *Cache) storeAllMembersSnapshot(previous *allMembersState, generation uint64, members []*domain.Member) *allMembersState {
	nextGeneration := generation + 1
	next := newAllMembersState(members, nextGeneration, time.Now())

	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()

	if c.snapshotGeneration.Load() != generation || c.allMembersSnapshot.Load() != previous {
		return nil
	}

	c.pointOverlay = nil
	c.snapshotGeneration.Store(nextGeneration)
	c.allMembersSnapshot.Store(next)

	return next
}

func (c *Cache) allMembersView() (snapshot *allMembersState, generation uint64) {
	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()

	return c.allMembersSnapshot.Load(), c.snapshotGeneration.Load()
}

func snapshotSuccessful(snap *allMembersState) bool {
	return snap != nil && snap.hasSuccessful
}

func cloneMemberSlice(in []*domain.Member) []*domain.Member {
	if len(in) == 0 {
		return []*domain.Member{}
	}

	out := make([]*domain.Member, len(in))
	copy(out, in)

	return out
}
