package kakaoroom

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/constants"
)

type catalogListerFunc func(context.Context) ([]Facts, error)

func (f catalogListerFunc) GetRooms(ctx context.Context) ([]Facts, error) {
	return f(ctx)
}

type catalogStoreFuncs struct {
	getFunc    func(context.Context, string) (Facts, bool, error)
	upsertFunc func(context.Context, Facts) error
}

func (s catalogStoreFuncs) get(ctx context.Context, roomID string) (Facts, bool, error) {
	if s.getFunc == nil {
		return Facts{}, false, nil
	}

	return s.getFunc(ctx, roomID)
}

func (s catalogStoreFuncs) upsert(ctx context.Context, facts Facts) error {
	if s.upsertFunc == nil {
		return nil
	}

	return s.upsertFunc(ctx, facts)
}

func startCatalogLookup(ctx context.Context, c *Catalog, roomID string) <-chan bool {
	done := make(chan bool, 1)

	go func() {
		done <- c.OpenChat(ctx, roomID)
	}()

	return done
}

func TestCatalogRefreshCoalescesListingAndPersistenceAcrossRooms(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rooms := make([]Facts, 100)
		for i := range rooms {
			rooms[i] = Facts{RoomID: fmt.Sprint(i), RoomType: "OM"}
		}

		listRelease := make(chan struct{})
		storeRelease := make(chan struct{})
		listCalls, storeCalls := 0, 0
		c := newCoalescingCatalog(rooms, listRelease, storeRelease, &listCalls, &storeCalls)

		leader := startCatalogLookup(t.Context(), c, "missing")

		synctest.Wait()

		followers := make([]<-chan bool, 20)
		for i := range followers {
			followers[i] = startCatalogLookup(t.Context(), c, fmt.Sprint(i%10))
		}

		synctest.Wait()
		require.Equal(t, 1, listCalls)
		require.Zero(t, storeCalls)

		close(listRelease)
		synctest.Wait()
		require.Equal(t, 1, storeCalls)

		// HTTP가 끝난 뒤 들어온 다른 miss도 진행 중인 저장 순회에 합류한다.
		late := startCatalogLookup(t.Context(), c, "also-missing")

		synctest.Wait()
		require.Equal(t, 1, listCalls)
		require.Equal(t, 1, storeCalls)

		select {
		case <-leader:
			t.Fatal("refresh returned before its persistence pass completed")
		default:
		}

		close(storeRelease)
		synctest.Wait()
		require.False(t, <-leader)
		require.False(t, <-late)

		for _, done := range followers {
			require.True(t, <-done)
		}

		require.Equal(t, 1, listCalls)
		require.Equal(t, len(rooms), storeCalls)
	})
}

func TestCatalogRefreshFindsLaterRoomWithoutNegativeCache(t *testing.T) {
	t.Parallel()

	rooms := make([]Facts, 1, 2)

	rooms[0] = Facts{RoomID: "existing", RoomType: "OM"}

	listCalls, storeCalls := 0, 0
	c := New(nil, catalogListerFunc(func(context.Context) ([]Facts, error) {
		listCalls++

		return rooms, nil
	}), nil)

	c.store = catalogStoreFuncs{upsertFunc: func(context.Context, Facts) error {
		storeCalls++

		return nil
	}}

	require.False(t, c.OpenChat(t.Context(), "later"))

	rooms = append(rooms, Facts{RoomID: "later", RoomType: "OM"})

	require.True(t, c.OpenChat(t.Context(), "later"))
	require.Equal(t, 2, listCalls)
	require.Equal(t, 3, storeCalls)
}

func TestCatalogRefreshIsScopedToCatalog(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		calls := 0
		lister := catalogListerFunc(func(ctx context.Context) ([]Facts, error) {
			calls++

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
			}

			return []Facts{{RoomID: "room", RoomType: "OM"}}, nil
		})
		first := startCatalogLookup(t.Context(), New(nil, lister, nil), "room")

		synctest.Wait()

		second := startCatalogLookup(t.Context(), New(nil, lister, nil), "room")

		synctest.Wait()
		require.Equal(t, 2, calls)
		close(release)
		synctest.Wait()
		require.True(t, <-first)
		require.True(t, <-second)
	})
}

func TestCatalogRefreshWaitersCancelIndependently(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"initiator", "follower", "initiator_deadline"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runCatalogWaiterCancellation(t, name)
		})
	}
}

