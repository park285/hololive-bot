package holodexprovider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kapu/hololive-shared/internal/service/holodex/provider/streammapping"
	"github.com/kapu/hololive-shared/internal/testredis"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// owner가 원천에서 대기할 때 follower의 첫 캐시 읽기 완료를 관측한다.
type streamFillObservationKey struct{}

type streamFillReadObserver struct {
	waiting chan struct{}
	once    sync.Once
}

type streamFillTestCache struct {
	cache.KeyValueCache
}

func (c streamFillTestCache) Get(ctx context.Context, key string, dest any) error {
	err := c.KeyValueCache.Get(ctx, key, dest)
	if observed, ok := ctx.Value(streamFillObservationKey{}).(*streamFillReadObserver); ok {
		observed.once.Do(func() { close(observed.waiting) })
	}

	if err != nil {
		return fmt.Errorf("read serialized test cache: %w", err)
	}

	return nil
}

func newStreamFillTestService(t *testing.T, requester *MockRequester) *Service {
	t.Helper()

	host, port, _ := testredis.StartMiniRedis(t)
	logger := slog.New(slog.DiscardHandler)

	store, err := cache.NewCacheService(t.Context(), cache.Config{
		Host:              host,
		Port:              port,
		DisableCache:      true,
		ForceSingleClient: true,
	}, logger)
	if err != nil {
		t.Fatalf("create serialized cache: %v", err)
	}

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close serialized cache: %v", err)
		}
	})

	return &Service{
		requester:    requester,
		logger:       logger,
		cacheManager: NewCacheManager(streamFillTestCache{KeyValueCache: store}, logger),
		mapper:       streammapping.NewStreamMapper(logger),
		filter:       streammapping.NewStreamFilter(logger),
	}
}

type streamFillTestResult struct {
	streams []*domain.Stream
	err     error
}

func startStreamFillTestCall(ctx context.Context, call func(context.Context) ([]*domain.Stream, error)) <-chan streamFillTestResult {
	result := make(chan streamFillTestResult, 1)

	go func() {
		streams, err := call(ctx)
		result <- streamFillTestResult{streams: streams, err: err}
	}()

	return result
}

func streamFillTestPayload(t *testing.T, status domain.StreamStatus, org string) []byte {
	t.Helper()

	return mustMarshalStreamRawList(t, []streammapping.StreamRaw{{
		ID:          "cache-fill-stream",
		Title:       "Original title",
		ChannelID:   new("cache-fill-channel"),
		Status:      status,
		LiveViewers: new(42),
		Channel: &streammapping.ChannelRaw{
			ID:   "cache-fill-channel",
			Name: "Original channel",
			Org:  &org,
		},
	}})
}

func assertStreamFillResult(t *testing.T, result streamFillTestResult, status domain.StreamStatus, empty bool) {
	t.Helper()

	if result.err != nil {
		t.Fatalf("stream query failed: %v", result.err)
	}

	if empty {
		if len(result.streams) != 0 {
			t.Fatalf("empty origin returned streams: %+v", result.streams)
		}

		return
	}

	if len(result.streams) != 1 {
		t.Fatalf("streams = %+v, want one mapped stream", result.streams)
	}

	stream := result.streams[0]
	if stream.ID != "cache-fill-stream" || stream.Title != "Original title" || stream.Status != status ||
		stream.ViewerCount == nil || *stream.ViewerCount != 42 ||
		stream.Channel == nil || stream.Channel.Name != "Original channel" ||
		stream.Channel.Org == nil || *stream.Channel.Org != constants.HolodexAPIParams.OrgHololive {
		t.Fatalf("mapped stream changed: %+v", stream)
	}
}

func assertStreamFillIdle(t *testing.T, service *Service) {
	t.Helper()

	service.streamCacheFills.mu.Lock()
	defer service.streamCacheFills.mu.Unlock()

	if count := len(service.streamCacheFills.owners); count != 0 {
		t.Errorf("finished cache fills retained %d active keys", count)
	}
}

func TestStreamCacheFill_ConcurrentMissesUseSerializedCache(t *testing.T) {
	for _, status := range []domain.StreamStatus{domain.StreamStatusLive, domain.StreamStatusUpcoming} {
		for _, empty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/empty=%t", status, empty), func(t *testing.T) {
				testConcurrentStreamFill(t, status, empty)
			})
		}
	}
}

