package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	sharedlogging "github.com/kapu/hololive-shared/pkg/logging"
)

// mockLockCache: lockCache mock 구현
type mockLockCache struct {
	setNXFn            func(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	compareAndDeleteFn func(ctx context.Context, key, expectedValue string) (bool, error)
	delManyFn          func(ctx context.Context, keys []string) (int64, error)
}

func (m *mockLockCache) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if m.setNXFn != nil {
		return m.setNXFn(ctx, key, value, ttl)
	}
	return true, nil
}

func (m *mockLockCache) CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error) {
	if m.compareAndDeleteFn != nil {
		return m.compareAndDeleteFn(ctx, key, expectedValue)
	}
	return true, nil
}

func (m *mockLockCache) DelMany(ctx context.Context, keys []string) (int64, error) {
	if m.delManyFn != nil {
		return m.delManyFn(ctx, keys)
	}
	return int64(len(keys)), nil
}

var testLogger = sharedlogging.NewLogger

func TestNewLocker_NilCache_ReturnsNoop(t *testing.T) {
	locker := NewLocker(nil, testLogger())
	if _, ok := locker.(noopNotificationLocker); !ok {
		t.Fatalf("expected noopNotificationLocker, got %T", locker)
	}
}

func TestTryAcquire_Success(t *testing.T) {
	cache := &mockLockCache{
		setNXFn: func(_ context.Context, _, _ string, _ time.Duration) (bool, error) {
			return true, nil
		},
	}
	locker := NewLocker(cache, testLogger())

	token, acquired, err := locker.TryAcquire(context.Background(), "lock:test", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("expected acquired=true")
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestTryAcquire_AlreadyHeld(t *testing.T) {
	cache := &mockLockCache{
		setNXFn: func(_ context.Context, _, _ string, _ time.Duration) (bool, error) {
			return false, nil
		},
	}
	locker := NewLocker(cache, testLogger())

	_, acquired, err := locker.TryAcquire(context.Background(), "lock:held", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if acquired {
		t.Fatal("expected acquired=false")
	}
}

func TestTryAcquire_ValkeyError_GracefulDegradation(t *testing.T) {
	cache := &mockLockCache{
		setNXFn: func(_ context.Context, _, _ string, _ time.Duration) (bool, error) {
			return false, errors.New("connection refused")
		},
	}
	locker := NewLocker(cache, testLogger())

	token, acquired, err := locker.TryAcquire(context.Background(), "lock:fail", time.Minute)
	if err != nil {
		t.Fatalf("expected no error on degradation, got: %v", err)
	}
	if !acquired {
		t.Fatal("expected acquired=true on degradation")
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestRelease_CASMatch(t *testing.T) {
	var deletedKey, deletedValue string
	cache := &mockLockCache{
		compareAndDeleteFn: func(_ context.Context, key, expectedValue string) (bool, error) {
			deletedKey = key
			deletedValue = expectedValue
			return true, nil
		},
	}
	locker := NewLocker(cache, testLogger())

	err := locker.Release(context.Background(), "lock:release", "my-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deletedKey != "lock:release" {
		t.Fatalf("expected key=lock:release, got %s", deletedKey)
	}
	if deletedValue == "" {
		t.Fatal("expected non-empty value")
	}
}

func TestRelease_CASMismatch(t *testing.T) {
	cache := &mockLockCache{
		compareAndDeleteFn: func(_ context.Context, _, _ string) (bool, error) {
			return false, nil
		},
	}
	locker := NewLocker(cache, testLogger())

	err := locker.Release(context.Background(), "lock:mismatch", "wrong-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRelease_CASError_NoErrorReturned(t *testing.T) {
	cache := &mockLockCache{
		compareAndDeleteFn: func(_ context.Context, _, _ string) (bool, error) {
			return false, errors.New("redis down")
		},
	}
	locker := NewLocker(cache, testLogger())

	err := locker.Release(context.Background(), "lock:error", "token")
	if err != nil {
		t.Fatalf("expected nil error on CAS failure, got: %v", err)
	}
}

func TestClaimRoom_Success(t *testing.T) {
	cache := &mockLockCache{
		setNXFn: func(_ context.Context, _, _ string, _ time.Duration) (bool, error) {
			return true, nil
		},
	}
	locker := NewLocker(cache, testLogger())

	acquired, err := locker.ClaimRoom(context.Background(), "claim:room1", time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("expected acquired=true")
	}
}

func TestClaimRoom_AlreadyClaimed(t *testing.T) {
	cache := &mockLockCache{
		setNXFn: func(_ context.Context, _, _ string, _ time.Duration) (bool, error) {
			return false, nil
		},
	}
	locker := NewLocker(cache, testLogger())

	acquired, err := locker.ClaimRoom(context.Background(), "claim:room1", time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if acquired {
		t.Fatal("expected acquired=false")
	}
}

func TestClaimRoom_ValkeyError_GracefulDegradation(t *testing.T) {
	cache := &mockLockCache{
		setNXFn: func(_ context.Context, _, _ string, _ time.Duration) (bool, error) {
			return false, errors.New("connection refused")
		},
	}
	locker := NewLocker(cache, testLogger())

	acquired, err := locker.ClaimRoom(context.Background(), "claim:fail", time.Hour)
	if err != nil {
		t.Fatalf("expected no error on degradation, got: %v", err)
	}
	if !acquired {
		t.Fatal("expected acquired=true on degradation")
	}
}

func TestReleaseRoomClaims_Success(t *testing.T) {
	var deletedKeys []string
	cache := &mockLockCache{
		delManyFn: func(_ context.Context, keys []string) (int64, error) {
			deletedKeys = keys
			return int64(len(keys)), nil
		},
	}
	locker := NewLocker(cache, testLogger())

	err := locker.ReleaseRoomClaims(context.Background(), []string{"claim:a", "claim:b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deletedKeys) != 2 {
		t.Fatalf("expected 2 keys deleted, got %d", len(deletedKeys))
	}
}

func TestReleaseRoomClaims_EmptyKeys_NoOp(t *testing.T) {
	called := false
	cache := &mockLockCache{
		delManyFn: func(_ context.Context, _ []string) (int64, error) {
			called = true
			return 0, nil
		},
	}
	locker := NewLocker(cache, testLogger())

	err := locker.ReleaseRoomClaims(context.Background(), []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Fatal("expected DelMany not to be called for empty keys")
	}
}

func TestNoop_TryAcquire_AlwaysTrue(t *testing.T) {
	locker := NewLocker(nil, testLogger())

	_, acquired, err := locker.TryAcquire(context.Background(), "any", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("expected noop to always acquire")
	}
}

func TestNoop_ClaimRoom_AlwaysTrue(t *testing.T) {
	locker := NewLocker(nil, testLogger())

	acquired, err := locker.ClaimRoom(context.Background(), "any", time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("expected noop to always claim")
	}
}

func TestNoop_Release_ReleaseRoomClaims_NoOp(t *testing.T) {
	locker := NewLocker(nil, testLogger())

	if err := locker.Release(context.Background(), "key", "token"); err != nil {
		t.Fatalf("noop Release should not error: %v", err)
	}
	if err := locker.ReleaseRoomClaims(context.Background(), []string{"a", "b"}); err != nil {
		t.Fatalf("noop ReleaseRoomClaims should not error: %v", err)
	}
}
