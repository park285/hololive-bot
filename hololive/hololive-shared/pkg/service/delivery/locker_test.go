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

package delivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
)

// mockLockCache: lockCache mock 구현.
type mockLockCache struct {
	setNXFn            func(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	compareAndDeleteFn func(ctx context.Context, key, expectedValue string) (bool, error)
	delManyFn          func(ctx context.Context, keys []string) (int64, error)
}

func (m *mockLockCache) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if m.setNXFn != nil {
		out, err := m.setNXFn(ctx, key, value, ttl)
		if err != nil {
			return out, fmt.Errorf("set NX fn: %w", err)
		}

		return out, nil
	}

	return true, nil
}

func (m *mockLockCache) CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error) {
	if m.compareAndDeleteFn != nil {
		out, err := m.compareAndDeleteFn(ctx, key, expectedValue)
		if err != nil {
			return out, fmt.Errorf("compare and delete fn: %w", err)
		}

		return out, nil
	}

	return true, nil
}

func (m *mockLockCache) DelMany(ctx context.Context, keys []string) (int64, error) {
	if m.delManyFn != nil {
		out, err := m.delManyFn(ctx, keys)
		if err != nil {
			return out, fmt.Errorf("del many fn: %w", err)
		}

		return out, nil
	}

	return int64(len(keys)), nil
}

func testLogger() *slog.Logger {
	return sharedlogging.NewTestLogger()
}

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

	token, acquired, err := locker.TryAcquire(t.Context(), "lock:test", time.Minute)
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

	_, acquired, err := locker.TryAcquire(t.Context(), "lock:held", time.Minute)
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

	token, acquired, err := locker.TryAcquire(t.Context(), "lock:fail", time.Minute)
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

	err := locker.Release(t.Context(), "lock:release", "my-token")
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

	err := locker.Release(t.Context(), "lock:mismatch", "wrong-token")
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

	err := locker.Release(t.Context(), "lock:error", "token")
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

	acquired, err := locker.ClaimRoom(t.Context(), "claim:room1", time.Hour)
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

	acquired, err := locker.ClaimRoom(t.Context(), "claim:room1", time.Hour)
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

	acquired, err := locker.ClaimRoom(t.Context(), "claim:fail", time.Hour)
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

	err := locker.ReleaseRoomClaims(t.Context(), []string{"claim:a", "claim:b"})
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

	err := locker.ReleaseRoomClaims(t.Context(), []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if called {
		t.Fatal("expected DelMany not to be called for empty keys")
	}
}

func TestNoop_TryAcquire_AlwaysTrue(t *testing.T) {
	locker := NewLocker(nil, testLogger())

	_, acquired, err := locker.TryAcquire(t.Context(), "any", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !acquired {
		t.Fatal("expected noop to always acquire")
	}
}

func TestNoop_ClaimRoom_AlwaysTrue(t *testing.T) {
	locker := NewLocker(nil, testLogger())

	acquired, err := locker.ClaimRoom(t.Context(), "any", time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !acquired {
		t.Fatal("expected noop to always claim")
	}
}

func TestNoop_Release_ReleaseRoomClaims_NoOp(t *testing.T) {
	locker := NewLocker(nil, testLogger())

	if err := locker.Release(t.Context(), "key", "token"); err != nil {
		t.Fatalf("noop Release should not error: %v", err)
	}

	if err := locker.ReleaseRoomClaims(t.Context(), []string{"a", "b"}); err != nil {
		t.Fatalf("noop ReleaseRoomClaims should not error: %v", err)
	}
}
