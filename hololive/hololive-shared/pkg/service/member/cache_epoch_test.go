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
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/internal/testredis"
	"github.com/kapu/hololive-shared/internal/testutil"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedcache "github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

type fakeMemberEpochAuthority struct {
	mu           sync.Mutex
	epoch        uint64
	currentErr   error
	advanceErr   error
	publishErr   error
	subscribed   chan struct{}
	messages     chan string
	disconnects  chan error
	publishCalls atomic.Int64
}

func (f *fakeMemberEpochAuthority) Current(context.Context) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.currentErr != nil {
		return 0, f.currentErr
	}

	return f.epoch, nil
}

func (f *fakeMemberEpochAuthority) Advance(context.Context) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.advanceErr != nil {
		return 0, f.advanceErr
	}

	f.epoch++

	return f.epoch, nil
}

func (f *fakeMemberEpochAuthority) Publish(context.Context, uint64) error {
	f.publishCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.publishErr
}

func (f *fakeMemberEpochAuthority) Subscribe(ctx context.Context, onSubscribed func(), onMessage func(string)) error {
	if onSubscribed != nil {
		onSubscribed()
	}

	if f.subscribed != nil {
		select {
		case f.subscribed <- struct{}{}:
		default:
		}
	}

	for {
		select {
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("subscribe to member epoch: %w", err)
			}

			return nil
		case message := <-f.messages:
			onMessage(message)
		case err := <-f.disconnects:
			return err
		}
	}
}

func (f *fakeMemberEpochAuthority) setEpochTwo() {
	f.mu.Lock()

	f.epoch = 2
	f.mu.Unlock()
}

func (f *fakeMemberEpochAuthority) setCurrentError(err error) {
	f.mu.Lock()

	f.currentErr = err
	f.mu.Unlock()
}

func newEpochTestCache(authority memberEpochAuthority) *Cache {
	c := &Cache{
		epoch:                  authority,
		epochReconcileInterval: 5 * time.Millisecond,
		logger:                 slog.New(slog.DiscardHandler),
		snapshotTTL:            time.Minute,
	}
	c.authorityEpoch.Store(1)
	c.authorityHealthy.Store(true)

	return c
}

func TestCacheEpoch_InFlightOldLoaderCannotPublishAfterRemoteBump(t *testing.T) {
	authority := &fakeMemberEpochAuthority{epoch: 1}
	c := newEpochTestCache(authority)
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})

	var calls atomic.Int64

	c.loadAllMembers = func(context.Context) ([]*domain.Member, error) {
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-releaseFirst

			return []*domain.Member{{Name: testMemberNameOld, ChannelID: "old"}}, nil
		}

		return []*domain.Member{{Name: testMemberNameNew, ChannelID: "new"}}, nil
	}

	type loadResult struct {
		members []*domain.Member
		err     error
	}

	done := make(chan loadResult, 1)

	go func() {
		members, err := c.AllMembers(t.Context())
		done <- loadResult{members: members, err: err}
	}()

	<-firstStarted
	authority.setEpochTwo()

	if err := c.reconcileEpoch(t.Context(), epochReconcileSubscription); err != nil {
		t.Fatalf("reconcileEpoch() error = %v", err)
	}

	close(releaseFirst)

	result := <-done
	if result.err != nil {
		t.Fatalf("AllMembers() error = %v", result.err)
	}

	got := result.members
	if len(got) != 1 || got[0].Name != testMemberNameNew {
		t.Fatalf("AllMembers() = %+v, want only New", got)
	}

	if _, ok := c.byName.Load(testMemberNameOld); ok {
		t.Fatal("old loader resurrected a prior-epoch name")
	}
}

