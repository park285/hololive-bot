package acl

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// 관리 plane Service의 mutation이 성공하면, Follow로 묶인 봇 plane Service의 판정이 mutation 반환
// 시점에 이미 바뀌어 있어야 한다(비동기 Pub/Sub 전달을 기다리지 않는다).
func TestFollowAppliesSourceMutationsBeforeTheyReturn(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()

	store.settings[dbKeyEnabled] = testDBEnabledTrue
	store.settings[dbKeyMode] = string(ACLModeWhitelist)

	adminSide := newReloadTestService(store)
	botSide := newReloadTestService(store)
	botSide.Follow(adminSide)

	if added, err := adminSide.AddRoom(t.Context(), "3001"); err != nil || !added {
		t.Fatalf("AddRoom = (%v, %v), want added", added, err)
	}

	if !botSide.IsRoomAllowed("3001") {
		t.Fatal("whitelisted room must be allowed on the bot side right after AddRoom")
	}

	if removed, err := adminSide.RemoveRoom(t.Context(), "3001"); err != nil || !removed {
		t.Fatalf("RemoveRoom = (%v, %v), want removed", removed, err)
	}

	if botSide.IsRoomAllowed("3001") {
		t.Fatal("removed room must be denied on the bot side right after RemoveRoom")
	}

	if err := adminSide.SetMode(t.Context(), ACLModeBlacklist); err != nil {
		t.Fatalf("SetMode error: %v", err)
	}

	if !botSide.IsRoomAllowed("3001") {
		t.Fatal("empty blacklist must allow the room on the bot side right after SetMode")
	}

	if _, mode, _ := botSide.GetACLStatus(); mode != ACLModeBlacklist {
		t.Fatalf("bot-side mode = %s, want blacklist", mode)
	}

	if err := adminSide.SetEnabled(t.Context(), false); err != nil {
		t.Fatalf("SetEnabled error: %v", err)
	}

	if enabled, _, _ := botSide.GetACLStatus(); enabled {
		t.Fatal("bot side must observe ACL disabled right after SetEnabled")
	}
}

// PG 쓰기가 실패하면 오류를 돌려주고, 원본 메모리도 추종 복제본도 바뀌지 않는다.
// (SetEnabled/SetMode의 같은 계약은 TestACLService_Set*_DoesNotMutateMemoryOnDBFailure가 본다.)
func TestRoomMutationStoreFailureLeavesSourceAndFollowerUnchanged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		setup  func(*fakeACLStore)
		mutate func(context.Context, *Service) error
	}{
		{
			name: "add room",
			setup: func(store *fakeACLStore) {
				store.createRoomHook = func(string, string) error { return errors.New("pg down") }
			},
			mutate: func(ctx context.Context, s *Service) error {
				_, err := s.AddRoom(ctx, "3002")

				return err
			},
		},
		{
			name: "remove room",
			setup: func(store *fakeACLStore) {
				store.deleteRoomHook = func(string, string) error { return errors.New("pg down") }
			},
			mutate: func(ctx context.Context, s *Service) error {
				_, err := s.RemoveRoom(ctx, "3001")

				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := newFakeACLStore()

			store.settings[dbKeyEnabled] = testDBEnabledTrue
			store.settings[dbKeyMode] = string(ACLModeWhitelist)
			store.rooms[roomKey{roomID: "3001", listType: listTypeWhitelist}] = struct{}{}

			adminSide := newReloadTestService(store)
			botSide := newReloadTestService(store)

			for _, s := range []*Service{adminSide, botSide} {
				if err := s.Reload(t.Context()); err != nil {
					t.Fatalf("initial Reload error: %v", err)
				}
			}

			botSide.Follow(adminSide)
			tc.setup(store)

			if err := tc.mutate(t.Context(), adminSide); err == nil {
				t.Fatal("mutation error = nil, want store failure")
			}

			for side, s := range map[string]*Service{"admin": adminSide, "bot": botSide} {
				enabled, mode, rooms := s.GetACLStatus()
				if !enabled || mode != ACLModeWhitelist || len(rooms) != 1 || rooms[0] != "3001" {
					t.Fatalf("%s state = (%v, %s, %v), want unchanged (true, whitelist, [3001])", side, enabled, mode, rooms)
				}
			}
		})
	}
}

