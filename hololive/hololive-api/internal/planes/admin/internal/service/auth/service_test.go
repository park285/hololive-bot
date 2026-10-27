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

package auth

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/valkey-io/valkey-go"
	"golang.org/x/crypto/bcrypt"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

type failingCacheClient struct {
	cache.Client

	nilDoMulti bool
	// casKeyConflict가 지정되면 해당 키의 CompareAndDelete가 (false, nil)을 반환해
	// 동시 회전(다른 요청이 먼저 claim)을 시뮬레이션한다.
	casKeyConflict string
	// beforeCompareAndDelete가 지정되면 CompareAndDelete 직전에 한 번 실행된다.
	// Refresh의 세대 확인과 claim 사이에 끼어드는 세대 상승을 결정적으로 재현한다.
	beforeCompareAndDelete func()
}

func (c *failingCacheClient) CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error) {
	if hook := c.beforeCompareAndDelete; hook != nil {
		c.beforeCompareAndDelete = nil

		hook()
	}

	if c.casKeyConflict != "" && key == c.casKeyConflict {
		return false, nil
	}

	out, err := c.Client.CompareAndDelete(ctx, key, expectedValue)
	if err != nil {
		return out, fmt.Errorf("compare and delete: %w", err)
	}

	return out, nil
}

func (c *failingCacheClient) DoMulti(ctx context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult {
	if c.nilDoMulti {
		return nil
	}

	return c.Client.DoMulti(ctx, cmds...)
}

func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	return dbtest.NewPool(t)
}

// testConfig는 운영 기본값에서 bcrypt 비용만 최솟값으로 낮춘다. 비용 12 해시는 race 실행에서 회당 약 4초라
// NewService의 더미 해시만으로 패키지 시간이 커진다. 운영 기본 비용은 service_bcrypt_cost_test.go가 검증한다.
func testConfig() Config {
	config := DefaultConfig()

	config.BcryptCost = bcrypt.MinCost

	return config
}