func TestCacheEpoch_RemoteBumpRejectsStaleFallbackAfterReloadFailure(t *testing.T) {
	authority := &fakeMemberEpochAuthority{epoch: 2}
	c := newEpochTestCache(authority)
	c.authorityEpoch.Store(1)
	c.allMembersSnapshot.Store(&allMembersState{
		members:       []*domain.Member{{Name: testMemberNameOld, ChannelID: "old"}},
		loadedAt:      time.Now().Add(-2 * time.Minute),
		hasSuccessful: true,
	})

	wantErr := errors.New("database unavailable")

	c.loadAllMembers = func(context.Context) ([]*domain.Member, error) {
		return nil, wantErr
	}

	members, err := c.AllMembers(t.Context())
	if !errors.Is(err, wantErr) {
		t.Fatalf("AllMembers() error = %v, want %v", err, wantErr)
	}

	if members != nil {
		t.Fatalf("AllMembers() = %+v, want no stale fallback", members)
	}

	if c.allMembersSnapshot.Load() == nil || snapshotSuccessful(c.allMembersSnapshot.Load()) {
		t.Fatalf("snapshot = %+v, want cold failure state in epoch 2", c.allMembersSnapshot.Load())
	}
}

func TestCacheEpoch_PeriodicReconciliationRecoversMissedNotification(t *testing.T) {
	authority := &fakeMemberEpochAuthority{epoch: 1}
	c := newEpochTestCache(authority)
	c.allMembersSnapshot.Store(&allMembersState{members: []*domain.Member{{Name: testMemberNameOld}}, loadedAt: time.Now(), hasSuccessful: true})

	ctx, cancel := context.WithCancel(t.Context())

	defer cancel()

	go c.runEpochReconcileWorker(ctx, nil)

	authority.setEpochTwo()
	assertEventually(t, func() bool {
		return c.authorityEpoch.Load() == 2 && c.allMembersSnapshot.Load() == nil
	})
}

func TestCacheEpoch_ReconciliationOutlivesBootstrapContext(t *testing.T) {
	authority := &fakeMemberEpochAuthority{
		epoch:       1,
		subscribed:  make(chan struct{}, 1),
		messages:    make(chan string, 1),
		disconnects: make(chan error),
	}
	c := newEpochTestCache(authority)
	bootstrapCtx, cancelBootstrap := context.WithCancel(t.Context())
	runtimeCtx, stopRuntime := context.WithCancel(memberEpochRuntimeContext(bootstrapCtx))

	defer stopRuntime()

	go c.runEpochReconciliation(runtimeCtx)

	select {
	case <-authority.subscribed:
	case <-time.After(time.Second):
		t.Fatal("subscriber did not start")
	}

	cancelBootstrap()
	authority.setEpochTwo()

	authority.messages <- `{"version":2,"epoch":2}`

	assertEventually(t, func() bool { return c.authorityEpoch.Load() == 2 })
}

func TestCacheEpoch_SubscriptionConfirmationReconcilesMissedEpoch(t *testing.T) {
	authority := &fakeMemberEpochAuthority{
		epoch:       2,
		subscribed:  make(chan struct{}, 1),
		messages:    make(chan string),
		disconnects: make(chan error),
	}
	c := newEpochTestCache(authority)
	c.authorityEpoch.Store(1)

	ctx, cancel := context.WithCancel(t.Context())

	defer cancel()

	go c.runEpochReconciliation(ctx)

	select {
	case <-authority.subscribed:
	case <-time.After(time.Second):
		t.Fatal("subscriber did not start")
	}

	assertEventually(t, func() bool { return c.authorityEpoch.Load() == 2 })
}

func TestCacheEpoch_ReconnectReconcilesBeforeRetry(t *testing.T) {
	authority := &fakeMemberEpochAuthority{
		epoch:       1,
		subscribed:  make(chan struct{}, 2),
		messages:    make(chan string),
		disconnects: make(chan error, 1),
	}
	c := newEpochTestCache(authority)

	c.epochReconcileInterval = time.Hour

	ctx, cancel := context.WithCancel(t.Context())

	defer cancel()

	go c.runEpochReconciliation(ctx)

	select {
	case <-authority.subscribed:
	case <-time.After(time.Second):
		t.Fatal("subscriber did not start")
	}

	authority.setEpochTwo()

	authority.disconnects <- errors.New("connection lost")

	assertEventually(t, func() bool { return c.authorityEpoch.Load() == 2 })
}