// fakeStoreStatus는 PG(fake store)에 커밋된 ACL 상태를 GetACLStatus와 같은 모양으로 돌려준다.
func fakeStoreStatus(t *testing.T, store *fakeACLStore) (enabled bool, mode ACLMode, rooms []string) {
	t.Helper()

	store.mu.Lock()
	defer store.mu.Unlock()

	enabled, err := parseACLEnabledStrict(store.settings[dbKeyEnabled])
	if err != nil {
		t.Fatalf("stored enabled: %v", err)
	}

	mode, err = parseACLModeStrict(store.settings[dbKeyMode])
	if err != nil {
		t.Fatalf("stored mode: %v", err)
	}

	rooms = []string{}

	for key := range store.rooms {
		if key.listType == string(mode) {
			rooms = append(rooms, key.roomID)
		}
	}

	slices.Sort(rooms)

	return enabled, mode, rooms
}

func assertFollowerMatchesStore(t *testing.T, follower *Service, store *fakeACLStore) {
	t.Helper()

	wantEnabled, wantMode, wantRooms := fakeStoreStatus(t, store)

	enabled, mode, rooms := follower.GetACLStatus()
	if enabled != wantEnabled || mode != wantMode || !slices.Equal(rooms, wantRooms) {
		t.Fatalf("follower = (%v, %s, %v), want PG state (%v, %s, %v)", enabled, mode, rooms, wantEnabled, wantMode, wantRooms)
	}
}

// newFollowPair는 store를 공유하는 관리/봇 plane Service를 초기 Reload한 뒤 봇 쪽이 관리 쪽을 Follow하게 묶는다.
func newFollowPair(t *testing.T, store *fakeACLStore) (adminSide, botSide *Service) {
	t.Helper()

	adminSide = newReloadTestService(store)
	botSide = newReloadTestService(store)

	for _, s := range []*Service{adminSide, botSide} {
		if err := s.Reload(t.Context()); err != nil {
			t.Fatalf("initial Reload error: %v", err)
		}
	}

	botSide.Follow(adminSide)

	return adminSide, botSide
}

// 겹친 두 mutation의 follower Reload가 서로 다른 시점의 스냅샷을 읽어도, 마지막에 적용되는 스냅샷은
// 두 커밋을 모두 반영해야 한다. A의 Reload가 설정을 읽은 뒤(B 커밋 전) 방 목록 읽기에서 멈춘 사이
// B가 커밋·통지하게 만들어, A의 오래된 스냅샷이 B의 최신 스냅샷을 덮어쓰는 순서를 강제한다.
func TestFollowOverlappingMutationsConvergeFollowerToPG(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()

	store.settings[dbKeyEnabled] = testDBEnabledTrue
	store.settings[dbKeyMode] = string(ACLModeWhitelist)

	adminSide, botSide := newFollowPair(t, store)

	var listCalls atomic.Int32

	aReading := make(chan struct{})
	releaseA := make(chan struct{})

	store.listRoomsHook = func() error {
		if listCalls.Add(1) == 1 {
			close(aReading)
			<-releaseA
		}

		return nil
	}

	ctx := t.Context()
	aDone := make(chan error, 1)
	bDone := make(chan error, 1)

	go func() { aDone <- adminSide.SetMode(ctx, ACLModeBlacklist) }()

	<-aReading // A의 Reload는 enabled=true(B 커밋 전 값)를 이미 읽었다.

	go func() { bDone <- adminSide.SetEnabled(ctx, false) }()

	// 직렬화가 없으면 B의 Reload가 여기서 끝나 최신 스냅샷을 먼저 적용한다. 직렬화되어 있으면 B는
	// A의 Reload가 끝날 때까지 기다리므로 유예 시간 뒤에 A를 풀어 준다.
	var bErr error

	bFinished := false

	select {
	case bErr = <-bDone:
		bFinished = true
	case <-time.After(200 * time.Millisecond):
	}

	close(releaseA)

	if err := <-aDone; err != nil {
		t.Fatalf("SetMode error: %v", err)
	}

	if !bFinished {
		bErr = <-bDone
	}

	if bErr != nil {
		t.Fatalf("SetEnabled error: %v", bErr)
	}

	assertFollowerMatchesStore(t, botSide, store)

	if enabled, mode, _ := botSide.GetACLStatus(); enabled || mode != ACLModeBlacklist {
		t.Fatalf("follower = (%v, %s), want both commits (false, blacklist)", enabled, mode)
	}
}

