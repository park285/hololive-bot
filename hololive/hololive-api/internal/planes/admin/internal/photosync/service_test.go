package photosync

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testChannelID = "channel-1"

type periodicMemberStore struct {
	queryCount atomic.Int32
	reached    chan struct{}
	closeOnce  sync.Once
}

func (r *periodicMemberStore) membersNeedingSync(context.Context, time.Duration) ([]string, error) {
	if r.queryCount.Add(1) >= 2 {
		r.closeOnce.Do(func() {
			close(r.reached)
		})
	}

	return nil, nil
}

func (r *periodicMemberStore) updatePhoto(context.Context, string, string) error {
	return nil
}

type retryMemberStore struct {
	errors      []error
	channelIDs  []string
	updateErr   error
	onCall      func(int)
	calls       int
	updateCalls int
	updated     map[string]string
}

func (r *retryMemberStore) membersNeedingSync(context.Context, time.Duration) ([]string, error) {
	r.calls++
	if r.onCall != nil {
		r.onCall(r.calls)
	}

	if r.calls <= len(r.errors) {
		return nil, r.errors[r.calls-1]
	}

	return r.channelIDs, nil
}

func (r *retryMemberStore) updatePhoto(_ context.Context, channelID, photoURL string) error {
	r.updateCalls++
	if r.updateErr != nil {
		return r.updateErr
	}

	if r.updated == nil {
		r.updated = map[string]string{}
	}

	r.updated[channelID] = photoURL

	return nil
}

type stubPhotoSource map[string]string

func (s stubPhotoSource) HololiveChannelPhotos(context.Context) (map[string]string, error) {
	return maps.Clone(s), nil
}

func TestPhotoSyncRunPeriodicSyncLogsStoppedWhenContextCanceled(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	ps := &Service{
		logger:       slog.New(slog.NewTextHandler(&logs, nil)),
		syncInterval: time.Hour,
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	ps.runPeriodicSync(ctx)

	if !strings.Contains(logs.String(), "Photo sync service stopped") {
		t.Fatalf("runPeriodicSync() log = %q, want stop message", logs.String())
	}
}

func TestPhotoSyncRunPeriodicSyncCallsSyncOnPeriodicTick(t *testing.T) {
	t.Parallel()

	reached := make(chan struct{})
	store := &periodicMemberStore{reached: reached}

	ps := &Service{
		members:        store,
		logger:         slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		syncInterval:   10 * time.Millisecond,
		staleThreshold: time.Hour,
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		ps.runPeriodicSync(ctx)
		close(done)
	}()

	select {
	case <-reached:
	case <-time.After(500 * time.Millisecond):
		cancel()
		t.Fatalf("photo sync periodic query count = %d, want at least 2", store.queryCount.Load())
	}

	cancel()

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("runPeriodicSync did not stop after context cancellation")
	}
}

func TestPhotoSyncRetryPolicy(t *testing.T) {
	t.Parallel()

	options := retryOptions()
	if options.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts = %d, want 3", options.MaxAttempts)
	}

	if options.BaseDelay != 5*time.Second {
		t.Fatalf("BaseDelay = %s, want 5s", options.BaseDelay)
	}

	if options.Jitter != 2*time.Second {
		t.Fatalf("Jitter = %s, want 2s", options.Jitter)
	}
}

func TestPhotoSyncWithRetrySuccess(t *testing.T) {
	t.Parallel()

	store := &retryMemberStore{}
	logs := &bytes.Buffer{}
	ps := &Service{
		members:        store,
		logger:         slog.New(slog.NewTextHandler(logs, nil)),
		staleThreshold: time.Hour,
	}
	options := retryOptions()

	options.Sleep = func(context.Context, time.Duration) bool {
		t.Fatal("unexpected retry sleep")

		return false
	}

	ps.syncWithRetry(t.Context(), options)

	if store.calls != 1 {
		t.Fatalf("sync attempts = %d, want 1", store.calls)
	}

	assertRetryLogCounts(t, logs.String(), 0, 0)
}

func TestPhotoSyncWithRetrySucceedsAfterRetry(t *testing.T) {
	t.Parallel()

	store := &retryMemberStore{errors: []error{errors.New("temporary failure")}}
	logs := &bytes.Buffer{}
	ps := &Service{
		members:        store,
		logger:         slog.New(slog.NewTextHandler(logs, nil)),
		staleThreshold: time.Hour,
	}
	options := retryOptions()
	delays := make([]time.Duration, 0, 1)

	options.Sleep = func(_ context.Context, delay time.Duration) bool {
		delays = append(delays, delay)

		return true
	}

	ps.syncWithRetry(t.Context(), options)

	if store.calls != 2 {
		t.Fatalf("sync attempts = %d, want 2", store.calls)
	}

	if len(delays) != 1 || delays[0] < 3*time.Second || delays[0] > 7*time.Second {
		t.Fatalf("retry delays = %v, want one delay in [3s, 7s]", delays)
	}

	assertRetryLogCounts(t, logs.String(), 1, 0)
}

