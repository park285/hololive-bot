package holodexprovider

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type photoSyncTestMemberRepository struct {
	queryCount atomic.Int32
	reached    chan struct{}
	closeOnce  sync.Once
}

type photoSyncRetryMemberRepository struct {
	errors      []error
	channelIDs  []string
	updateErr   error
	onCall      func(int)
	calls       int
	updateCalls int
}

func (r *photoSyncRetryMemberRepository) GetAllChannelIDs(context.Context) ([]string, error) {
	return nil, nil
}

func (r *photoSyncRetryMemberRepository) GetMembersNeedingPhotoSync(context.Context, time.Duration) ([]string, error) {
	r.calls++
	if r.onCall != nil {
		r.onCall(r.calls)
	}

	if r.calls <= len(r.errors) {
		return nil, r.errors[r.calls-1]
	}

	return r.channelIDs, nil
}

func (r *photoSyncRetryMemberRepository) UpdatePhoto(context.Context, string, string) error {
	r.updateCalls++

	return r.updateErr
}

func (r *photoSyncTestMemberRepository) GetAllChannelIDs(context.Context) ([]string, error) {
	return nil, nil
}

func (r *photoSyncTestMemberRepository) GetMembersNeedingPhotoSync(context.Context, time.Duration) ([]string, error) {
	if r.queryCount.Add(1) >= 2 {
		r.closeOnce.Do(func() {
			close(r.reached)
		})
	}

	return nil, nil
}

func (r *photoSyncTestMemberRepository) UpdatePhoto(context.Context, string, string) error {
	return nil
}

