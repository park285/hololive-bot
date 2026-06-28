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
	"sync"
	"testing"
	"time"

	"github.com/valkey-io/valkey-go"
	"golang.org/x/crypto/bcrypt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/testutil"
	sharedlogging "github.com/park285/shared-go/pkg/logging"
)

type failingCacheClient struct {
	cache.Client

	failSAddKey   string
	failSAddErr   error
	failExpireKey string
	failExpireErr error
	failDelKeys   map[string]error
	failDelMany   error
	failSRemKey   string
	failSRemErr   error
	nilDoMulti    bool
	// casKeyConflict가 지정되면 해당 키의 CompareAndDelete가 (false, nil)을 반환해
	// 동시 회전(다른 요청이 먼저 claim)을 시뮬레이션한다.
	casKeyConflict string
}

func (c *failingCacheClient) CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error) {
	if c.casKeyConflict != "" && key == c.casKeyConflict {
		return false, nil
	}
	return c.Client.CompareAndDelete(ctx, key, expectedValue)
}

func (c *failingCacheClient) SAdd(ctx context.Context, key string, members []string) (int64, error) {
	if c.failSAddErr != nil && key == c.failSAddKey {
		return 0, c.failSAddErr
	}
	return c.Client.SAdd(ctx, key, members)
}

func (c *failingCacheClient) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if c.failExpireErr != nil && key == c.failExpireKey {
		return c.failExpireErr
	}
	return c.Client.Expire(ctx, key, ttl)
}

func (c *failingCacheClient) Del(ctx context.Context, key string) error {
	if err, ok := c.failDelKeys[key]; ok {
		return err
	}
	return c.Client.Del(ctx, key)
}

func (c *failingCacheClient) DelMany(ctx context.Context, keys []string) (int64, error) {
	if c.failDelMany != nil {
		return 0, c.failDelMany
	}
	return c.Client.DelMany(ctx, keys)
}

