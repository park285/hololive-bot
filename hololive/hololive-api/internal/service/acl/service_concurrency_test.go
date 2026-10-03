package acl

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

// PG 대기 중에도 접근 판정은 확정된 상태를 유지하고 반대 mutation은 같은 순서로 커밋한다.
func TestConcurrentRoomMutationsPreserveCommittedACL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newFakeACLStore()

		store.settings[dbKeyEnabled] = testDBEnabledTrue
		store.settings[dbKeyMode] = string(ACLModeWhitelist)

		admin, bot := newFollowPair(t, store)
		entered, release := make(chan struct{}), make(chan struct{})
		unblock := sync.OnceFunc(func() { close(release) })
		t.Cleanup(unblock)

		store.createRoomHook = func(string, string) error {
			close(entered)
			<-release

			return nil
		}

		type outcome struct {
			changed bool
			err     error
		}

		addDone, removeDone := make(chan outcome, 1), make(chan outcome, 1)

		go func() { changed, err := admin.AddRoom(t.Context(), "3001"); addDone <- outcome{changed, err} }()

		<-entered
		require.False(t, admin.IsRoomAllowed("3001"), "미커밋 권한을 먼저 허용하면 안 된다")

		go func() { changed, err := admin.RemoveRoom(t.Context(), "3001"); removeDone <- outcome{changed, err} }()

		select {
		case got := <-removeDone:
			t.Fatalf("remove completed before pending add: %+v", got)
		default:
		}

		unblock()

		added, removed := <-addDone, <-removeDone
		require.NoError(t, added.err)
		require.True(t, added.changed)
		require.NoError(t, removed.err)
		require.True(t, removed.changed)
		assertFollowerMatchesStore(t, admin, store)
		assertFollowerMatchesStore(t, bot, store)
		require.False(t, bot.IsRoomAllowed("3001"))
	})
}

func TestFailedRoomDeletionKeepsCommittedACLVisible(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newFakeACLStore()

		store.settings[dbKeyEnabled] = testDBEnabledTrue
		store.settings[dbKeyMode] = string(ACLModeBlacklist)
		store.rooms[roomKey{roomID: "3001", listType: listTypeBlacklist}] = struct{}{}

		admin, bot := newFollowPair(t, store)
		entered, release := make(chan struct{}), make(chan struct{})
		unblock := sync.OnceFunc(func() { close(release) })
		t.Cleanup(unblock)

		store.deleteRoomHook = func(string, string) error {
			close(entered)
			<-release

			return errors.New("delete failed")
		}

		done := make(chan error, 1)

		go func() { _, err := admin.RemoveRoom(t.Context(), "3001"); done <- err }()

		<-entered
		require.False(t, admin.IsRoomAllowed("3001"), "DB 삭제가 실패할 수 있으므로 차단을 먼저 해제하면 안 된다")
		require.False(t, bot.IsRoomAllowed("3001"))
		unblock()
		require.Error(t, <-done)
		assertFollowerMatchesStore(t, admin, store)
		assertFollowerMatchesStore(t, bot, store)
	})
}

// 기존 경합이 남긴 메모리/PG 불일치에서도 명시적인 철회는 실제 저장된 권한을 제거한다.
func TestRemoveRoomRevokesPersistedRoomMissingFromMemory(t *testing.T) {
	pool := dbtest.NewPool(t)
	store := newPgxACLStore(pool)
	require.NoError(t, store.UpsertSetting(t.Context(), dbKeyEnabled, testDBEnabledTrue))
	require.NoError(t, store.UpsertSetting(t.Context(), dbKeyMode, string(ACLModeWhitelist)))
	require.NoError(t, store.CreateRoom(t.Context(), "3001", listTypeWhitelist))

	admin := &Service{
		store: store, logger: slog.New(slog.DiscardHandler), enabled: true,
		mode: ACLModeWhitelist, whitelistRooms: map[string]struct{}{}, blacklistRooms: map[string]struct{}{},
	}
	bot := &Service{
		store: store, logger: slog.New(slog.DiscardHandler), enabled: true,
		mode: ACLModeWhitelist, whitelistRooms: map[string]struct{}{}, blacklistRooms: map[string]struct{}{},
	}
	require.NoError(t, bot.Reload(t.Context()))
	bot.Follow(admin)

	removed, err := admin.RemoveRoom(t.Context(), "3001")
	require.NoError(t, err)
	require.True(t, removed)

	rooms, err := store.ListRooms(t.Context())
	require.NoError(t, err)
	require.Empty(t, rooms)
	require.False(t, bot.IsRoomAllowed("3001"))
}

type pausedCreateACLStore struct {
	aclStore

	entered chan struct{}
	release chan struct{}
}

func (s *pausedCreateACLStore) CreateRoom(ctx context.Context, roomID, listType string) error {
	close(s.entered)

	select {
	case <-s.release:
	case <-ctx.Done():
		return fmt.Errorf("wait for create release: %w", ctx.Err())
	}

	if err := s.aclStore.CreateRoom(ctx, roomID, listType); err != nil {
		return fmt.Errorf("create room through paused store: %w", err)
	}

	return nil
}

func TestConcurrentRoomMutationsConvergePostgresAndBot(t *testing.T) {
	pool := dbtest.NewPool(t)
	store := &pausedCreateACLStore{aclStore: newPgxACLStore(pool), entered: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(store.release) })
	t.Cleanup(release)
	require.NoError(t, store.UpsertSetting(t.Context(), dbKeyEnabled, testDBEnabledTrue))
	require.NoError(t, store.UpsertSetting(t.Context(), dbKeyMode, string(ACLModeWhitelist)))

	admin := &Service{
		store: store, logger: slog.New(slog.DiscardHandler), enabled: true,
		mode: ACLModeWhitelist, whitelistRooms: map[string]struct{}{}, blacklistRooms: map[string]struct{}{},
	}
	bot := &Service{
		store: store.aclStore, logger: slog.New(slog.DiscardHandler), enabled: true,
		mode: ACLModeWhitelist, whitelistRooms: map[string]struct{}{}, blacklistRooms: map[string]struct{}{},
	}
	bot.Follow(admin)

	type outcome struct {
		changed bool
		err     error
	}

	addDone, removeDone := make(chan outcome, 1), make(chan outcome, 1)

	go func() { changed, err := admin.AddRoom(t.Context(), "3001"); addDone <- outcome{changed, err} }()

	<-store.entered
	require.False(t, admin.IsRoomAllowed("3001"))

	removeStarted := make(chan struct{})

	go func() {
		close(removeStarted)

		changed, err := admin.RemoveRoom(t.Context(), "3001")
		removeDone <- outcome{changed, err}
	}()

	<-removeStarted
	release()

	added, removed := <-addDone, <-removeDone
	require.NoError(t, added.err)
	require.True(t, added.changed)
	require.NoError(t, removed.err)
	require.True(t, removed.changed)

	rooms, err := store.ListRooms(t.Context())
	require.NoError(t, err)
	require.Empty(t, rooms)
	require.False(t, admin.IsRoomAllowed("3001"))
	require.False(t, bot.IsRoomAllowed("3001"))
}