func TestCatalogRefreshTimeoutPreservesRememberedFactsForHealthyWaiter(t *testing.T) {
	t.Parallel()

	for _, blockedWrite := range []int{1, 2} {
		t.Run(fmt.Sprintf("blocked_write_%d", blockedWrite), func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				rooms := []Facts{
					{RoomID: "requested", RoomType: "OM"},
					{RoomID: "unrelated", RoomType: "DirectChat"},
					{RoomID: "unvisited", RoomType: "OM"},
				}
				writes := 0
				c := New(nil, catalogListerFunc(func(context.Context) ([]Facts, error) {
					return rooms, nil
				}), nil)

				c.store = catalogStoreFuncs{upsertFunc: func(ctx context.Context, _ Facts) error {
					writes++
					if writes == blockedWrite {
						<-ctx.Done()

						return ctx.Err()
					}

					return nil
				}}

				healthy := startCatalogLookup(t.Context(), c, "requested")

				synctest.Wait()

				facts, remembered := c.cached("requested")
				require.True(t, remembered)
				require.True(t, facts.OpenChat())

				waiterCtx, cancel := context.WithCancel(t.Context())
				defer cancel()

				canceled := startCatalogLookup(waiterCtx, c, "unvisited")

				synctest.Wait()
				cancel()
				synctest.Wait()
				require.False(t, <-canceled)

				time.Sleep(constants.QueryTimeout.Default)
				synctest.Wait()
				require.True(t, <-healthy, "유효한 관측 사실을 다른 저장의 timeout 때문에 plain으로 바꾸면 안 된다")
				require.Equal(t, blockedWrite, writes)

				_, visited := c.cached("unvisited")
				require.False(t, visited)
			})
		})
	}
}

func TestCatalogRefreshOwnerTimeoutCoversWholePassAndAllowsRetry(t *testing.T) {
	t.Parallel()

	for _, phase := range []string{"listing", "persistence"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			runCatalogOwnerTimeout(t, phase)
		})
	}
}

func TestCatalogRefreshListingFailureAllowsLaterRefresh(t *testing.T) {
	t.Parallel()

	calls := 0
	c := New(nil, catalogListerFunc(func(context.Context) ([]Facts, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("listing failed")
		}

		return []Facts{{RoomID: "target", RoomType: "OM"}}, nil
	}), nil)

	require.False(t, c.OpenChat(t.Context(), "target"))
	require.True(t, c.OpenChat(t.Context(), "target"))
	require.Equal(t, 2, calls)
}

func TestCatalogRefreshPersistenceFailurePreservesMemoryAndAllowsLaterPass(t *testing.T) {
	t.Parallel()

	rooms := []Facts{{RoomID: "first", RoomType: "OM"}, {RoomID: "second", RoomType: "OM"}}
	listCalls, storeCalls := 0, 0
	persisted := make(map[string]Facts)
	c := New(nil, catalogListerFunc(func(context.Context) ([]Facts, error) {
		listCalls++

		return rooms, nil
	}), nil)

	c.store = catalogStoreFuncs{upsertFunc: func(_ context.Context, facts Facts) error {
		storeCalls++

		if listCalls == 1 {
			return errors.New("persistence failed")
		}

		persisted[facts.RoomID] = facts

		return nil
	}}

	require.True(t, c.OpenChat(t.Context(), "first"), "usable memory remains available after a write failure")
	require.True(t, c.OpenChat(t.Context(), "second"))
	require.Equal(t, 2, storeCalls, "ordinary store errors must not abandon remaining rooms")
	require.Empty(t, persisted)
	require.False(t, c.OpenChat(t.Context(), "missing"))
	require.Equal(t, 2, listCalls)
	require.Equal(t, 4, storeCalls)
	require.Len(t, persisted, 2)
}

func TestCatalogObserveRetriesPersistenceDespiteIdenticalMemory(t *testing.T) {
	t.Parallel()

	calls := 0

	var persisted Facts

	c := New(nil, nil, nil)

	c.store = catalogStoreFuncs{upsertFunc: func(_ context.Context, facts Facts) error {
		calls++
		if calls == 1 {
			return errors.New("persistence failed")
		}

		persisted = facts

		return nil
	}}

	c.Observe(t.Context(), "room", "OM", "link")
	require.True(t, c.OpenChat(t.Context(), "room"))
	require.Empty(t, persisted)
	c.Observe(t.Context(), "room", "OM", "link")
	require.Equal(t, 2, calls)
	require.Equal(t, Facts{RoomID: "room", RoomType: "OM", RoomLinkID: "link"}, persisted)
}

func TestCatalogObservedMetadataResolvesWithoutRefresh(t *testing.T) {
	t.Parallel()

	listCalls := 0
	c := New(nil, catalogListerFunc(func(context.Context) ([]Facts, error) {
		listCalls++

		return []Facts{{RoomID: "plain", RoomType: "OM"}, {RoomID: "open", RoomType: "DirectChat"}}, nil
	}), nil)
	c.Observe(t.Context(), "plain", "DirectChat", "")
	c.Observe(t.Context(), "open", "OM", "")
	require.False(t, c.OpenChat(t.Context(), "plain"))
	require.True(t, c.OpenChat(t.Context(), "open"))
	require.Zero(t, listCalls)
}