func TestCacheEpoch_MutationSucceedsWhenNotificationFails(t *testing.T) {
	authority := &fakeMemberEpochAuthority{epoch: 4, publishErr: errors.New("pubsub unavailable")}
	c := newEpochTestCache(authority)

	if err := c.InvalidateAll(t.Context()); err != nil {
		t.Fatalf("InvalidateAll() error = %v", err)
	}

	if got := c.authorityEpoch.Load(); got != 5 {
		t.Fatalf("authority epoch = %d, want 5", got)
	}

	if got := authority.publishCalls.Load(); got != 1 {
		t.Fatalf("publish calls = %d, want 1", got)
	}
}

func TestCacheEpoch_UnavailableBypassesStaleSnapshot(t *testing.T) {
	authority := &fakeMemberEpochAuthority{epoch: 1}
	c := newEpochTestCache(authority)
	c.allMembersSnapshot.Store(&allMembersState{
		members:       []*domain.Member{{Name: "Stale"}},
		loadedAt:      time.Now(),
		hasSuccessful: true,
	})

	c.loadAllMembers = func(context.Context) ([]*domain.Member, error) {
		return []*domain.Member{{Name: "Database"}}, nil
	}

	authority.setCurrentError(errors.New("valkey unavailable"))

	if err := c.reconcileEpoch(t.Context(), epochReconcilePeriodic); err == nil {
		t.Fatal("reconcileEpoch() error = nil, want failure")
	}

	got, err := c.AllMembers(t.Context())
	if err != nil {
		t.Fatalf("AllMembers() error = %v", err)
	}

	if len(got) != 1 || got[0].Name != "Database" {
		t.Fatalf("AllMembers() = %+v, want direct database result", got)
	}

	if c.allMembersSnapshot.Load() != nil {
		t.Fatal("stale snapshot remained available while epoch was uncertain")
	}
}

func TestCacheEpoch_RegressionFailsClosed(t *testing.T) {
	authority := &fakeMemberEpochAuthority{epoch: 4}
	c := newEpochTestCache(authority)
	c.authorityEpoch.Store(5)
	c.allMembersSnapshot.Store(&allMembersState{members: []*domain.Member{{Name: "Stale"}}, loadedAt: time.Now(), hasSuccessful: true})

	if err := c.reconcileEpoch(t.Context(), epochReconcilePeriodic); err == nil {
		t.Fatal("reconcileEpoch() accepted a regressed authority")
	}

	if c.authorityHealthy.Load() {
		t.Fatal("regressed authority left cache enabled")
	}

	if c.allMembersSnapshot.Load() != nil {
		t.Fatal("regressed authority retained stale snapshot")
	}
}

func TestCacheEpoch_CorruptNotificationUsesAuthority(t *testing.T) {
	authority := &fakeMemberEpochAuthority{epoch: 3}
	c := newEpochTestCache(authority)
	c.handleEpochNotification(`{"version":2,"epoch":"broken"}`)

	if err := c.reconcileEpoch(t.Context(), epochReconcileSubscription); err != nil {
		t.Fatalf("reconcileEpoch() error = %v", err)
	}

	if got := c.authorityEpoch.Load(); got != 3 {
		t.Fatalf("authority epoch = %d, want reconciled 3", got)
	}
}

func TestCacheEpoch_PointLookupsNeverReadPriorEpochKeys(t *testing.T) {
	cacheClient := cachemocks.NewLenientClient()

	var keys []string

	cacheClient.GetFunc = func(_ context.Context, key string, _ any) error {
		keys = append(keys, key)
		return errors.New("miss")
	}

	c := newEpochTestCache(&fakeMemberEpochAuthority{epoch: 2})

	c.cache = cacheClient
	c.authorityEpoch.Store(2)

	if got := c.loadChannelFromDistributedCache(t.Context(), "renamed-channel", 0); got != nil {
		t.Fatalf("channel lookup = %+v, want miss", got)
	}

	if got := c.loadNameFromDistributedCache(t.Context(), "DeletedName", 0); got != nil {
		t.Fatalf("name lookup = %+v, want miss", got)
	}

	if got := c.getAliasFromCache(t.Context(), "RemovedAlias", 0); got != nil {
		t.Fatalf("alias lookup = %+v, want miss", got)
	}

	want := []string{
		memberEpochDataPrefix + "2:" + memberChannelKeyPrefix + "renamed-channel",
		memberEpochDataPrefix + "2:" + memberNameKeyPrefix + "DeletedName",
		memberEpochDataPrefix + "2:" + memberAliasKeyPrefix + "RemovedAlias",
	}
	if len(keys) != len(want) {
		t.Fatalf("lookup keys = %v, want %v", keys, want)
	}

	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("lookup key[%d] = %q, want %q", i, keys[i], want[i])
		}
	}
}

