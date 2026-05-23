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
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	sharedlogging "github.com/park285/hololive-bot/shared-go/pkg/logging"
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

func newTestLogger() *slog.Logger {
	return sharedlogging.NewTestLogger()
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dbName := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormLogger.Default.LogMode(gormLogger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	return db
}

func newTestCacheWithMini(t *testing.T) (*cache.Service, *miniredis.Miniredis, func()) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	host, portStr, err := net.SplitHostPort(mr.Addr())
	if err != nil {
		mr.Close()
		t.Fatalf("failed to split host/port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		mr.Close()
		t.Fatalf("failed to parse port: %v", err)
	}

	cacheClient, err := cache.NewCacheService(context.Background(), cache.Config{
		Host:              host,
		Port:              port,
		DisableCache:      true,
		ForceSingleClient: true,
	}, newTestLogger())
	if err != nil {
		mr.Close()
		t.Fatalf("failed to create cache service: %v", err)
	}

	cleanup := func() {
		_ = cacheClient.Close()
		mr.Close()
	}

	return cacheClient, mr, cleanup
}

func newTestCache(t *testing.T) (*cache.Service, func()) {
	t.Helper()

	cacheClient, _, cleanup := newTestCacheWithMini(t)
	return cacheClient, cleanup
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
	service, err := NewService(context.Background(), db, nil, newTestLogger(), DefaultConfig())
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
	cacheClient, cleanup := newTestCache(t)
	defer cleanup()

	config := DefaultConfig()
	config.SessionTTL = 30 * time.Minute
	config.UserSessionsTTL = 2 * time.Hour

	service, err := NewService(context.Background(), db, cacheClient, newTestLogger(), config)
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
	cacheClient, cleanup := newTestCache(t)
	defer cleanup()

	config := DefaultConfig()
	config.LoginRateLimitPerMinute = 2
	config.LoginFailLimit = 100 // 레이트리밋 테스트에서 락 영향 제거

	service, err := NewService(context.Background(), db, cacheClient, newTestLogger(), config)
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
	cacheClient, cleanup := newTestCache(t)
	defer cleanup()

	config := DefaultConfig()
	config.LoginRateLimitPerMinute = 1000
	config.LoginFailLimit = 3
	config.LoginFailWindow = 10 * time.Minute
	config.LoginLockDuration = 10 * time.Minute

	service, err := NewService(context.Background(), db, cacheClient, newTestLogger(), config)
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

func TestPasswordReset_RevokesSessions(t *testing.T) {
	db := newTestDB(t)
	cacheClient, cleanup := newTestCache(t)
	defer cleanup()

	config := DefaultConfig()
	config.LoginRateLimitPerMinute = 1000

	service, err := NewService(context.Background(), db, cacheClient, newTestLogger(), config)
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

func TestPasswordResetRequest_RateLimited(t *testing.T) {
	db := newTestDB(t)
	cacheClient, cleanup := newTestCache(t)
	defer cleanup()

	config := DefaultConfig()
	config.PasswordResetRequestRateLimitPerMinute = 2

	service, err := NewService(context.Background(), db, cacheClient, newTestLogger(), config)
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

func TestCreateSession_RollsBackSessionWhenIndexAddFails(t *testing.T) {
	db := newTestDB(t)
	baseCache, cleanup := newTestCache(t)
	defer cleanup()

	userID := "user-1"
	cacheClient := &failingCacheClient{
		Client:      baseCache,
		failSAddKey: userSessionsKeyPrefix + userID,
		failSAddErr: stdErrors.New("sadd failed"),
		failDelKeys: map[string]error{},
	}

	service, err := NewService(context.Background(), db, cacheClient, newTestLogger(), DefaultConfig())
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
	baseCache, cleanup := newTestCache(t)
	defer cleanup()

	userID := "user-1"
	cacheClient := &failingCacheClient{
		Client:        baseCache,
		failExpireKey: userSessionsKeyPrefix + userID,
		failExpireErr: stdErrors.New("expire failed"),
		failDelKeys:   map[string]error{},
	}

	service, err := NewService(context.Background(), db, cacheClient, newTestLogger(), DefaultConfig())
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

func TestRefresh_RollsBackNewSessionWhenOldInvalidationFails(t *testing.T) {
	db := newTestDB(t)
	baseCache, cleanup := newTestCache(t)
	defer cleanup()

	service, err := NewService(context.Background(), db, baseCache, newTestLogger(), DefaultConfig())
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

	oldKey := sessionKeyPrefix + sha256Hex(session.Token)
	service.cacheClient = &failingCacheClient{
		Client:      baseCache,
		failDelKeys: map[string]error{oldKey: stdErrors.New("delete old session failed")},
	}

	_, err = service.Refresh(context.Background(), session.Token)
	if err == nil {
		t.Fatalf("expected refresh error")
	}
	assertAuthCode(t, err, CodeInternal)

	sessionKeys, err := baseCache.ScanKeys(context.Background(), sessionKeyPrefix+"*", 10)
	if err != nil {
		t.Fatalf("scan session keys: %v", err)
	}
	if len(sessionKeys) != 1 || sessionKeys[0] != oldKey {
		t.Fatalf("expected only old session key to remain, got=%v", sessionKeys)
	}

	members, err := baseCache.SMembers(context.Background(), userSessionsKeyPrefix+user.ID)
	if err != nil {
		t.Fatalf("read user sessions index: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("expected no indexed sessions after rollback, got=%v", members)
	}
}

func TestRefresh_KeepsNewSessionWhenOldIndexRemovalFails(t *testing.T) {
	db := newTestDB(t)
	baseCache, cleanup := newTestCache(t)
	defer cleanup()

	service, err := NewService(context.Background(), db, baseCache, newTestLogger(), DefaultConfig())
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
	baseCache, cleanup := newTestCache(t)
	defer cleanup()

	service, err := NewService(context.Background(), db, baseCache, newTestLogger(), DefaultConfig())
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
	cacheClient, mini, cleanup := newTestCacheWithMini(t)
	defer cleanup()

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
