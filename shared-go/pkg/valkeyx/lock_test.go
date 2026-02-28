package valkeyx

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

func setupTestClient(t *testing.T) (valkey.Client, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:  []string{mr.Addr()},
		DisableCache: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		client.Close()
	})

	return client, mr
}

func TestAcquire_Success(t *testing.T) {
	client, _ := setupTestClient(t)
	ctx := context.Background()

	cfg := DefaultLockConfig()
	cfg.TTL = 5 * time.Second
	cfg.KeyPrefix = "test:"

	lock, acquired, err := Acquire(ctx, client, "resource1", cfg)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotNil(t, lock)

	err = lock.Unlock(ctx)
	assert.NoError(t, err)
}

func TestAcquire_Concurrent_SecondAttemptFails(t *testing.T) {
	client, _ := setupTestClient(t)
	ctx := context.Background()

	cfg := DefaultLockConfig()
	cfg.TTL = 3 * time.Second
	cfg.MaxRetries = 1
	cfg.KeyPrefix = "test:"

	lock1, acquired1, err := Acquire(ctx, client, "contested_resource", cfg)
	require.NoError(t, err)
	require.True(t, acquired1)

	lock2, acquired2, err := TryAcquire(ctx, client, "contested_resource", cfg)
	require.NoError(t, err)
	assert.False(t, acquired2, "second acquire should fail when lock is held")
	assert.Nil(t, lock2)

	_ = lock1.Unlock(ctx)
}

func TestAcquire_ReleaseAndReacquire(t *testing.T) {
	client, _ := setupTestClient(t)
	ctx := context.Background()

	cfg := DefaultLockConfig()
	cfg.TTL = 5 * time.Second
	cfg.KeyPrefix = "test:"

	lock1, acquired, err := Acquire(ctx, client, "resource2", cfg)
	require.NoError(t, err)
	require.True(t, acquired)

	err = lock1.Unlock(ctx)
	require.NoError(t, err)

	lock2, acquired, err := Acquire(ctx, client, "resource2", cfg)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotNil(t, lock2)

	err = lock2.Unlock(ctx)
	assert.NoError(t, err)
}

func TestAcquire_WithRetry(t *testing.T) {
	client, _ := setupTestClient(t)
	ctx := context.Background()

	cfg := DefaultLockConfig()
	cfg.TTL = 500 * time.Millisecond
	cfg.MaxRetries = 10
	cfg.RetryInterval = 50 * time.Millisecond
	cfg.KeyPrefix = "test:"

	lock1, acquired, err := Acquire(ctx, client, "resource3", cfg)
	require.NoError(t, err)
	require.True(t, acquired)

	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = lock1.Unlock(ctx)
	}()

	lock2, acquired, err := Acquire(ctx, client, "resource3", cfg)
	require.NoError(t, err)
	require.True(t, acquired)

	_ = lock2.Unlock(ctx)
}

func TestAcquire_WithHolder(t *testing.T) {
	client, _ := setupTestClient(t)
	ctx := context.Background()

	cfg := DefaultLockConfig()
	cfg.TTL = 5 * time.Second
	cfg.EnableHolder = true
	cfg.HolderName = "user123"
	cfg.KeyPrefix = "test:"

	lock, acquired, err := Acquire(ctx, client, "resource4", cfg)
	require.NoError(t, err)
	require.True(t, acquired)

	holderKey := "test:holder:{resource4}"
	cmd := client.B().Get().Key(holderKey).Build()
	value, err := client.Do(ctx, cmd).ToString()
	require.NoError(t, err)
	assert.Contains(t, value, "user123")

	err = lock.Unlock(ctx)
	require.NoError(t, err)

	cmd = client.B().Get().Key(holderKey).Build()
	_, err = client.Do(ctx, cmd).ToString()
	assert.Error(t, err)
}

func TestExtend_Success(t *testing.T) {
	client, _ := setupTestClient(t)
	ctx := context.Background()

	cfg := DefaultLockConfig()
	cfg.TTL = 1 * time.Second
	cfg.KeyPrefix = "test:"

	lock, acquired, err := Acquire(ctx, client, "resource6", cfg)
	require.NoError(t, err)
	require.True(t, acquired)

	renewed, err := lock.Extend(ctx, 5*time.Second)
	require.NoError(t, err)
	assert.True(t, renewed)

	lockKey := "test:{resource6}"
	cmd := client.B().Ttl().Key(lockKey).Build()
	ttl, err := client.Do(ctx, cmd).AsInt64()
	require.NoError(t, err)
	assert.Greater(t, ttl, int64(3))

	err = lock.Unlock(ctx)
	assert.NoError(t, err)
}

func TestTryAcquire_NoBlocking(t *testing.T) {
	client, _ := setupTestClient(t)
	ctx := context.Background()

	cfg := DefaultLockConfig()
	cfg.TTL = 5 * time.Second
	cfg.KeyPrefix = "test:"

	lock1, acquired, err := TryAcquire(ctx, client, "resource7", cfg)
	require.NoError(t, err)
	require.True(t, acquired)

	start := time.Now()
	lock2, acquired, err := TryAcquire(ctx, client, "resource7", cfg)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Nil(t, lock2)
	assert.Less(t, elapsed, 100*time.Millisecond, "should fail immediately without blocking")

	_ = lock1.Unlock(ctx)
}

func TestAcquire_ContextCancellation(t *testing.T) {
	client, _ := setupTestClient(t)

	cfg := DefaultLockConfig()
	cfg.TTL = 10 * time.Second
	cfg.MaxRetries = 100
	cfg.RetryInterval = 100 * time.Millisecond
	cfg.KeyPrefix = "test:"

	lock1, acquired, err := Acquire(context.Background(), client, "resource8", cfg)
	require.NoError(t, err)
	require.True(t, acquired)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	lock2, acquired, err := Acquire(ctx, client, "resource8", cfg)
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Nil(t, lock2)

	_ = lock1.Unlock(context.Background())
}

func TestAcquire_EmptyKey(t *testing.T) {
	client, _ := setupTestClient(t)
	ctx := context.Background()

	cfg := DefaultLockConfig()

	lock, acquired, err := Acquire(ctx, client, "", cfg)
	assert.Error(t, err)
	assert.False(t, acquired)
	assert.Nil(t, lock)
	assert.Contains(t, err.Error(), "empty")
}

func TestAcquire_NilClient(t *testing.T) {
	ctx := context.Background()
	cfg := DefaultLockConfig()

	lock, acquired, err := Acquire(ctx, nil, "resource", cfg)
	assert.Error(t, err)
	assert.False(t, acquired)
	assert.Nil(t, lock)
	assert.Contains(t, err.Error(), "nil")
}