func TestPhotoSyncWithRetryLogsFinalFailureOnce(t *testing.T) {
	t.Parallel()

	store := &retryMemberStore{errors: []error{
		errors.New("first failure"),
		errors.New("second failure"),
		errors.New("third failure"),
	}}
	logs := &bytes.Buffer{}
	ps := &Service{
		members:        store,
		logger:         slog.New(slog.NewTextHandler(logs, nil)),
		staleThreshold: time.Hour,
	}
	options := retryOptions()
	sleepCount := 0

	options.Sleep = func(context.Context, time.Duration) bool {
		sleepCount++

		return true
	}

	ps.syncWithRetry(t.Context(), options)

	if store.calls != 3 {
		t.Fatalf("sync attempts = %d, want 3", store.calls)
	}

	if sleepCount != 2 {
		t.Fatalf("retry sleeps = %d, want 2", sleepCount)
	}

	// 재시도 직전 실패 2번만 "will retry"이고, 마지막 실패는 원인 오류를 담은 Error 한 줄이다.
	assertRetryLogCounts(t, logs.String(), 2, 1)

	if !strings.Contains(logs.String(), "third failure") {
		t.Fatalf("final failure log does not carry the last error; logs = %q", logs.String())
	}
}

func TestPhotoSyncWithRetryStopsOnCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	store := &retryMemberStore{
		errors: []error{errors.New("temporary failure")},
		onCall: func(int) {
			cancel()
		},
	}
	logs := &bytes.Buffer{}
	ps := &Service{
		members:        store,
		logger:         slog.New(slog.NewTextHandler(logs, nil)),
		staleThreshold: time.Hour,
	}

	ps.syncWithRetry(ctx, retryOptions())

	if store.calls != 1 {
		t.Fatalf("sync attempts = %d, want 1", store.calls)
	}

	assertRetryLogCounts(t, logs.String(), 1, 0)
}

func TestPhotoSyncWithRetryDoesNotRetryUpdateFailure(t *testing.T) {
	t.Parallel()

	store := &retryMemberStore{
		channelIDs: []string{testChannelID},
		updateErr:  errors.New("update failure"),
	}
	logs := &bytes.Buffer{}
	ps := &Service{
		photos:         stubPhotoSource{testChannelID: "https://example.com/photo.jpg"},
		members:        store,
		logger:         slog.New(slog.NewTextHandler(logs, nil)),
		staleThreshold: time.Hour,
	}
	options := retryOptions()

	options.Sleep = func(context.Context, time.Duration) bool {
		t.Fatal("unexpected retry sleep")

		return false
	}

	ps.syncWithRetry(t.Context(), options)

	if store.calls != 1 {
		t.Fatalf("sync attempts = %d, want 1", store.calls)
	}

	if store.updateCalls != 1 {
		t.Fatalf("update attempts = %d, want 1", store.updateCalls)
	}

	assertRetryLogCounts(t, logs.String(), 0, 0)

	if got := strings.Count(logs.String(), "Failed to update photo"); got != 1 {
		t.Fatalf("update failure log count = %d, want 1; logs = %q", got, logs.String())
	}
}

// 대상 채널 가운데 Holodex 사진이 있는 채널만 고해상도 URL로 저장한다.
func TestPhotoSyncStoresUpgradedPhotoForTargetsWithPhoto(t *testing.T) {
	t.Parallel()

	store := &retryMemberStore{channelIDs: []string{testChannelID, "channel-2"}}
	ps := &Service{
		photos: stubPhotoSource{
			testChannelID: "https://example.com/avatar=s88-c-k",
			"channel-3":   "https://example.com/other=s88",
		},
		members:        store,
		logger:         slog.New(slog.DiscardHandler),
		staleThreshold: time.Hour,
	}

	if err := ps.doSync(t.Context()); err != nil {
		t.Fatalf("doSync() error = %v", err)
	}

	want := map[string]string{testChannelID: "https://example.com/avatar=s1024-c-k"}
	if !maps.Equal(store.updated, want) {
		t.Fatalf("updated photos = %v, want %v", store.updated, want)
	}
}

func TestUpgradePhotoResolution_ReplacesKnownSizeWithSingleStandardSuffix(t *testing.T) {
	t.Parallel()

	if got := upgradePhotoResolution("https://example.com/avatar=s88=s240"); got != "https://example.com/avatar=s1024=s240" {
		t.Fatalf("upgradePhotoResolution() = %q", got)
	}
}

func assertRetryLogCounts(t *testing.T, logs string, retryCount, finalCount int) {
	t.Helper()

	if got := strings.Count(logs, "Photo sync failed, will retry"); got != retryCount {
		t.Errorf("retry log count = %d, want %d; logs = %q", got, retryCount, logs)
	}

	if got := strings.Count(logs, "Photo sync failed after all retries"); got != finalCount {
		t.Errorf("final log count = %d, want %d; logs = %q", got, finalCount, logs)
	}
}
