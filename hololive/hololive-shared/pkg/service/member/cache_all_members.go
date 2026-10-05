// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
)

const (
	// Durable epoch가 freshness를 소유하고, 이 TTL은 같은 epoch 안의 정기 DB refresh 상한을 둔다.
	allMembersSnapshotTTL         = 5 * time.Minute
	allMembersSnapshotLoadTimeout = 10 * time.Second
	allMembersSnapshotRetryDelay  = time.Minute
)

// allMembersState는 게시 단위다. 성공 상태는 members와 그 불변 색인 index를 함께 갖고, 재시도 대기 상태는 직전 성공
// 상태의 members·index를 그대로 공유한다.
type allMembersState struct {
	index         *memberSnapshotIndex
	members       []*domain.Member
	loadedAt      time.Time
	retryAfter    time.Time
	loadErr       error
	generation    uint64
	hasSuccessful bool
}

// newAllMembersState는 적재 결과로 성공 상태를 만든다. Nil 멤버는 snapshot에서 뺀다.
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

// AllMembers는 전체 멤버 목록을 돌려준다. 돌려준 slice는 호출자 소유다. 공유 적재는 호출자 취소와 분리된 상한 안에서
// 끝까지 진행하고, 기다리던 호출자만 자기 ctx가 끝나면 즉시 ctx 오류로 빠진다.
func (c *Cache) AllMembers(ctx context.Context) ([]*domain.Member, error) {
	snap, err := c.membersSnapshot(ctx, "all_members")
	if err != nil {
		return nil, err
	}

	return cloneMemberSlice(snap.members), nil
}

// membersSnapshot은 전체 멤버 조회와 다건 조회가 함께 쓰는 snapshot을 돌려준다. Epoch이 불확실하면 게시하지 않는 일회용
// 상태를 PostgreSQL에서 만들어 같은 색인 규칙으로 답한다.
func (c *Cache) membersSnapshot(ctx context.Context, operation string) (*allMembersState, error) {
	if c == nil {
		return nil, errors.New("member cache is nil")
	}

	for {
		if c.cacheBypassRequired(operation) {
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

// loadAllMembersBypass는 epoch 불확실 구간의 직접 조회다. 공유 적재가 아니므로 호출자 취소를 그대로 따르고, 같은
// 상한 시간을 둔다.
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

// loadAllMembersResult는 공유 적재 결과를 돌려준다. ErrAllMembersGenerationChanged는 새 generation에서 다시 시도하라는
// 신호다. 공유 적재가 실패하면 같은 generation의 직전 성공 snapshot을 stale 결과로 쓴다.
func (c *Cache) loadAllMembersResult(
	ctx context.Context,
	snap *allMembersState,
	generation uint64,
) (*allMembersState, error) {
	loaded, err := c.loadAllMembersSnapshot(ctx, generation)
	if err == nil {
		return loaded, nil
	}

	// 호출자 취소는 공유 적재 실패가 아니므로 stale snapshot으로 바꾸지 않는다. 공유 적재 panic은 결함이므로 stale
	// 성공으로 가리지 않고 그대로 실패로 돌려준다.
	if errors.Is(err, errAllMembersGenerationChanged) || ctx.Err() != nil || errors.Is(err, errAllMembersLoadPanicked) {
		return nil, err
	}

	return c.staleAllMembersResult(snap, generation, err)
}

// staleAllMembersResult는 generation이 그대로이고 직전 성공 snapshot이 있으면 그것을, generation이 바뀌었으면
// errAllMembersGenerationChanged를, 아니면 적재 오류를 돌려준다.
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

// snapshotOutcome은 적재 없이 낼 수 있는 응답이다. 성공 snapshot 또는 재시도 대기 중인 적재 오류 중 하나다.
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

// cachedSnapshotAt은 적재 없이 답할 수 있으면 ok=true와 응답을 돌려준다. 신선한 snapshot, 재시도 대기 중인 stale
// snapshot, 재시도 대기 중인 cold 적재 오류가 여기에 해당한다.
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

// loadAllMembersSnapshot은 generation별 공유 적재를 시작하거나 합류해 결과를 기다린다. 공유 적재는 첫 호출자 ctx의 값만
// 물려받고 취소와는 분리되며 allMembersSnapshotLoadTimeout 상한을 갖는다. 기다리는 호출자는 각자 ctx가 끝나면 다른
// 대기자와 공유 적재에 영향 없이 빠진다.
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

// errAllMembersLoadPanicked는 공유 적재가 panic으로 끝났다는 실패다. Stale snapshot으로 대체하지 않는다.
var errAllMembersLoadPanicked = errors.New("member snapshot load panicked")

// guardedReloadAllMembersSnapshot은 공유 적재를 panic 경계 안에서 실행한다. Singleflight.DoChan은 fn panic을 새
// goroutine에서 다시 던져 어떤 호출자도 복구할 수 없게 하므로(프로세스 종료), 여기서 복구해 명시적 오류로 바꾼다.
// Panic은 snapshot 상태를 바꾸지 않으므로 다음 조회가 다시 적재를 시도한다.
func (c *Cache) guardedReloadAllMembersSnapshot(
	ctx context.Context,
	loader func(context.Context) ([]*domain.Member, error),
	generation uint64,
) (*allMembersState, error) {
	var (
		loaded  *allMembersState
		loadErr error
	)

	if panicErr := panicguard.RunE(c.logger, panicguard.BackgroundTask, "member-snapshot-load", func() error {
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

// handleAllMembersLoadFailure는 항상 오류를 돌려준다. 같은 generation이면 재시도 대기 상태를 게시하고 적재 오류를,
// 그 사이 generation이 바뀌었으면 재시도 신호를 돌려준다.
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

// storeAllMembersSnapshot은 색인을 잠금 밖에서 만든 뒤, 적재를 시작한 generation과 이전 상태가 그대로일 때만 새
// snapshot을 다음 generation으로 게시한다. Generation이 오르므로 이전 point overlay는 함께 무효가 된다. 게시하지
// 못하면 nil이다.
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