func TestCacheEpoch_TwoProcessesConvergeAcrossValkey(t *testing.T) {
	host, port, mini := testredis.StartMiniRedis(t)
	t.Cleanup(mini.Close)

	newService := func() *sharedcache.Service {
		service, err := sharedcache.NewCacheService(t.Context(), sharedcache.Config{
			Host:         host,
			Port:         port,
			DisableCache: true,
		}, sharedlogging.NewTestLogger())
		if err != nil {
			t.Fatalf("NewCacheService() error = %v", err)
		}

		t.Cleanup(func() {
			if err := service.Close(); err != nil {
				t.Errorf("Close() error = %v", err)
			}
		})

		return service
	}

	config := CacheConfig{EpochReconcileInterval: 10 * time.Millisecond}

	first, err := NewMemberCache(t.Context(), nil, newService(), slog.New(slog.DiscardHandler), config)
	if err != nil {
		t.Fatalf("first NewMemberCache() error = %v", err)
	}

	second, err := NewMemberCache(t.Context(), nil, newService(), slog.New(slog.DiscardHandler), config)
	if err != nil {
		t.Fatalf("second NewMemberCache() error = %v", err)
	}

	assertEventually(t, func() bool { return mini.PubSubNumSub(memberEpochChannel)[memberEpochChannel] == 2 })

	// miniredis의 RESP2 Pub/Sub 연결 제약이 subscriber 자신의 mutation 명령을 간헐적으로 오염시키므로
	// 실제 multi-process topology처럼 별도 command client에서 mutation을 발행한다.
	publisherService := newService()
	publisher := newEpochTestCache(newValkeyMemberEpochAuthority(publisherService.GetClient()))

	publisher.cache = publisherService

	if err := publisher.InvalidateAll(t.Context()); err != nil {
		t.Fatalf("InvalidateAll() error = %v", err)
	}

	assertEventually(t, func() bool {
		return publisher.authorityEpoch.Load() == 2 && first.authorityEpoch.Load() == 2 && second.authorityEpoch.Load() == 2
	})
}

func TestCacheEpoch_InvalidationLeavesUnprefixedKeyspaceUntouched(t *testing.T) {
	service, mini := testutil.NewTestCacheServiceWithMini(t.Context(), t)
	c := &Cache{
		cache:  service,
		epoch:  newValkeyMemberEpochAuthority(service.GetClient()),
		logger: slog.New(slog.DiscardHandler),
	}
	c.authorityEpoch.Store(1)
	c.authorityHealthy.Store(true)

	if err := mini.Set(memberNameKeyPrefix+"unprefixed", "stale"); err != nil {
		t.Fatalf("seed unprefixed key: %v", err)
	}

	if err := c.InvalidateAll(t.Context()); err != nil {
		t.Fatalf("InvalidateAll() error = %v", err)
	}

	if !mini.Exists(memberEpochAuthorityKey) {
		t.Fatal("invalidation deleted the V2 authority key")
	}

	if !mini.Exists(memberNameKeyPrefix + "unprefixed") {
		t.Fatal("invalidation scanned/deleted outside the epoch-scoped namespace; contraction removed the legacy member:* sweep")
	}
}