func assertAuthCode(t *testing.T, err error, want ErrorCode) {
	t.Helper()

	ae, ok := stdErrors.AsType[*Error](err)
	if !ok {
		t.Fatalf("expected *auth.Error, got: %T (%v)", err, err)
	}

	if ae.Code != want {
		t.Fatalf("unexpected code: got=%s want=%s", ae.Code, want)
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	db := newTestDB(t)

	service, err := NewService(db, nil, slog.New(slog.DiscardHandler), testConfig())
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = service.Register(t.Context(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	_, err = service.Register(t.Context(), "USER@example.com", "Password1", "User2")
	if err == nil {
		t.Fatal("expected duplicate error, got nil")
	}

	assertAuthCode(t, err, CodeEmailExists)
}

func TestLogin_SessionFlow(t *testing.T) {
	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t.Context(), t)

	config := testConfig()

	config.SessionTTL = 30 * time.Minute

	service, err := NewService(db, cacheClient, slog.New(slog.DiscardHandler), config)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = service.Register(t.Context(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	session, user, err := service.Login(t.Context(), "user@example.com", "Password1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	if session == nil || session.Token == "" {
		t.Fatal("expected session token")
	}

	if user == nil || user.ID == "" {
		t.Fatal("expected user")
	}

	me, err := service.Me(t.Context(), session.Token)
	if err != nil {
		t.Fatalf("me failed: %v", err)
	}

	if me.ID != user.ID {
		t.Fatalf("unexpected me user: got=%s want=%s", me.ID, user.ID)
	}

	refreshed, err := service.Refresh(t.Context(), session.Token)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	_, err = service.Me(t.Context(), session.Token)
	if err == nil {
		t.Fatal("expected old token to be invalid after refresh")
	}

	assertAuthCode(t, err, CodeUnauthorized)

	_, err = service.Me(t.Context(), refreshed.Token)
	if err != nil {
		t.Fatalf("me with refreshed token failed: %v", err)
	}

	if logoutErr := service.Logout(t.Context(), refreshed.Token); logoutErr != nil {
		t.Fatalf("logout failed: %v", logoutErr)
	}

	_, err = service.Me(t.Context(), refreshed.Token)
	if err == nil {
		t.Fatal("expected token to be invalid after logout")
	}

	assertAuthCode(t, err, CodeUnauthorized)
}

func TestLogin_RateLimited(t *testing.T) {
	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t.Context(), t)

	config := testConfig()

	config.LoginRateLimitPerMinute = 2
	config.LoginFailLimit = 100 // 레이트리밋 테스트에서 락 영향 제거

	service, err := NewService(db, cacheClient, slog.New(slog.DiscardHandler), config)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = service.Register(t.Context(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	_, _, err = service.Login(t.Context(), "user@example.com", "WrongPass1", "1.2.3.4")
	if err == nil {
		t.Fatal("expected login failure")
	}

	assertAuthCode(t, err, CodeInvalidCredentials)

	_, _, err = service.Login(t.Context(), "user@example.com", "WrongPass1", "1.2.3.4")
	if err == nil {
		t.Fatal("expected login failure")
	}

	assertAuthCode(t, err, CodeInvalidCredentials)

	_, _, err = service.Login(t.Context(), "user@example.com", "WrongPass1", "1.2.3.4")
	if err == nil {
		t.Fatal("expected rate limited error")
	}

	assertAuthCode(t, err, CodeRateLimited)
}

func TestLogin_AccountLocked(t *testing.T) {
	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t.Context(), t)

	config := testConfig()

	config.LoginRateLimitPerMinute = 1000
	config.LoginFailLimit = 3
	config.LoginFailWindow = 10 * time.Minute
	config.LoginLockDuration = 10 * time.Minute

	service, err := NewService(db, cacheClient, slog.New(slog.DiscardHandler), config)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = service.Register(t.Context(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	for i := range 3 {
		_, _, err = service.Login(t.Context(), "user@example.com", "WrongPass1", "127.0.0.1")
		if err == nil {
			t.Fatalf("expected login failure at attempt %d", i+1)
		}

		assertAuthCode(t, err, CodeInvalidCredentials)
	}

	_, _, err = service.Login(t.Context(), "user@example.com", "Password1", "127.0.0.1")
	if err == nil {
		t.Fatal("expected account locked error")
	}

	assertAuthCode(t, err, CodeAccountLocked)
}

func TestLogin_UnknownEmailRecordsFailureAndLocksAccount(t *testing.T) {
	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t.Context(), t)

	config := testConfig()

	config.LoginRateLimitPerMinute = 1000
	config.LoginFailLimit = 1
	config.LoginFailWindow = 10 * time.Minute
	config.LoginLockDuration = 10 * time.Minute

	service, err := NewService(db, cacheClient, slog.New(slog.DiscardHandler), config)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, _, err = service.Login(t.Context(), "missing@example.com", "Password1", "127.0.0.1")
	if err == nil {
		t.Fatal("expected invalid credentials for unknown email")
	}

	assertAuthCode(t, err, CodeInvalidCredentials)

	_, _, err = service.Login(t.Context(), "missing@example.com", "Password1", "127.0.0.1")
	if err == nil {
		t.Fatal("expected account locked after unknown-email failure hook")
	}

	assertAuthCode(t, err, CodeAccountLocked)
}

func TestMe_ReturnsUnauthorizedWhenSessionUserIsMissing(t *testing.T) {
	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t.Context(), t)

	service, err := NewService(db, cacheClient, slog.New(slog.DiscardHandler), testConfig())
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	session, err := service.createSession(t.Context(), "missing-user", 0)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err = service.Me(t.Context(), session.Token)
	if err == nil {
		t.Fatal("expected unauthorized for missing user")
	}

	assertAuthCode(t, err, CodeUnauthorized)
}

// CAS 회전에서 old session claim이 실패하면(동시 회전) 새 세션을 만들지 않고
// CodeUnauthorized를 반환해야 한다. 기존 old session 키는 다른 요청이 소유하므로 건드리지 않는다.
func TestRefresh_RejectsWhenSessionAlreadyClaimed(t *testing.T) {
	db := newTestDB(t)
	baseCache := testutil.NewTestCacheService(t.Context(), t)

	service, err := NewService(db, baseCache, slog.New(slog.DiscardHandler), testConfig())
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = service.Register(t.Context(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	session, _, err := service.Login(t.Context(), "user@example.com", "Password1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	oldKey := sessionKeyPrefix + sha256Hex(session.Token)

	service.cacheClient = &failingCacheClient{
		Client:         baseCache,
		casKeyConflict: oldKey,
	}

	_, err = service.Refresh(t.Context(), session.Token)
	if err == nil {
		t.Fatal("expected refresh to be rejected when session already claimed")
	}

	assertAuthCode(t, err, CodeUnauthorized)

	var sessionKeys []string

	err = baseCache.ScanKeyPages(t.Context(), sessionKeyPrefix+"*", 10, func(keys []string) error {
		sessionKeys = append(sessionKeys, keys...)
		return nil
	})
	if err != nil {
		t.Fatalf("scan session keys: %v", err)
	}

	if len(sessionKeys) != 1 || sessionKeys[0] != oldKey {
		t.Fatalf("expected only old session key to remain (no new session), got=%v", sessionKeys)
	}
}

func TestIncrWithTTL_SetsTTLOnFirstIncrementAndKeepsIt(t *testing.T) {
	cacheClient, mini := testutil.NewTestCacheServiceWithMini(t.Context(), t)

	key := "auth:test:counter"

	count, err := incrWithTTL(t.Context(), cacheClient, key, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("first incr failed: %v", err)
	}

	if count != 1 {
		t.Fatalf("unexpected first count: got=%d want=1", count)
	}

	firstTTL := mini.TTL(key)
	if firstTTL != time.Second {
		t.Fatalf("expected first ttl to ceil to 1s, got=%v", firstTTL)
	}

	count, err = incrWithTTL(t.Context(), cacheClient, key, 2*time.Second)
	if err != nil {
		t.Fatalf("second incr failed: %v", err)
	}

	if count != 2 {
		t.Fatalf("unexpected second count: got=%d want=2", count)
	}

	secondTTL := mini.TTL(key)
	if secondTTL != firstTTL {
		t.Fatalf("expected ttl to stay unchanged after second incr, got=%v want=%v", secondTTL, firstTTL)
	}
}

func TestIncrWithTTL_HealsCounterWithoutTTLOnNextIncrement(t *testing.T) {
	cacheClient, mini := testutil.NewTestCacheServiceWithMini(t.Context(), t)

	key := "auth:test:counter:no-ttl"
	results := cacheClient.DoMulti(t.Context(), cacheClient.B().Incr().Key(key).Build())

	if len(results) != 1 {
		t.Fatalf("seed incr result count=%d want=1", len(results))
	}

	if err := results[0].Error(); err != nil {
		t.Fatalf("seed incr failed: %v", err)
	}

	if ttl := mini.TTL(key); ttl != 0 {
		t.Fatalf("seed key ttl=%v want no ttl", ttl)
	}

	count, err := incrWithTTL(t.Context(), cacheClient, key, 2*time.Second)
	if err != nil {
		t.Fatalf("incrWithTTL failed: %v", err)
	}

	if count != 2 {
		t.Fatalf("count=%d want=2", count)
	}

	if ttl := mini.TTL(key); ttl != 2*time.Second {
		t.Fatalf("healed ttl=%v want=2s", ttl)
	}
}

func TestIncrWithTTL_ReturnsErrorOnNilPipelineResults(t *testing.T) {
	baseCache := testutil.NewTestCacheService(t.Context(), t)
	cacheClient := &failingCacheClient{Client: baseCache, nilDoMulti: true}

	_, err := incrWithTTL(t.Context(), cacheClient, "auth:test:nil-results", time.Minute)
	if err == nil {
		t.Fatal("expected error for nil pipeline results")
	}
}
