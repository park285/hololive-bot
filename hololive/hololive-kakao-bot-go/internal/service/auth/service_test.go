package auth

import (
	"context"
	stdErrors "errors"
	"fmt"
	"io"
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
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
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

func newTestCache(t *testing.T) (*cache.Service, func()) {
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

	cacheSvc, err := cache.NewCacheService(context.Background(), cache.Config{
		Host:         host,
		Port:         port,
		DisableCache: true,
	}, newTestLogger())
	if err != nil {
		mr.Close()
		t.Fatalf("failed to create cache service: %v", err)
	}

	cleanup := func() {
		_ = cacheSvc.Close()
		mr.Close()
	}

	return cacheSvc, cleanup
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
	svc, err := NewService(context.Background(), db, nil, newTestLogger(), DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = svc.Register(context.Background(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	_, err = svc.Register(context.Background(), "USER@example.com", "Password1", "User2")
	if err == nil {
		t.Fatalf("expected duplicate error, got nil")
	}
	assertAuthCode(t, err, CodeEmailExists)
}

func TestLogin_SessionFlow(t *testing.T) {
	db := newTestDB(t)
	cacheSvc, cleanup := newTestCache(t)
	defer cleanup()

	cfg := DefaultConfig()
	cfg.SessionTTL = 30 * time.Minute
	cfg.UserSessionsTTL = 2 * time.Hour

	svc, err := NewService(context.Background(), db, cacheSvc, newTestLogger(), cfg)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = svc.Register(context.Background(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	session, user, err := svc.Login(context.Background(), "user@example.com", "Password1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if session == nil || session.Token == "" {
		t.Fatalf("expected session token")
	}
	if user == nil || user.ID == "" {
		t.Fatalf("expected user")
	}

	me, err := svc.Me(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("me failed: %v", err)
	}
	if me.ID != user.ID {
		t.Fatalf("unexpected me user: got=%s want=%s", me.ID, user.ID)
	}

	refreshed, err := svc.Refresh(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	_, err = svc.Me(context.Background(), session.Token)
	if err == nil {
		t.Fatalf("expected old token to be invalid after refresh")
	}
	assertAuthCode(t, err, CodeUnauthorized)

	_, err = svc.Me(context.Background(), refreshed.Token)
	if err != nil {
		t.Fatalf("me with refreshed token failed: %v", err)
	}

	if err := svc.Logout(context.Background(), refreshed.Token); err != nil {
		t.Fatalf("logout failed: %v", err)
	}

	_, err = svc.Me(context.Background(), refreshed.Token)
	if err == nil {
		t.Fatalf("expected token to be invalid after logout")
	}
	assertAuthCode(t, err, CodeUnauthorized)
}

func TestLogin_RateLimited(t *testing.T) {
	db := newTestDB(t)
	cacheSvc, cleanup := newTestCache(t)
	defer cleanup()

	cfg := DefaultConfig()
	cfg.LoginRateLimitPerMinute = 2
	cfg.LoginFailLimit = 100 // 레이트리밋 테스트에서 락 영향 제거

	svc, err := NewService(context.Background(), db, cacheSvc, newTestLogger(), cfg)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = svc.Register(context.Background(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	_, _, err = svc.Login(context.Background(), "user@example.com", "WrongPass1", "1.2.3.4")
	if err == nil {
		t.Fatalf("expected login failure")
	}
	assertAuthCode(t, err, CodeInvalidCredentials)

	_, _, err = svc.Login(context.Background(), "user@example.com", "WrongPass1", "1.2.3.4")
	if err == nil {
		t.Fatalf("expected login failure")
	}
	assertAuthCode(t, err, CodeInvalidCredentials)

	_, _, err = svc.Login(context.Background(), "user@example.com", "WrongPass1", "1.2.3.4")
	if err == nil {
		t.Fatalf("expected rate limited error")
	}
	assertAuthCode(t, err, CodeRateLimited)
}

func TestLogin_AccountLocked(t *testing.T) {
	db := newTestDB(t)
	cacheSvc, cleanup := newTestCache(t)
	defer cleanup()

	cfg := DefaultConfig()
	cfg.LoginRateLimitPerMinute = 1000
	cfg.LoginFailLimit = 3
	cfg.LoginFailWindow = 10 * time.Minute
	cfg.LoginLockDuration = 10 * time.Minute

	svc, err := NewService(context.Background(), db, cacheSvc, newTestLogger(), cfg)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = svc.Register(context.Background(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	for i := range 3 {
		_, _, err = svc.Login(context.Background(), "user@example.com", "WrongPass1", "127.0.0.1")
		if err == nil {
			t.Fatalf("expected login failure at attempt %d", i+1)
		}
		assertAuthCode(t, err, CodeInvalidCredentials)
	}

	_, _, err = svc.Login(context.Background(), "user@example.com", "Password1", "127.0.0.1")
	if err == nil {
		t.Fatalf("expected account locked error")
	}
	assertAuthCode(t, err, CodeAccountLocked)
}

func TestPasswordReset_RevokesSessions(t *testing.T) {
	db := newTestDB(t)
	cacheSvc, cleanup := newTestCache(t)
	defer cleanup()

	cfg := DefaultConfig()
	cfg.LoginRateLimitPerMinute = 1000

	svc, err := NewService(context.Background(), db, cacheSvc, newTestLogger(), cfg)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = svc.Register(context.Background(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	session, _, err := svc.Login(context.Background(), "user@example.com", "Password1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	resetToken, err := svc.RequestPasswordReset(context.Background(), "user@example.com", "127.0.0.1")
	if err != nil {
		t.Fatalf("reset-request failed: %v", err)
	}
	if resetToken == "" {
		t.Fatalf("expected reset token")
	}

	if err := svc.ResetPassword(context.Background(), resetToken, "NewPassw0rd1"); err != nil {
		t.Fatalf("reset failed: %v", err)
	}

	_, err = svc.Me(context.Background(), session.Token)
	if err == nil {
		t.Fatalf("expected old session revoked after reset")
	}
	assertAuthCode(t, err, CodeUnauthorized)

	_, _, err = svc.Login(context.Background(), "user@example.com", "Password1", "127.0.0.1")
	if err == nil {
		t.Fatalf("expected old password to be invalid")
	}
	assertAuthCode(t, err, CodeInvalidCredentials)

	_, _, err = svc.Login(context.Background(), "user@example.com", "NewPassw0rd1", "127.0.0.1")
	if err != nil {
		t.Fatalf("expected login with new password: %v", err)
	}
}

func TestPasswordResetRequest_RateLimited(t *testing.T) {
	db := newTestDB(t)
	cacheSvc, cleanup := newTestCache(t)
	defer cleanup()

	cfg := DefaultConfig()
	cfg.PasswordResetRequestRateLimitPerMinute = 2

	svc, err := NewService(context.Background(), db, cacheSvc, newTestLogger(), cfg)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, err = svc.Register(context.Background(), "user@example.com", "Password1", "User")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	_, err = svc.RequestPasswordReset(context.Background(), "user@example.com", "10.0.0.1")
	if err != nil {
		t.Fatalf("first reset-request failed: %v", err)
	}

	_, err = svc.RequestPasswordReset(context.Background(), "user@example.com", "10.0.0.1")
	if err != nil {
		t.Fatalf("second reset-request failed: %v", err)
	}

	_, err = svc.RequestPasswordReset(context.Background(), "user@example.com", "10.0.0.1")
	if err == nil {
		t.Fatalf("expected rate limited error")
	}
	assertAuthCode(t, err, CodeRateLimited)
}