func TestPhotoSyncRunPeriodicSyncLogsStoppedWhenContextCanceled(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	ps := &PhotoSyncService{
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
	repository := &photoSyncTestMemberRepository{reached: reached}

	ps := &PhotoSyncService{
		memberRepository: repository,
		logger:           slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		syncInterval:     10 * time.Millisecond,
		staleThreshold:   time.Hour,
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
		t.Fatalf("photo sync periodic query count = %d, want at least 2", repository.queryCount.Load())
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

	options := photoSyncRetryOptions()
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

	repository := &photoSyncRetryMemberRepository{}
	logs := &bytes.Buffer{}
	ps := &PhotoSyncService{
		memberRepository: repository,
		logger:           slog.New(slog.NewTextHandler(logs, nil)),
		staleThreshold:   time.Hour,
	}
	options := photoSyncRetryOptions()

	options.Sleep = func(context.Context, time.Duration) bool {
		t.Fatal("unexpected retry sleep")

		return false
	}

	ps.syncWithRetry(t.Context(), options)

	if repository.calls != 1 {
		t.Fatalf("sync attempts = %d, want 1", repository.calls)
	}

	assertPhotoSyncRetryLogCounts(t, logs.String(), 0, 0)
}

func TestPhotoSyncWithRetrySucceedsAfterRetry(t *testing.T) {
	t.Parallel()

	repository := &photoSyncRetryMemberRepository{errors: []error{errors.New("temporary failure")}}
	logs := &bytes.Buffer{}
	ps := &PhotoSyncService{
		memberRepository: repository,
		logger:           slog.New(slog.NewTextHandler(logs, nil)),
		staleThreshold:   time.Hour,
	}
	options := photoSyncRetryOptions()
	delays := make([]time.Duration, 0, 1)

	options.Sleep = func(_ context.Context, delay time.Duration) bool {
		delays = append(delays, delay)

		return true
	}

	ps.syncWithRetry(t.Context(), options)

	if repository.calls != 2 {
		t.Fatalf("sync attempts = %d, want 2", repository.calls)
	}

	if len(delays) != 1 || delays[0] < 3*time.Second || delays[0] > 7*time.Second {
		t.Fatalf("retry delays = %v, want one delay in [3s, 7s]", delays)
	}

	assertPhotoSyncRetryLogCounts(t, logs.String(), 1, 0)
}

func TestPhotoSyncWithRetryLogsFinalFailureOnce(t *testing.T) {
	t.Parallel()

	repository := &photoSyncRetryMemberRepository{errors: []error{
		errors.New("first failure"),
		errors.New("second failure"),
		errors.New("third failure"),
	}}
	logs := &bytes.Buffer{}
	ps := &PhotoSyncService{
		memberRepository: repository,
		logger:           slog.New(slog.NewTextHandler(logs, nil)),
		staleThreshold:   time.Hour,
	}
	options := photoSyncRetryOptions()
	sleepCount := 0

	options.Sleep = func(context.Context, time.Duration) bool {
		sleepCount++

		return true
	}

	ps.syncWithRetry(t.Context(), options)

	if repository.calls != 3 {
		t.Fatalf("sync attempts = %d, want 3", repository.calls)
	}

	if sleepCount != 2 {
		t.Fatalf("retry sleeps = %d, want 2", sleepCount)
	}

	assertPhotoSyncRetryLogCounts(t, logs.String(), 3, 1)
}

func TestPhotoSyncWithRetryStopsOnCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	repository := &photoSyncRetryMemberRepository{
		errors: []error{errors.New("temporary failure")},
		onCall: func(int) {
			cancel()
		},
	}
	logs := &bytes.Buffer{}
	ps := &PhotoSyncService{
		memberRepository: repository,
		logger:           slog.New(slog.NewTextHandler(logs, nil)),
		staleThreshold:   time.Hour,
	}

	ps.syncWithRetry(ctx, photoSyncRetryOptions())

	if repository.calls != 1 {
		t.Fatalf("sync attempts = %d, want 1", repository.calls)
	}

	assertPhotoSyncRetryLogCounts(t, logs.String(), 1, 0)
}

func TestPhotoSyncWithRetryDoesNotRetryUpdateFailure(t *testing.T) {
	t.Parallel()

	requester := &MockRequester{
		DoRequestFunc: func(context.Context, string, string, url.Values) ([]byte, error) {
			return []byte(`[{"id":"channel-1","name":"Member","photo":"https://example.com/photo.jpg"}]`), nil
		},
	}
	repository := &photoSyncRetryMemberRepository{
		channelIDs: []string{"channel-1"},
		updateErr:  errors.New("update failure"),
	}
	logs := &bytes.Buffer{}
	ps := &PhotoSyncService{
		holodex:          newServiceForFallbackTest(requester),
		memberRepository: repository,
		logger:           slog.New(slog.NewTextHandler(logs, nil)),
		staleThreshold:   time.Hour,
	}
	options := photoSyncRetryOptions()

	options.Sleep = func(context.Context, time.Duration) bool {
		t.Fatal("unexpected retry sleep")

		return false
	}

	ps.syncWithRetry(t.Context(), options)

	if repository.calls != 1 {
		t.Fatalf("sync attempts = %d, want 1", repository.calls)
	}

	if repository.updateCalls != 1 {
		t.Fatalf("update attempts = %d, want 1", repository.updateCalls)
	}

	assertPhotoSyncRetryLogCounts(t, logs.String(), 0, 0)

	if got := strings.Count(logs.String(), "Failed to update photo"); got != 1 {
		t.Fatalf("update failure log count = %d, want 1; logs = %q", got, logs.String())
	}
}

func assertPhotoSyncRetryLogCounts(t *testing.T, logs string, retryCount, finalCount int) {
	t.Helper()

	if got := strings.Count(logs, "Photo sync failed, will retry"); got != retryCount {
		t.Errorf("retry log count = %d, want %d; logs = %q", got, retryCount, logs)
	}

	if got := strings.Count(logs, "Photo sync failed after all retries"); got != finalCount {
		t.Errorf("final log count = %d, want %d; logs = %q", got, finalCount, logs)
	}
}