func (c *failingCacheClient) SRem(ctx context.Context, key string, members []string) (int64, error) {
	if c.failSRemErr != nil && key == c.failSRemKey {
		return 0, c.failSRemErr
	}
	return c.Client.SRem(ctx, key, members)
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

func assertAuthCode(t *testing.T, err error, want ErrorCode) {
	t.Helper()

	var ae *Error
	if !stdErrors.As(err, &ae) {
		t.Fatalf("expected *auth.Error, got: %T (%v)", err, err)
	}
	if ae.Code != want {
		t.Fatalf("unexpected code: got=%s want=%s", ae.Code, want)
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	db := newTestDB(t)
	service, err := NewService(context.Background(), db, nil, sharedlogging.NewTestLogger(), DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = service.Register(context.Background(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	_, err = service.Register(context.Background(), "USER@example.com", "Password1", "User2")
	if err == nil {
		t.Fatalf("expected duplicate error, got nil")
	}
	assertAuthCode(t, err, CodeEmailExists)
}

func TestLogin_SessionFlow(t *testing.T) {
	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t, context.Background())

	config := DefaultConfig()
	config.SessionTTL = 30 * time.Minute
	config.UserSessionsTTL = 2 * time.Hour

	service, err := NewService(context.Background(), db, cacheClient, sharedlogging.NewTestLogger(), config)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = service.Register(context.Background(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	session, user, err := service.Login(context.Background(), "user@example.com", "Password1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if session == nil || session.Token == "" {
		t.Fatalf("expected session token")
	}
	if user == nil || user.ID == "" {
		t.Fatalf("expected user")
	}

	me, err := service.Me(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("me failed: %v", err)
	}
	if me.ID != user.ID {
		t.Fatalf("unexpected me user: got=%s want=%s", me.ID, user.ID)
	}

	refreshed, err := service.Refresh(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	_, err = service.Me(context.Background(), session.Token)
	if err == nil {
		t.Fatalf("expected old token to be invalid after refresh")
	}
	assertAuthCode(t, err, CodeUnauthorized)

	_, err = service.Me(context.Background(), refreshed.Token)
	if err != nil {
		t.Fatalf("me with refreshed token failed: %v", err)
	}

	if err := service.Logout(context.Background(), refreshed.Token); err != nil {
		t.Fatalf("logout failed: %v", err)
	}

	_, err = service.Me(context.Background(), refreshed.Token)
	if err == nil {
		t.Fatalf("expected token to be invalid after logout")
	}
	assertAuthCode(t, err, CodeUnauthorized)
}

func TestLogin_RateLimited(t *testing.T) {
	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t, context.Background())

	config := DefaultConfig()
	config.LoginRateLimitPerMinute = 2
	config.LoginFailLimit = 100 // 레이트리밋 테스트에서 락 영향 제거

	service, err := NewService(context.Background(), db, cacheClient, sharedlogging.NewTestLogger(), config)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = service.Register(context.Background(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	_, _, err = service.Login(context.Background(), "user@example.com", "WrongPass1", "1.2.3.4")
	if err == nil {
		t.Fatalf("expected login failure")
	}
	assertAuthCode(t, err, CodeInvalidCredentials)

	_, _, err = service.Login(context.Background(), "user@example.com", "WrongPass1", "1.2.3.4")
	if err == nil {
		t.Fatalf("expected login failure")
	}
	assertAuthCode(t, err, CodeInvalidCredentials)

	_, _, err = service.Login(context.Background(), "user@example.com", "WrongPass1", "1.2.3.4")
	if err == nil {
		t.Fatalf("expected rate limited error")
	}
	assertAuthCode(t, err, CodeRateLimited)
}

func TestLogin_AccountLocked(t *testing.T) {
	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t, context.Background())

	config := DefaultConfig()
	config.LoginRateLimitPerMinute = 1000
	config.LoginFailLimit = 3
	config.LoginFailWindow = 10 * time.Minute
	config.LoginLockDuration = 10 * time.Minute

	service, err := NewService(context.Background(), db, cacheClient, sharedlogging.NewTestLogger(), config)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = service.Register(context.Background(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	for i := range 3 {
		_, _, err = service.Login(context.Background(), "user@example.com", "WrongPass1", "127.0.0.1")
		if err == nil {
			t.Fatalf("expected login failure at attempt %d", i+1)
		}
		assertAuthCode(t, err, CodeInvalidCredentials)
	}

	_, _, err = service.Login(context.Background(), "user@example.com", "Password1", "127.0.0.1")
	if err == nil {
		t.Fatalf("expected account locked error")
	}
	assertAuthCode(t, err, CodeAccountLocked)
}

func TestLogin_UnknownEmailRecordsFailureAndLocksAccount(t *testing.T) {
	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t, context.Background())

	config := DefaultConfig()
	config.LoginRateLimitPerMinute = 1000
	config.LoginFailLimit = 1
	config.LoginFailWindow = 10 * time.Minute
	config.LoginLockDuration = 10 * time.Minute

	service, err := NewService(context.Background(), db, cacheClient, sharedlogging.NewTestLogger(), config)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, _, err = service.Login(context.Background(), "missing@example.com", "Password1", "127.0.0.1")
	if err == nil {
		t.Fatalf("expected invalid credentials for unknown email")
	}
	assertAuthCode(t, err, CodeInvalidCredentials)

	_, _, err = service.Login(context.Background(), "missing@example.com", "Password1", "127.0.0.1")
	if err == nil {
		t.Fatalf("expected account locked after unknown-email failure hook")
	}
	assertAuthCode(t, err, CodeAccountLocked)
}

func TestMe_ReturnsUnauthorizedWhenSessionUserIsMissing(t *testing.T) {
	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t, context.Background())

	service, err := NewService(context.Background(), db, cacheClient, sharedlogging.NewTestLogger(), DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	session, err := service.createSession(context.Background(), "missing-user")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err = service.Me(context.Background(), session.Token)
	if err == nil {
		t.Fatalf("expected unauthorized for missing user")
	}
	assertAuthCode(t, err, CodeUnauthorized)
}

func TestPasswordReset_RevokesSessions(t *testing.T) {
	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t, context.Background())

	config := DefaultConfig()
	config.LoginRateLimitPerMinute = 1000

	service, err := NewService(context.Background(), db, cacheClient, sharedlogging.NewTestLogger(), config)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = service.Register(context.Background(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	session, _, err := service.Login(context.Background(), "user@example.com", "Password1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	resetToken, err := service.RequestPasswordReset(context.Background(), "user@example.com", "127.0.0.1")
	if err != nil {
		t.Fatalf("reset-request failed: %v", err)
	}
	if resetToken == "" {
		t.Fatalf("expected reset token")
	}

	if err := service.ResetPassword(context.Background(), resetToken, "NewPassw0rd1"); err != nil {
		t.Fatalf("reset failed: %v", err)
	}

	_, err = service.Me(context.Background(), session.Token)
	if err == nil {
		t.Fatalf("expected old session revoked after reset")
	}
	assertAuthCode(t, err, CodeUnauthorized)

	_, _, err = service.Login(context.Background(), "user@example.com", "Password1", "127.0.0.1")
	if err == nil {
		t.Fatalf("expected old password to be invalid")
	}
	assertAuthCode(t, err, CodeInvalidCredentials)

	_, _, err = service.Login(context.Background(), "user@example.com", "NewPassw0rd1", "127.0.0.1")
	if err != nil {
		t.Fatalf("expected login with new password: %v", err)
	}
}

func TestPasswordResetRequest_UnknownEmailReturnsEmptyToken(t *testing.T) {
	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t, context.Background())

	service, err := NewService(context.Background(), db, cacheClient, sharedlogging.NewTestLogger(), DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	token, err := service.RequestPasswordReset(context.Background(), "missing@example.com", "127.0.0.1")
	if err != nil {
		t.Fatalf("request password reset for unknown email should not fail: %v", err)
	}
	if token != "" {
		t.Fatalf("expected empty token for unknown email, got %q", token)
	}
}

func TestPasswordResetRequest_RateLimited(t *testing.T) {
	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t, context.Background())

	config := DefaultConfig()
	config.PasswordResetRequestRateLimitPerMinute = 2

	service, err := NewService(context.Background(), db, cacheClient, sharedlogging.NewTestLogger(), config)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = service.Register(context.Background(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	_, err = service.RequestPasswordReset(context.Background(), "user@example.com", "10.0.0.1")
	if err != nil {
		t.Fatalf("first reset-request failed: %v", err)
	}

	_, err = service.RequestPasswordReset(context.Background(), "user@example.com", "10.0.0.1")
	if err != nil {
		t.Fatalf("second reset-request failed: %v", err)
	}

	_, err = service.RequestPasswordReset(context.Background(), "user@example.com", "10.0.0.1")
	if err == nil {
		t.Fatalf("expected rate limited error")
	}
	assertAuthCode(t, err, CodeRateLimited)
}

func TestResetPassword_RollsBackPasswordUpdateWhenMarkTokenUsedFails(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t, ctx)

	config := DefaultConfig()
	config.BcryptCost = bcrypt.MinCost

	service, err := NewService(ctx, db, cacheClient, sharedlogging.NewTestLogger(), config)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	if _, err := service.Register(ctx, "user@example.com", "Password1", "User"); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	resetToken, err := service.RequestPasswordReset(ctx, "user@example.com", "127.0.0.1")
	if err != nil {
		t.Fatalf("request password reset: %v", err)
	}

	reset, err := service.findValidPasswordResetToken(ctx, sha256Hex(resetToken), time.Now().UTC())
	if err != nil {
		t.Fatalf("find reset token: %v", err)
	}

	oldHash := storedPasswordHash(t, service)
	if _, err := db.Exec(ctx, `
		CREATE OR REPLACE FUNCTION auth_reset_token_sleep()
		RETURNS trigger AS $$
		BEGIN
			PERFORM pg_sleep(0.2);
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql
	`); err != nil {
		t.Fatalf("create sleep function: %v", err)
	}
	if _, err := db.Exec(ctx, `
		CREATE TRIGGER auth_reset_token_sleep_before_update
		BEFORE UPDATE ON auth_password_reset_tokens
		FOR EACH ROW EXECUTE FUNCTION auth_reset_token_sleep()
	`); err != nil {
		t.Fatalf("create sleep trigger: %v", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	_, err = service.applyPasswordReset(timeoutCtx, reset.TokenHash, "new-hash", time.Now().UTC())
	if err == nil {
		t.Fatalf("expected applyPasswordReset to fail")
	}
	assertAuthCode(t, err, CodeInternal)

	if got := storedPasswordHash(t, service); got != oldHash {
		t.Fatalf("password hash should roll back, got=%q want=%q", got, oldHash)
	}

	var usedAt *time.Time
	if err := db.QueryRow(ctx, `SELECT used_at FROM auth_password_reset_tokens WHERE token_hash = $1`, reset.TokenHash).Scan(&usedAt); err != nil {
		t.Fatalf("load token used_at: %v", err)
	}
	if usedAt != nil {
		t.Fatalf("token used_at should remain null after rollback")
	}
}

func TestResetPassword_ConsumesTokenExactlyOnceUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t, ctx)

	config := DefaultConfig()
	config.BcryptCost = bcrypt.MinCost
	config.LoginRateLimitPerMinute = 1000

	service, err := NewService(ctx, db, cacheClient, sharedlogging.NewTestLogger(), config)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	if _, err := service.Register(ctx, "user@example.com", "Password1", "User"); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	resetToken, err := service.RequestPasswordReset(ctx, "user@example.com", "127.0.0.1")
	if err != nil {
		t.Fatalf("request password reset: %v", err)
	}

	const workers = 8
	var (
		wg        sync.WaitGroup
		start     = make(chan struct{})
		mu        sync.Mutex
		successes int
		failures  []error
	)
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			<-start
			err := service.ResetPassword(ctx, resetToken, "NewPassw0rd1")
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successes++
				return
			}
			failures = append(failures, err)
		}()
	}
	close(start)
	wg.Wait()

	if successes != 1 {
		t.Fatalf("expected exactly one concurrent reset to succeed, got %d", successes)
	}
	if len(failures) != workers-1 {
		t.Fatalf("expected %d rejected resets, got %d", workers-1, len(failures))
	}
	for _, err := range failures {
		assertAuthCode(t, err, CodeInvalidInput)
	}

	if err := service.ResetPassword(ctx, resetToken, "NewPassw0rd2"); err == nil {
		t.Fatalf("expected reused token to be rejected after consumption")
	} else {
		assertAuthCode(t, err, CodeInvalidInput)
	}

	if _, _, err := service.Login(ctx, "user@example.com", "NewPassw0rd1", "127.0.0.1"); err != nil {
		t.Fatalf("expected login with the winning new password: %v", err)
	}
}

func TestCreateSession_RollsBackSessionWhenIndexAddFails(t *testing.T) {
	db := newTestDB(t)
	baseCache := testutil.NewTestCacheService(t, context.Background())

	userID := "user-1"
	cacheClient := &failingCacheClient{
		Client:      baseCache,
		failSAddKey: userSessionsKeyPrefix + userID,
		failSAddErr: stdErrors.New("sadd failed"),
		failDelKeys: map[string]error{},
	}

	service, err := NewService(context.Background(), db, cacheClient, sharedlogging.NewTestLogger(), DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = service.createSession(context.Background(), userID)
	if err == nil {
		t.Fatalf("expected createSession error")
	}
	assertAuthCode(t, err, CodeInternal)

	sessionKeys, err := baseCache.ScanKeys(context.Background(), sessionKeyPrefix+"*", 10)
	if err != nil {
		t.Fatalf("scan session keys: %v", err)
	}
	if len(sessionKeys) != 0 {
		t.Fatalf("expected no session keys after rollback, got=%v", sessionKeys)
	}
}

func TestCreateSession_RollsBackSessionWhenIndexExpireFails(t *testing.T) {
	db := newTestDB(t)
	baseCache := testutil.NewTestCacheService(t, context.Background())

	userID := "user-1"
	cacheClient := &failingCacheClient{
		Client:        baseCache,
		failExpireKey: userSessionsKeyPrefix + userID,
		failExpireErr: stdErrors.New("expire failed"),
		failDelKeys:   map[string]error{},
	}

	service, err := NewService(context.Background(), db, cacheClient, sharedlogging.NewTestLogger(), DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = service.createSession(context.Background(), userID)
	if err == nil {
		t.Fatalf("expected createSession error")
	}
	assertAuthCode(t, err, CodeInternal)

	sessionKeys, err := baseCache.ScanKeys(context.Background(), sessionKeyPrefix+"*", 10)
	if err != nil {
		t.Fatalf("scan session keys: %v", err)
	}
	if len(sessionKeys) != 0 {
		t.Fatalf("expected no session keys after rollback, got=%v", sessionKeys)
	}
}

// CAS 회전에서 old session claim이 실패하면(동시 회전) 새 세션을 만들지 않고
// CodeUnauthorized를 반환해야 한다. old session 키는 다른 요청이 소유하므로 건드리지 않는다.
func TestRefresh_RejectsWhenSessionAlreadyClaimed(t *testing.T) {
	db := newTestDB(t)
	baseCache := testutil.NewTestCacheService(t, context.Background())

	service, err := NewService(context.Background(), db, baseCache, sharedlogging.NewTestLogger(), DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = service.Register(context.Background(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	session, _, err := service.Login(context.Background(), "user@example.com", "Password1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	oldKey := sessionKeyPrefix + sha256Hex(session.Token)
	service.cacheClient = &failingCacheClient{
		Client:         baseCache,
		casKeyConflict: oldKey,
	}

	_, err = service.Refresh(context.Background(), session.Token)
	if err == nil {
		t.Fatalf("expected refresh to be rejected when session already claimed")
	}
	assertAuthCode(t, err, CodeUnauthorized)

	sessionKeys, err := baseCache.ScanKeys(context.Background(), sessionKeyPrefix+"*", 10)
	if err != nil {
		t.Fatalf("scan session keys: %v", err)
	}
	if len(sessionKeys) != 1 || sessionKeys[0] != oldKey {
		t.Fatalf("expected only old session key to remain (no new session), got=%v", sessionKeys)
	}
}

func TestRefresh_KeepsNewSessionWhenOldIndexRemovalFails(t *testing.T) {
	db := newTestDB(t)
	baseCache := testutil.NewTestCacheService(t, context.Background())

	service, err := NewService(context.Background(), db, baseCache, sharedlogging.NewTestLogger(), DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = service.Register(context.Background(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	session, user, err := service.Login(context.Background(), "user@example.com", "Password1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	oldHash := sha256Hex(session.Token)
	oldKey := sessionKeyPrefix + oldHash
	service.cacheClient = &failingCacheClient{
		Client:      baseCache,
		failSRemKey: userSessionsKeyPrefix + user.ID,
		failSRemErr: stdErrors.New("remove old session index failed"),
	}

	newSession, err := service.Refresh(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("refresh should succeed when old session key is removed: %v", err)
	}

	if newSession == nil || newSession.Token == "" {
		t.Fatalf("expected new session")
	}

	exists, err := baseCache.Exists(context.Background(), oldKey)
	if err != nil {
		t.Fatalf("check old session key: %v", err)
	}
	if exists {
		t.Fatalf("expected old session key to be deleted")
	}

	newKey := sessionKeyPrefix + sha256Hex(newSession.Token)
	exists, err = baseCache.Exists(context.Background(), newKey)
	if err != nil {
		t.Fatalf("check new session key: %v", err)
	}
	if !exists {
		t.Fatalf("expected new session key to remain")
	}

	if _, err := service.Me(context.Background(), newSession.Token); err != nil {
		t.Fatalf("expected new session token to be valid: %v", err)
	}
}

func TestResetPassword_IgnoresSessionRevocationFailureAfterCommit(t *testing.T) {
	db := newTestDB(t)
	baseCache := testutil.NewTestCacheService(t, context.Background())

	service, err := NewService(context.Background(), db, baseCache, sharedlogging.NewTestLogger(), DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = service.Register(context.Background(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	session, _, err := service.Login(context.Background(), "user@example.com", "Password1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	resetToken, err := service.RequestPasswordReset(context.Background(), "user@example.com", "127.0.0.1")
	if err != nil {
		t.Fatalf("request password reset failed: %v", err)
	}

	service.cacheClient = &failingCacheClient{
		Client:      baseCache,
		failDelMany: stdErrors.New("delete sessions failed"),
	}

	err = service.ResetPassword(context.Background(), resetToken, "NewPassw0rd1")
	if err != nil {
		t.Fatalf("reset password should succeed after password commit: %v", err)
	}

	if _, err := service.Me(context.Background(), session.Token); err != nil {
		t.Fatalf("expected existing session to remain when revocation fails: %v", err)
	}

	if _, _, err := service.Login(context.Background(), "user@example.com", "NewPassw0rd1", "127.0.0.1"); err != nil {
		t.Fatalf("expected login with new password to succeed: %v", err)
	}
}

func TestIncrWithTTL_SetsTTLOnFirstIncrementAndKeepsIt(t *testing.T) {
	cacheClient, mini := testutil.NewTestCacheServiceWithMini(t, context.Background())

	key := "auth:test:counter"

	count, err := incrWithTTL(context.Background(), cacheClient, key, 500*time.Millisecond)
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

	count, err = incrWithTTL(context.Background(), cacheClient, key, 2*time.Second)
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
	cacheClient, mini := testutil.NewTestCacheServiceWithMini(t, context.Background())

	key := "auth:test:counter:no-ttl"
	results := cacheClient.DoMulti(context.Background(), cacheClient.B().Incr().Key(key).Build())
	if len(results) != 1 {
		t.Fatalf("seed incr result count=%d want=1", len(results))
	}
	if err := results[0].Error(); err != nil {
		t.Fatalf("seed incr failed: %v", err)
	}
	if ttl := mini.TTL(key); ttl != 0 {
		t.Fatalf("seed key ttl=%v want no ttl", ttl)
	}

	count, err := incrWithTTL(context.Background(), cacheClient, key, 2*time.Second)
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
	baseCache := testutil.NewTestCacheService(t, context.Background())
	cacheClient := &failingCacheClient{Client: baseCache, nilDoMulti: true}

	_, err := incrWithTTL(context.Background(), cacheClient, "auth:test:nil-results", time.Minute)
	if err == nil {
		t.Fatalf("expected error for nil pipeline results")
	}
}