// reloadFailureHooks는 follower Reload(ListRooms)를 켜고 끌 수 있게 실패시키고, PG 쓰기 횟수를 센다.
type reloadFailureHooks struct {
	failReload atomic.Bool
	writes     atomic.Int32
}

func installReloadFailureHooks(store *fakeACLStore) *reloadFailureHooks {
	hooks := &reloadFailureHooks{}
	countWrite := func(string, string) error {
		hooks.writes.Add(1)

		return nil
	}

	store.listRoomsHook = func() error {
		if hooks.failReload.Load() {
			return errors.New("pg read failed")
		}

		return nil
	}
	store.upsertHook = countWrite
	store.createRoomHook = countWrite
	store.deleteRoomHook = countWrite

	return hooks
}

type followRetryCase struct {
	name   string
	mutate func(context.Context, *Service) (changed bool, err error)
	// reportsChange는 결과 bool이 의미 있는 mutation(AddRoom/RemoveRoom)인지다.
	reportsChange bool
}

// follower Reload가 실패하면 mutation은 PG 커밋 뒤에도 ErrACLPropagation을 돌려준다. 같은 요청을
// 다시 보내면 PG에 쓰지 않고 follower만 재동기화해, 기존 결과(no-op 성공/false)와 함께 수렴한다.
func TestFollowReloadFailureSurfacesAndSameRequestRetryConverges(t *testing.T) {
	t.Parallel()

	tests := []followRetryCase{
		{
			name: "set enabled",
			mutate: func(ctx context.Context, s *Service) (bool, error) {
				return true, s.SetEnabled(ctx, false)
			},
		},
		{
			name: "set mode",
			mutate: func(ctx context.Context, s *Service) (bool, error) {
				return true, s.SetMode(ctx, ACLModeBlacklist)
			},
		},
		{
			name: "add room",
			mutate: func(ctx context.Context, s *Service) (bool, error) {
				return s.AddRoom(ctx, "3002")
			},
			reportsChange: true,
		},
		{
			name: "remove room",
			mutate: func(ctx context.Context, s *Service) (bool, error) {
				return s.RemoveRoom(ctx, "3001")
			},
			reportsChange: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runFollowRetryCase(t, tc)
		})
	}
}

func runFollowRetryCase(t *testing.T, tc followRetryCase) {
	t.Helper()

	store := newFakeACLStore()

	store.settings[dbKeyEnabled] = testDBEnabledTrue
	store.settings[dbKeyMode] = string(ACLModeWhitelist)
	store.rooms[roomKey{roomID: "3001", listType: listTypeWhitelist}] = struct{}{}

	adminSide, botSide := newFollowPair(t, store)
	hooks := installReloadFailureHooks(store)
	beforeEnabled, beforeMode, beforeRooms := botSide.GetACLStatus()

	hooks.failReload.Store(true)

	changed, err := tc.mutate(t.Context(), adminSide)
	if !errors.Is(err, ErrACLPropagation) {
		t.Fatalf("first mutation error = %v, want ErrACLPropagation", err)
	}

	if tc.reportsChange && !changed {
		t.Fatal("first mutation must report the committed change alongside the propagation error")
	}

	if got := hooks.writes.Load(); got != 1 {
		t.Fatalf("PG writes after first mutation = %d, want 1 (committed)", got)
	}

	if enabled, mode, rooms := botSide.GetACLStatus(); enabled != beforeEnabled || mode != beforeMode || !slices.Equal(rooms, beforeRooms) {
		t.Fatalf("follower changed to (%v, %s, %v) despite failed reload", enabled, mode, rooms)
	}

	hooks.failReload.Store(false)

	changed, err = tc.mutate(t.Context(), adminSide)
	if err != nil {
		t.Fatalf("retry error = %v, want nil", err)
	}

	if tc.reportsChange && changed {
		t.Fatal("retry must keep the existing result (already exists / not found)")
	}

	if got := hooks.writes.Load(); got != 1 {
		t.Fatalf("PG writes after retry = %d, want 1 (retry must not write)", got)
	}

	assertFollowerMatchesStore(t, botSide, store)
}
