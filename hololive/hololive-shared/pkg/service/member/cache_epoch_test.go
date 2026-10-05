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

	"github.com/alicebob/miniredis/v2"
	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/internal/testredis"
	"github.com/kapu/hololive-shared/internal/testutil"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedcache "github.com/kapu/hololive-shared/pkg/service/cache"
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

// withTestEpochAuthority는 분산 캐시를 쓰는 테스트 Cache에 건강한 fake epoch authority를 주입한다. 분산 캐시가 있으면
// epoch authority가 필수라는 불변식(configureEpoch)을 직접 구성한 테스트 Cache에서도 지킨다.
func withTestEpochAuthority(c *Cache) *Cache {
	c.epoch = &fakeMemberEpochAuthority{epoch: 1}
	c.authorityEpoch.Store(1)
	c.authorityHealthy.Store(true)

	return c
}

// newEpochTestCache는 epoch 조정 경로를 검증한다.
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

	if got, _ := c.lookupPointInMemory(pointLookupName, testMemberNameOld); got != nil {
		t.Fatal("old loader resurrected a prior-epoch name")
	}
}

func TestCacheEpoch_RemoteBumpRejectsStaleFallbackAfterReloadFailure(t *testing.T) {
	authority := &fakeMemberEpochAuthority{epoch: 2}
	c := newEpochTestCache(authority)
	c.authorityEpoch.Store(1)
	c.allMembersSnapshot.Store(newAllMembersState([]*domain.Member{{Name: testMemberNameOld, ChannelID: "old"}}, 0, time.Now().Add(-2*time.Minute)))

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
	c.allMembersSnapshot.Store(newAllMembersState([]*domain.Member{{Name: testMemberNameOld}}, 0, time.Now()))

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
	c.allMembersSnapshot.Store(newAllMembersState([]*domain.Member{{Name: "Stale"}}, 0, time.Now()))

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

// Valkey 재시작으로 epoch key가 1로 다시 만들어지면 값이 작아져도 변경으로 받아들인다. Snapshot은 버리지만 cache는
// 계속 켜져 있어 다음 조회가 새 snapshot을 적재한다(PostgreSQL 영구 우회 없음).
func TestCacheEpoch_RegressionInvalidatesAndKeepsCacheEnabled(t *testing.T) {
	authority := &fakeMemberEpochAuthority{epoch: 1}
	c := newEpochTestCache(authority)
	c.authorityEpoch.Store(5)
	c.allMembersSnapshot.Store(newAllMembersState([]*domain.Member{{Name: "Stale"}}, 0, time.Now()))

	var loads atomic.Int64

	c.loadAllMembers = func(context.Context) ([]*domain.Member, error) {
		loads.Add(1)

		return []*domain.Member{{Name: "Database"}}, nil
	}

	if err := c.reconcileEpoch(t.Context(), epochReconcilePeriodic); err != nil {
		t.Fatalf("reconcileEpoch() error = %v", err)
	}

	if !c.authorityHealthy.Load() || c.authorityEpoch.Load() != 1 {
		t.Fatalf("authority healthy=%v epoch=%d, want healthy epoch 1", c.authorityHealthy.Load(), c.authorityEpoch.Load())
	}

	for range 2 {
		got, err := c.AllMembers(t.Context())
		if err != nil || len(got) != 1 || got[0].Name != "Database" {
			t.Fatalf("AllMembers() = %+v, %v; want reloaded Database snapshot", got, err)
		}
	}

	if loads.Load() != 1 {
		t.Fatalf("loader calls = %d, want one snapshot load then cache hits", loads.Load())
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

// epochTestDatabase는 여러 테스트 프로세스가 공유하는 PostgreSQL members 역할을 한다.
type epochTestDatabase struct {
	name atomic.Value
}

func (d *epochTestDatabase) load(context.Context) ([]*domain.Member, error) {
	name, ok := d.name.Load().(string)
	if !ok {
		return nil, errors.New("epoch test database name is not set")
	}

	return []*domain.Member{{ID: 1, ChannelID: "UC-epoch", Name: name}}, nil
}

type epochTestTopology struct {
	mini      *miniredis.Miniredis
	database  *epochTestDatabase
	processes []*Cache
	publisher *Cache
}

// newEpochTestTopology는 같은 Valkey를 보는 두 member cache 프로세스와, mutation만 발행하는 별도 command client를 만든다.
// Miniredis의 RESP2 Pub/Sub 연결 제약이 subscriber 자신의 mutation 명령을 간헐적으로 오염시키므로 실제 multi-process
// topology처럼 별도 client에서 mutation을 발행한다.
func newEpochTestTopology(t *testing.T) *epochTestTopology {
	t.Helper()

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

	topology := &epochTestTopology{mini: mini, database: &epochTestDatabase{}}
	topology.database.name.Store("Old")

	for range 2 {
		process, err := NewMemberCache(t.Context(), nil, newService(), slog.New(slog.DiscardHandler), CacheConfig{EpochReconcileInterval: 10 * time.Millisecond})
		if err != nil {
			t.Fatalf("NewMemberCache() error = %v", err)
		}

		process.loadAllMembers = topology.database.load
		topology.processes = append(topology.processes, process)
	}

	assertEventually(t, func() bool { return mini.PubSubNumSub(memberEpochChannel)[memberEpochChannel] == 2 })

	publisherService := newService()

	topology.publisher = newEpochTestCache(newValkeyMemberEpochAuthority(publisherService.GetClient()))

	return topology
}

func (topology *epochTestTopology) converged(epoch uint64, name string) func() bool {
	return func() bool {
		for _, process := range topology.processes {
			if process.authorityEpoch.Load() != epoch || !process.authorityHealthy.Load() {
				return false
			}

			members, err := process.AllMembers(context.Background())
			if err != nil || len(members) != 1 || members[0].Name != name {
				return false
			}
		}

		return true
	}
}

func TestCacheEpoch_TwoProcessesConvergeAcrossValkey(t *testing.T) {
	topology := newEpochTestTopology(t)

	assertEventually(t, topology.converged(1, "Old"))

	topology.database.name.Store("New")

	if err := topology.publisher.InvalidateAll(t.Context()); err != nil {
		t.Fatalf("InvalidateAll() error = %v", err)
	}

	assertEventually(t, topology.converged(2, "New"))

	// Valkey에는 epoch authority만 남고 멤버 데이터 key는 쓰이지 않는다.
	if keys := topology.mini.Keys(); len(keys) != 1 || keys[0] != memberEpochAuthorityKey {
		t.Fatalf("valkey keys = %v, want only %s", keys, memberEpochAuthorityKey)
	}
}

// warmup, 채널·이름·별칭 조회, 무효화 전체에서 Valkey에 쓰이는 것은 epoch authority key 하나뿐이다.
func TestCacheEpoch_MemberDataNeverWrittenToValkey(t *testing.T) {
	service, mini := testutil.NewTestCacheServiceWithMini(t.Context(), t)
	c := newEpochTestCache(newValkeyMemberEpochAuthority(service.GetClient()))

	c.loadAllMembers = func(context.Context) ([]*domain.Member, error) {
		return []*domain.Member{{ID: 1, ChannelID: "UC-epoch", Name: "Epoch", Aliases: &domain.Aliases{Ko: []string{"에포크"}}}}, nil
	}

	if err := c.reconcileEpoch(t.Context(), epochReconcileStartup); err != nil {
		t.Fatalf("reconcileEpoch() error = %v", err)
	}

	if err := c.WarmUpCache(t.Context()); err != nil {
		t.Fatalf("WarmUpCache() error = %v", err)
	}

	byChannel, channelErr := c.GetByChannelID(t.Context(), "UC-epoch")
	byName, nameErr := c.GetByName(t.Context(), "Epoch")
	byAlias, aliasErr := c.FindByAlias(t.Context(), "에포크")

	if channelErr != nil || nameErr != nil || aliasErr != nil || byChannel.ID != 1 || byName.ID != 1 || byAlias.ID != 1 {
		t.Fatalf("snapshot lookups = %+v/%v, %+v/%v, %+v/%v", byChannel, channelErr, byName, nameErr, byAlias, aliasErr)
	}

	if err := c.InvalidateAll(t.Context()); err != nil {
		t.Fatalf("InvalidateAll() error = %v", err)
	}

	if keys := mini.Keys(); len(keys) != 1 || keys[0] != memberEpochAuthorityKey {
		t.Fatalf("valkey keys = %v, want only %s", keys, memberEpochAuthorityKey)
	}
}

// Valkey 재시작으로 epoch key가 사라져 1로 다시 만들어져도(값 회귀) 장수 프로세스는 epoch 1을 건강한 authority로
// 받아들이고 새 snapshot으로 수렴한다. 예전 크기 비교는 여기서 authority를 영구 불확실로 두어 재시작 전까지 PostgreSQL을
// 직접 읽게 했다.
func TestCacheEpoch_RecreatedEpochAfterValkeyRestartConverges(t *testing.T) {
	topology := newEpochTestTopology(t)

	for range 2 {
		if err := topology.publisher.InvalidateAll(t.Context()); err != nil {
			t.Fatalf("InvalidateAll() error = %v", err)
		}
	}

	assertEventually(t, topology.converged(3, "Old"))

	topology.database.name.Store("AfterRestart")
	topology.mini.FlushAll()

	assertEventually(t, topology.converged(1, "AfterRestart"))

	topology.database.name.Store("AfterMutation")

	if err := topology.publisher.InvalidateAll(t.Context()); err != nil {
		t.Fatalf("InvalidateAll() after restart error = %v", err)
	}

	assertEventually(t, topology.converged(2, "AfterMutation"))
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