func newCoalescingCatalog(rooms []Facts, listRelease, storeRelease <-chan struct{}, listCalls, storeCalls *int) *Catalog {
	c := New(nil, catalogListerFunc(func(ctx context.Context) ([]Facts, error) {
		(*listCalls)++

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-listRelease:
		}

		return rooms, nil
	}), nil)

	c.store = catalogStoreFuncs{upsertFunc: func(ctx context.Context, _ Facts) error {
		(*storeCalls)++
		if *storeCalls == 1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-storeRelease:
			}
		}

		return nil
	}}

	return c
}

func runCatalogWaiterCancellation(t *testing.T, name string) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		listCalls, storeCalls := 0, 0

		var ownerErr func() error

		c := New(nil, catalogListerFunc(func(ctx context.Context) ([]Facts, error) {
			listCalls++

			ownerErr = ctx.Err

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
			}

			return []Facts{{RoomID: "room", RoomType: "OM"}}, nil
		}), nil)

		c.store = catalogStoreFuncs{upsertFunc: func(ctx context.Context, _ Facts) error {
			storeCalls++

			return ctx.Err()
		}}

		leaderCtx, followerCtx, cancel := catalogWaiterContexts(t, name)
		defer cancel()

		leader := startCatalogLookup(leaderCtx, c, "room")

		synctest.Wait()

		follower := startCatalogLookup(followerCtx, c, "room")

		synctest.Wait()

		if name == "initiator_deadline" {
			time.Sleep(time.Second)
		} else {
			cancel()
		}

		synctest.Wait()

		canceled, healthy := leader, follower

		if name == "follower" {
			canceled, healthy = follower, leader
		}

		require.False(t, <-canceled)
		require.NotNil(t, ownerErr)
		require.NoError(t, ownerErr())
		require.Equal(t, 1, listCalls)

		select {
		case <-healthy:
			t.Fatal("healthy waiter stopped with another waiter's cancellation")
		default:
		}

		close(release)
		synctest.Wait()
		require.True(t, <-healthy)
		require.Equal(t, 1, listCalls)
		require.Equal(t, 1, storeCalls)
	})
}

func runCatalogOwnerTimeout(t *testing.T, phase string) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		rooms := []Facts{{RoomID: "first", RoomType: "OM"}, {RoomID: "target", RoomType: "OM"}}
		listCalls, storeCalls := 0, 0

		var (
			deadline    time.Time
			hasDeadline bool
			ownerErr    func() error
		)

		ctx := t.Context()
		c := New(nil, catalogListerFunc(func(listingCtx context.Context) ([]Facts, error) {
			listCalls++
			if listCalls == 1 {
				deadline, hasDeadline = listingCtx.Deadline()
				ownerErr = listingCtx.Err

				if phase == "listing" {
					<-listingCtx.Done()

					return nil, listingCtx.Err()
				}

				time.Sleep(2 * time.Second)
			}

			return rooms, nil
		}), nil)

		c.store = catalogStoreFuncs{upsertFunc: func(ctx context.Context, _ Facts) error {
			storeCalls++

			if listCalls == 1 {
				<-ctx.Done()

				return ctx.Err()
			}

			return nil
		}}

		started := time.Now()
		done := startCatalogLookup(ctx, c, "target")

		synctest.Wait()

		require.True(t, hasDeadline)
		require.Equal(t, constants.QueryTimeout.Default, deadline.Sub(started))
		time.Sleep(constants.QueryTimeout.Default)
		synctest.Wait()
		require.False(t, <-done)
		require.Equal(t, constants.QueryTimeout.Default, time.Since(started), "HTTP와 저장이 하나의 전체 예산을 사용해야 한다")
		require.NotNil(t, ownerErr)
		require.ErrorIs(t, ownerErr(), context.DeadlineExceeded)

		if phase == "persistence" {
			require.Equal(t, 1, storeCalls, "expired owner must not attempt remaining writes")
		} else {
			require.Zero(t, storeCalls)
		}

		_, found := c.cached("target")
		require.False(t, found)

		require.True(t, c.OpenChat(ctx, "target"))
		require.Equal(t, 2, listCalls)

		if phase == "persistence" {
			require.Equal(t, 3, storeCalls)
		} else {
			require.Equal(t, 2, storeCalls)
		}
	})
}

func catalogWaiterContexts(t *testing.T, name string) (context.Context, context.Context, context.CancelFunc) {
	t.Helper()

	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	if name == "initiator_deadline" {
		ctx, cancel = context.WithTimeout(t.Context(), time.Second)
	} else {
		ctx, cancel = context.WithCancel(t.Context())
	}

	if name == "follower" {
		return t.Context(), ctx, cancel
	}

	return ctx, t.Context(), cancel
}
