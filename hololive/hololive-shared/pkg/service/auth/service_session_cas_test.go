package auth

import (
	"context"
	stdErrors "errors"
	"sync"
	"testing"

	"github.com/kapu/hololive-shared/pkg/testutil"
	sharedlogging "github.com/park285/shared-go/pkg/logging"
)

func newRefreshTestService(t *testing.T) (*Service, string) {
	t.Helper()

	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t, context.Background())

	service, err := NewService(context.Background(), db, cacheClient, sharedlogging.NewTestLogger(), DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	if _, err := service.Register(context.Background(), "user@example.com", "Password1", "User"); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	session, _, err := service.Login(context.Background(), "user@example.com", "Password1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	return service, session.Token
}

// 동일 토큰으로 연속 refresh 시 두 번째는 replay로 거부되어야 한다 (compare-and-delete).
func TestRefresh_SequentialReplayRejected(t *testing.T) {
	service, token := newRefreshTestService(t)

	first, err := service.Refresh(context.Background(), token)
	if err != nil {
		t.Fatalf("first refresh failed: %v", err)
	}
	if first == nil || first.Token == "" {
		t.Fatalf("expected rotated session")
	}

	_, err = service.Refresh(context.Background(), token)
	if err == nil {
		t.Fatalf("expected replay of consumed token to be rejected")
	}
	assertAuthCode(t, err, CodeUnauthorized)
}

// 동일 토큰으로 동시 refresh 시 정확히 하나만 성공해야 한다 (원자적 회전).
func TestRefresh_ConcurrentRotationSingleWinner(t *testing.T) {
	service, token := newRefreshTestService(t)

	const goroutines = 8
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		successes int
		unauth    int
	)

	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			_, err := service.Refresh(context.Background(), token)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				successes++
			default:
				var ae *Error
				if stdErrors.As(err, &ae) && ae.Code == CodeUnauthorized {
					unauth++
				}
			}
		}()
	}
	wg.Wait()

	if successes != 1 {
		t.Fatalf("expected exactly 1 successful refresh, got=%d (unauthorized=%d)", successes, unauth)
	}
	if unauth != goroutines-1 {
		t.Fatalf("expected %d unauthorized replays, got=%d", goroutines-1, unauth)
	}
}

// 회전 성공 후 이전 토큰의 세션 키는 원자적으로 삭제되어야 한다.
func TestRefresh_ConsumesOldSessionKey(t *testing.T) {
	service, token := newRefreshTestService(t)

	oldKey := sessionKeyPrefix + sha256Hex(token)

	if _, err := service.Refresh(context.Background(), token); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	exists, err := service.cacheClient.Exists(context.Background(), oldKey)
	if err != nil {
		t.Fatalf("exists check failed: %v", err)
	}
	if exists {
		t.Fatalf("expected old session key to be deleted after rotation")
	}
}
