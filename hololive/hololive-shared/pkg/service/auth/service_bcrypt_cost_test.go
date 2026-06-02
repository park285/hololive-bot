package auth

import (
	"context"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/kapu/hololive-shared/pkg/testutil"
	sharedlogging "github.com/park285/shared-go/pkg/logging"
)

func storedPasswordHash(t *testing.T, service *Service, email string) string {
	t.Helper()

	var passwordHash string
	if err := service.db.QueryRow(
		context.Background(),
		`SELECT password_hash FROM auth_users WHERE email = $1`,
		normalizeEmail(email),
	).Scan(&passwordHash); err != nil {
		t.Fatalf("load user: %v", err)
	}
	return passwordHash
}

// DefaultConfig의 BcryptCost는 안전 기본값(>=12)이어야 한다.
func TestDefaultConfig_BcryptCostSafeDefault(t *testing.T) {
	t.Parallel()

	if got := DefaultConfig().BcryptCost; got < 12 {
		t.Fatalf("DefaultConfig().BcryptCost = %d, want >= 12", got)
	}
}

// Register는 config.BcryptCost로 비밀번호를 해시해야 한다.
func TestRegister_UsesConfiguredBcryptCost(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	cfg := DefaultConfig()
	cfg.BcryptCost = 12

	service, err := NewService(context.Background(), db, nil, sharedlogging.NewTestLogger(), cfg)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	if _, err := service.Register(context.Background(), "user@example.com", "Password1", "User"); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	hash := storedPasswordHash(t, service, "user@example.com")
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("bcrypt.Cost: %v", err)
	}
	if cost != 12 {
		t.Fatalf("register hash cost = %d, want 12", cost)
	}

	// 해시/검증 일관성: 동일 비밀번호가 검증을 통과해야 한다.
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("Password1")); err != nil {
		t.Fatalf("password verification failed: %v", err)
	}
}

// ResetPassword도 config.BcryptCost로 새 비밀번호를 해시해야 한다.
func TestResetPassword_UsesConfiguredBcryptCost(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t, context.Background())
	cfg := DefaultConfig()
	cfg.BcryptCost = 13

	service, err := NewService(context.Background(), db, cacheClient, sharedlogging.NewTestLogger(), cfg)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	if _, err := service.Register(context.Background(), "user@example.com", "Password1", "User"); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	resetToken, err := service.RequestPasswordReset(context.Background(), "user@example.com", "127.0.0.1")
	if err != nil {
		t.Fatalf("reset request failed: %v", err)
	}

	if err := service.ResetPassword(context.Background(), resetToken, "NewPassw0rd1"); err != nil {
		t.Fatalf("reset failed: %v", err)
	}

	hash := storedPasswordHash(t, service, "user@example.com")
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("bcrypt.Cost: %v", err)
	}
	if cost != 13 {
		t.Fatalf("reset hash cost = %d, want 13", cost)
	}
}