func testConcurrentStreamFill(t *testing.T, status domain.StreamStatus, empty bool) {
	t.Helper()

	payload := streamFillTestPayload(t, status, constants.HolodexAPIParams.OrgHololive)

	if empty {
		payload = []byte("[]")
	}

	entered := make(chan struct{})
	release := make(chan struct{})

	var calls atomic.Int32

	requester := &MockRequester{DoRequestFunc: func(ctx context.Context, _, _ string, _ url.Values) ([]byte, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}

		select {
		case <-release:
			return payload, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	service := newStreamFillTestService(t, requester)
	query := func(ctx context.Context, org string) ([]*domain.Stream, error) {
		if status == domain.StreamStatusUpcoming {
			return service.GetUpcomingStreamsByOrg(ctx, 24, org)
		}

		return service.GetLiveStreamsByOrg(ctx, org)
	}
	owner := startStreamFillTestCall(t.Context(), func(ctx context.Context) ([]*domain.Stream, error) {
		return query(ctx, "Hololive")
	})

	<-entered

	results := startStreamFillFollowers(t, query)

	if got := calls.Load(); got != 1 {
		t.Errorf("origin calls while owner blocked = %d, want 1", got)
	}

	close(release)

	ownerResult := <-owner
	assertStreamFillResult(t, ownerResult, status, empty)

	followerResults := make([]streamFillTestResult, len(results))

	for i, result := range results {
		followerResults[i] = <-result
		assertStreamFillResult(t, followerResults[i], status, empty)
	}

	if !empty {
		assertFillCallerOwnership(t, ownerResult, followerResults, status)
	}

	streams, err := query(t.Context(), "holo")
	assertStreamFillResult(t, streamFillTestResult{streams: streams, err: err}, status, empty)

	if got := calls.Load(); got != 1 {
		t.Errorf("cold concurrent and warm origin calls = %d, want 1", got)
	}

	assertStreamFillIdle(t, service)
}

func startStreamFillFollowers(t *testing.T, query func(context.Context, string) ([]*domain.Stream, error)) []<-chan streamFillTestResult {
	t.Helper()

	const followers = 11

	results := make([]<-chan streamFillTestResult, followers)
	aliases := []string{"", "holo", " Hololive! "}

	for i := range followers {
		observer := &streamFillReadObserver{waiting: make(chan struct{})}

		results[i] = startStreamFillTestCall(context.WithValue(t.Context(), streamFillObservationKey{}, observer), func(ctx context.Context) ([]*domain.Stream, error) { return query(ctx, aliases[i%len(aliases)]) })
		<-observer.waiting
	}

	return results
}

func assertFillCallerOwnership(t *testing.T, ownerResult streamFillTestResult, followerResults []streamFillTestResult, status domain.StreamStatus) {
	t.Helper()

	ownerResult.streams[0].Title = "Owner mutation"
	*ownerResult.streams[0].ViewerCount = 0
	ownerResult.streams[0].Channel.Name = "Owner channel mutation"
	*ownerResult.streams[0].Channel.Org = "Owner org mutation"
	ownerResult.streams = append(ownerResult.streams, &domain.Stream{ID: "owner-only"})

	assertStreamFillResult(t, followerResults[0], status, false)

	followerResults[0].streams[0].Title = "Follower mutation"
	*followerResults[0].streams[0].ViewerCount = 1
	followerResults[0].streams[0].Channel.Name = "Follower channel mutation"
	*followerResults[0].streams[0].Channel.Org = "Follower org mutation"
	followerResults[0].streams = append(followerResults[0].streams, &domain.Stream{ID: "follower-only"})
	assertStreamFillResult(t, followerResults[1], status, false)
}

func TestStreamCacheFill_OwnerFailureDoesNotPoisonFollower(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("canceled=%t", canceled), func(t *testing.T) {
			payload := streamFillTestPayload(t, domain.StreamStatusLive, constants.HolodexAPIParams.OrgHololive)
			entered := make(chan struct{})
			release := make(chan struct{})
			originErr := errors.New("origin unavailable")

			var calls atomic.Int32

			requester := &MockRequester{DoRequestFunc: func(ctx context.Context, _, _ string, _ url.Values) ([]byte, error) {
				if calls.Add(1) != 1 {
					return payload, nil
				}

				close(entered)

				select {
				case <-release:
					return nil, originErr
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}}
			service := newStreamFillTestService(t, requester)
			ownerCtx, cancelOwner := context.WithCancel(t.Context())

			defer cancelOwner()

			owner := startStreamFillTestCall(ownerCtx, service.GetLiveStreams)

			<-entered

			followerCtx := &streamFillReadObserver{waiting: make(chan struct{})}
			follower := startStreamFillTestCall(context.WithValue(t.Context(), streamFillObservationKey{}, followerCtx), service.GetLiveStreams)
			<-followerCtx.waiting

			wantErr := originErr

			if canceled {
				cancelOwner()

				wantErr = context.Canceled
			} else {
				close(release)
			}

			if result := <-owner; !errors.Is(result.err, wantErr) {
				t.Errorf("owner error = %v, want %v", result.err, wantErr)
			}

			assertStreamFillResult(t, <-follower, domain.StreamStatusLive, false)

			streams, err := service.GetLiveStreams(t.Context())
			assertStreamFillResult(t, streamFillTestResult{streams: streams, err: err}, domain.StreamStatusLive, false)

			if got := calls.Load(); got != 2 {
				t.Errorf("origin calls = %d, want failed owner plus successful follower", got)
			}

			assertStreamFillIdle(t, service)
		})
	}
}

func TestStreamCacheFill_DifferentKeysProgressIndependently(t *testing.T) {
	entered := make(chan url.Values, 8)
	release := make(chan struct{})

	var calls atomic.Int32

	requester := &MockRequester{DoRequestFunc: func(ctx context.Context, _, _ string, params url.Values) ([]byte, error) {
		calls.Add(1)

		entered <- params

		select {
		case <-release:
			return []byte("[]"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	service := newStreamFillTestService(t, requester)
	queries := []func(context.Context) ([]*domain.Stream, error){
		service.GetLiveStreams,
		func(ctx context.Context) ([]*domain.Stream, error) {
			return service.GetLiveStreamsByOrg(ctx, constants.HolodexAPIParams.OrgVSpo)
		},
		func(ctx context.Context) ([]*domain.Stream, error) {
			return service.GetUpcomingStreams(ctx, 24)
		},
		func(ctx context.Context) ([]*domain.Stream, error) {
			return service.GetUpcomingStreams(ctx, constants.HolodexAPIParams.MaxUpcomingHours+1)
		},
		func(ctx context.Context) ([]*domain.Stream, error) {
			return service.GetUpcomingStreams(ctx, constants.HolodexAPIParams.MaxUpcomingHours+2)
		},
	}
	results := make([]<-chan streamFillTestResult, len(queries))

	for i, query := range queries {
		results[i] = startStreamFillTestCall(t.Context(), query)
	}

	clamped := 0

	for range queries {
		params := <-entered
		if params.Get("max_upcoming_hours") == fmt.Sprint(constants.HolodexAPIParams.MaxUpcomingHours) {
			clamped++
		}
	}

	close(release)

	if clamped != 2 {
		t.Errorf("requests sharing upstream clamp = %d, want 2 independent original-hours cache keys", clamped)
	}

	for _, result := range results {
		assertStreamFillResult(t, <-result, domain.StreamStatusLive, true)
	}

	for _, query := range queries {
		streams, err := query(t.Context())
		assertStreamFillResult(t, streamFillTestResult{streams: streams, err: err}, domain.StreamStatusLive, true)
	}

	if got := calls.Load(); int(got) != len(queries) {
		t.Errorf("origin calls = %d, want one per distinct cache key (%d)", got, len(queries))
	}

	assertStreamFillIdle(t, service)
}

func TestStreamCacheFill_FailedCallersEachFetchOnce(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	originErr := errors.New("origin unavailable")

	var (
		calls   atomic.Int32
		healthy atomic.Bool
	)

	requester := &MockRequester{DoRequestFunc: func(ctx context.Context, _, _ string, _ url.Values) ([]byte, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}

		select {
		case <-release:
			if healthy.Load() {
				return []byte("[]"), nil
			}

			return nil, originErr
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	service := newStreamFillTestService(t, requester)

	const callers = 6

	results := make([]<-chan streamFillTestResult, callers)

	results[0] = startStreamFillTestCall(t.Context(), service.GetLiveStreams)

	<-entered

	for i := 1; i < callers; i++ {
		ctx := &streamFillReadObserver{waiting: make(chan struct{})}

		results[i] = startStreamFillTestCall(context.WithValue(t.Context(), streamFillObservationKey{}, ctx), service.GetLiveStreams)
		<-ctx.waiting
	}

	close(release)

	for _, result := range results {
		if got := <-result; !errors.Is(got.err, originErr) {
			t.Errorf("failed query error = %v, want origin failure", got.err)
		}
	}

	if got := calls.Load(); got != callers {
		t.Errorf("failed origin calls = %d, want each caller's single fetch (%d)", got, callers)
	}

	assertStreamFillIdle(t, service)
	healthy.Store(true)

	streams, err := service.GetLiveStreams(t.Context())
	assertStreamFillResult(t, streamFillTestResult{streams: streams, err: err}, domain.StreamStatusLive, true)

	if got := calls.Load(); got != callers+1 {
		t.Errorf("origin calls after recovery = %d, want %d (failures must not cache empty success)", got, callers+1)
	}

	assertStreamFillIdle(t, service)
}

func TestStreamCacheFillGate_FollowerDeadlineAndCanceledWake(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var gate streamCacheFillGate

		if err := gate.acquire(t.Context(), "live"); err != nil {
			t.Fatal(err)
		}

		deadlineCtx, cancelDeadline := context.WithTimeout(t.Context(), time.Second)
		defer cancelDeadline()

		deadlineResult := make(chan error, 1)

		go func() { deadlineResult <- gate.acquire(deadlineCtx, "live") }()

		if err := <-deadlineResult; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("waiting follower error = %v, want deadline exceeded", err)
		}

		ctx, cancel := context.WithCancel(t.Context())
		canceledResult := make(chan error, 1)

		go func() { canceledResult <- gate.acquire(ctx, "live") }()

		synctest.Wait()
		cancel()
		gate.release("live")

		if err := <-canceledResult; !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled wake error = %v, want canceled", err)
		}

		if err := gate.acquire(ctx, "live"); !errors.Is(err, context.Canceled) {
			t.Fatalf("already canceled acquisition = %v, want canceled", err)
		}

		if err := gate.acquire(t.Context(), "live"); err != nil {
			t.Fatalf("valid caller after canceled follower: %v", err)
		}

		gate.release("live")

		if len(gate.owners) != 0 {
			t.Errorf("finished gate retained keys: %v", gate.owners)
		}
	})
}

type streamFillFaultCache struct {
	cache.KeyValueCache

	readFails  bool
	writeFails bool
}

func (c streamFillFaultCache) Get(ctx context.Context, key string, dest any) error {
	err := c.KeyValueCache.Get(ctx, key, dest)
	if c.readFails {
		return errors.New("cache read unavailable")
	}

	if err != nil {
		return fmt.Errorf("read fault test cache: %w", err)
	}

	return nil
}

func (c streamFillFaultCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if c.writeFails {
		return errors.New("cache write unavailable")
	}

	if err := c.KeyValueCache.Set(ctx, key, value, ttl); err != nil {
		return fmt.Errorf("write fault test cache: %w", err)
	}

	return nil
}

func TestStreamCacheFillCacheFailuresPreserveEachCaller(t *testing.T) {
	for _, readFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("read_failure=%t", readFails), func(t *testing.T) {
			payload := streamFillTestPayload(t, domain.StreamStatusLive, constants.HolodexAPIParams.OrgHololive)
			entered := make(chan struct{})
			release := make(chan struct{})

			var calls atomic.Int32

			requester := &MockRequester{DoRequestFunc: func(ctx context.Context, _, _ string, _ url.Values) ([]byte, error) {
				if calls.Add(1) == 1 {
					close(entered)
				}

				select {
				case <-release:
					return payload, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}}
			service := newStreamFillTestService(t, requester)

			service.cacheManager.cache = streamFillFaultCache{KeyValueCache: service.cacheManager.cache, readFails: readFails, writeFails: !readFails}

			owner := startStreamFillTestCall(t.Context(), service.GetLiveStreams)

			<-entered

			observer := &streamFillReadObserver{waiting: make(chan struct{})}
			follower := startStreamFillTestCall(context.WithValue(t.Context(), streamFillObservationKey{}, observer), service.GetLiveStreams)
			<-observer.waiting
			close(release)
			assertStreamFillResult(t, <-owner, domain.StreamStatusLive, false)
			assertStreamFillResult(t, <-follower, domain.StreamStatusLive, false)

			if got := calls.Load(); got != 2 {
				t.Errorf("origin calls=%d, want each caller's initial fetch", got)
			}

			assertStreamFillIdle(t, service)
		})
	}
}