func TestValkeyMemberEpochAuthorityRejectsOverflowAndCorruption(t *testing.T) {
	service, mini := testutil.NewTestCacheServiceWithMini(t.Context(), t)
	authority := newValkeyMemberEpochAuthority(service.GetClient())

	for _, value := range []string{"0", "-1", "1 ", "invalid"} {
		if err := mini.Set(memberEpochAuthorityKey, value); err != nil {
			t.Fatalf("seed corrupt epoch %q: %v", value, err)
		}

		if _, err := authority.Current(t.Context()); err == nil {
			t.Fatalf("Current() accepted corrupt value %q", value)
		}
	}

	if err := mini.Set(memberEpochAuthorityKey, strconv.FormatInt(math.MaxInt64, 10)); err != nil {
		t.Fatalf("seed max epoch: %v", err)
	}

	if _, err := authority.Current(t.Context()); err == nil {
		t.Fatal("Current() accepted an exhausted epoch")
	}

	if err := mini.Set(memberEpochAuthorityKey, strconv.FormatUint(maxMemberEpoch, 10)); err != nil {
		t.Fatalf("seed last usable epoch: %v", err)
	}

	if _, err := authority.Advance(t.Context()); err == nil {
		t.Fatal("Advance() accepted epoch overflow")
	}

	if _, err := authority.Current(t.Context()); err == nil {
		t.Fatal("Current() accepted the saturated epoch left by overflow")
	}
}

func TestCacheEpoch_ClientClosingStopsWithoutWarn(t *testing.T) {
	authority := &fakeMemberEpochAuthority{
		epoch:       1,
		subscribed:  make(chan struct{}, 2),
		messages:    make(chan string),
		disconnects: make(chan error, 1),
	}

	var logs bytes.Buffer

	c := newEpochTestCache(authority)

	c.logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	c.epochReconcileInterval = time.Hour

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan struct{})

	go func() {
		c.runEpochReconciliation(ctx)
		close(done)
	}()

	select {
	case <-authority.subscribed:
	case <-time.After(time.Second):
		t.Fatal("subscriber did not start")
	}

	authority.disconnects <- valkey.ErrClosing

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("epoch subscription did not stop after client close")
	}

	select {
	case <-authority.subscribed:
		t.Fatal("epoch subscription resubscribed after client close")
	default:
	}

	if strings.Contains(logs.String(), `"level":"WARN"`) {
		t.Fatalf("client close logged a warning: %s", logs.String())
	}
}

func TestCacheEpoch_CanceledSubscribeDoesNotWarn(t *testing.T) {
	authority := &fakeMemberEpochAuthority{
		epoch:       1,
		subscribed:  make(chan struct{}, 1),
		messages:    make(chan string),
		disconnects: make(chan error),
	}

	var logs bytes.Buffer

	c := newEpochTestCache(authority)

	c.logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

	ctx, cancel := context.WithCancel(t.Context())

	go c.runEpochReconciliation(ctx)

	select {
	case <-authority.subscribed:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("subscriber did not start")
	}

	cancel()
	time.Sleep(20 * time.Millisecond)

	if strings.Contains(logs.String(), `"level":"WARN"`) {
		t.Fatalf("canceled subscribe logged a warning: %s", logs.String())
	}
}

func TestCacheEpoch_UnexpectedDisconnectStillWarns(t *testing.T) {
	authority := &fakeMemberEpochAuthority{
		epoch:       1,
		subscribed:  make(chan struct{}, 2),
		messages:    make(chan string),
		disconnects: make(chan error, 1),
	}

	var logs bytes.Buffer

	c := newEpochTestCache(authority)

	c.logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	c.epochReconcileInterval = time.Hour

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go c.runEpochReconciliation(ctx)

	select {
	case <-authority.subscribed:
	case <-time.After(time.Second):
		t.Fatal("subscriber did not start")
	}

	authority.disconnects <- errors.New("connection lost")

	select {
	case <-authority.subscribed:
	case <-time.After(3 * time.Second):
		t.Fatal("subscriber did not reconnect after unexpected disconnect")
	}

	if !strings.Contains(logs.String(), `"level":"WARN"`) {
		t.Fatalf("unexpected disconnect did not log a warning: %s", logs.String())
	}
}

func TestParseMemberEpoch(t *testing.T) {
	if got, err := parseMemberEpoch("42"); err != nil || got != 42 {
		t.Fatalf("parseMemberEpoch(42) = %d, %v", got, err)
	}
}

func assertEventually(t *testing.T, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatal("condition was not satisfied before deadline")
}
