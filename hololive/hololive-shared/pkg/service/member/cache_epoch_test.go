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
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/internal/testredis"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedcache "github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/testutil"
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

func withTestEpochAuthority(c *Cache) *Cache {
	c.epoch = &fakeMemberEpochAuthority{epoch: 1}
	c.authorityEpoch.Store(1)
	c.authorityHealthy.Store(true)

	return c
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
		members, err := c.LoadAllMembers(t.Context())
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

	members, err := c.LoadAllMembers(t.Context())
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
	synctest.Test(t, func(t *testing.T) {
		authority := &fakeMemberEpochAuthority{epoch: 1}
		c := newEpochTestCache(authority)
		c.allMembersSnapshot.Store(newAllMembersState([]*domain.Member{{Name: testMemberNameOld}}, 0, time.Now()))

		ctx, cancel := context.WithCancel(t.Context())

		defer cancel()

		go c.runEpochReconcileWorker(ctx, nil)

		authority.setEpochTwo()
		synctest.Wait()
		synctest.Sleep(c.epochReconcileInterval)

		if c.authorityEpoch.Load() != 2 || c.allMembersSnapshot.Load() != nil {
			t.Fatal("periodic reconciliation did not invalidate the epoch-1 snapshot")
		}
	})
}

func TestCacheEpoch_ReconciliationOutlivesBootstrapContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		synctest.Wait()

		if got := c.authorityEpoch.Load(); got != 2 {
			t.Fatalf("authority epoch = %d, want 2", got)
		}
	})
}

func TestCacheEpoch_SubscriptionConfirmationReconcilesMissedEpoch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		synctest.Wait()

		if got := c.authorityEpoch.Load(); got != 2 {
			t.Fatalf("authority epoch = %d, want 2", got)
		}
	})
}

func TestCacheEpoch_ReconnectReconcilesBeforeRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		synctest.Wait()

		if got := c.authorityEpoch.Load(); got != 2 {
			t.Fatalf("authority epoch = %d, want 2", got)
		}
	})
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

	got, err := c.LoadAllMembers(t.Context())
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
		got, err := c.LoadAllMembers(t.Context())
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
	activity  chan struct{}
	database  *epochTestDatabase
	processes []*Cache
	publisher *Cache
}

func newEpochTestTopology(t *testing.T) *epochTestTopology {
	t.Helper()

	host, port, mini := testredis.StartMiniRedis(t)
	t.Cleanup(mini.Close)

	newService := func() *sharedcache.Service {
		service, err := sharedcache.NewCacheService(t.Context(), sharedcache.Config{
			Host:         host,
			Port:         port,
			DisableCache: true,
		}, slog.New(slog.DiscardHandler))
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

	topology := &epochTestTopology{mini: mini, database: &epochTestDatabase{}, activity: make(chan struct{}, 1)}
	mini.Server().SetPreHook(func(_ *server.Peer, _ string, _ ...string) bool {
		select {
		case topology.activity <- struct{}{}:
		default:
		}

		return false
	})
	topology.database.name.Store("Old")

	for range 2 {
		process, err := NewMemberCache(t.Context(), nil, newService(), slog.New(slog.DiscardHandler), CacheConfig{EpochReconcileInterval: 10 * time.Millisecond})
		if err != nil {
			t.Fatalf("NewMemberCache() error = %v", err)
		}

		t.Cleanup(process.Close)

		process.loadAllMembers = topology.database.load
		topology.processes = append(topology.processes, process)
	}

	topology.waitFor(t, func() bool { return mini.PubSubNumSub(memberEpochChannel)[memberEpochChannel] == 2 })

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

			members, err := process.LoadAllMembers(context.Background())
			if err != nil || len(members) != 1 || members[0].Name != name {
				return false
			}
		}

		return true
	}
}

func TestCacheEpoch_TwoProcessesConvergeAcrossValkey(t *testing.T) {
	topology := newEpochTestTopology(t)

	topology.waitFor(t, topology.converged(1, "Old"))

	topology.database.name.Store("New")

	if err := topology.publisher.InvalidateAll(t.Context()); err != nil {
		t.Fatalf("InvalidateAll() error = %v", err)
	}

	topology.waitFor(t, topology.converged(2, "New"))

	if keys := topology.mini.Keys(); len(keys) != 1 || keys[0] != memberEpochAuthorityKey {
		t.Fatalf("valkey keys = %v, want only %s", keys, memberEpochAuthorityKey)
	}
}

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

	byChannel, channelErr := c.FindMemberByChannelID(t.Context(), "UC-epoch")
	byName, nameErr := c.FindMemberByName(t.Context(), "Epoch")
	byAlias, aliasErr := c.FindMemberByAlias(t.Context(), "에포크")

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

func TestCacheEpoch_RecreatedEpochAfterValkeyRestartConverges(t *testing.T) {
	topology := newEpochTestTopology(t)

	for range 2 {
		if err := topology.publisher.InvalidateAll(t.Context()); err != nil {
			t.Fatalf("InvalidateAll() error = %v", err)
		}
	}

	topology.waitFor(t, topology.converged(3, "Old"))

	topology.database.name.Store("AfterRestart")
	topology.mini.FlushAll()

	topology.waitFor(t, topology.converged(1, "AfterRestart"))

	topology.database.name.Store("AfterMutation")

	if err := topology.publisher.InvalidateAll(t.Context()); err != nil {
		t.Fatalf("InvalidateAll() after restart error = %v", err)
	}

	topology.waitFor(t, topology.converged(2, "AfterMutation"))
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

	var worker sync.WaitGroup

	worker.Go(func() { c.runEpochReconciliation(ctx) })

	select {
	case <-authority.subscribed:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("subscriber did not start")
	}

	cancel()
	worker.Wait()

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

func (topology *epochTestTopology) waitFor(t *testing.T, condition func() bool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	for !condition() {
		select {
		case <-topology.activity:
		case <-ctx.Done():
			t.Fatal("cache processes did not converge before deadline")
		}
	}
}
